package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

var _ memory.Repository = (*Store)(nil)

func sourceKey(connector, externalID string) []byte {
	key, _ := json.Marshal([]string{connector, externalID})
	hash := sha256.Sum256(key)
	return hash[:]
}

func (s *Store) Ingest(ctx context.Context, scope memory.Scope, in memory.IngestRequest) (memory.IngestResult, error) {
	var result memory.IngestResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		result, err = s.ingestTx(ctx, tx, scope, in)
		return err
	})
	return result, err
}

func (s *Store) ingestTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.IngestRequest) (memory.IngestResult, error) {
	var result memory.IngestResult
	if !scope.Valid() || !scope.IsOwner {
		return result, memory.ErrForbidden
	}
	body, err := json.Marshal(in)
	if err != nil {
		return result, memory.ErrInvalid
	}
	hash := sha256.Sum256(body)
	key := sourceKey(in.Connector, in.ExternalID)
	err = func() error {
		// Serialize only this owner's source identity, including first insertion.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("%s:%x", scope.OwnerID, key)); err != nil {
			return err
		}
		var blocked bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM reimport_blocks WHERE owner_id=$1 AND source_key_hash=$2)", string(scope.OwnerID), key).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return memory.ErrBlocked
		}
		var id string
		var version int
		var state string
		err := tx.QueryRow(ctx, `SELECT s.id::text,r.version,r.state FROM sources s
			JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
			WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=$3 FOR UPDATE OF r`,
			string(scope.OwnerID), in.Connector, in.ExternalID).Scan(&id, &version, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			id = string(memory.NewID())
			if _, err := tx.Exec(ctx, "INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'source',1)", string(scope.OwnerID), id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO sources(owner_id,id,connector,external_id) VALUES($1,$2,$3,$4)", string(scope.OwnerID), id, in.Connector, in.ExternalID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if state != "active" {
			return memory.ErrBlocked
		}
		var priorHash []byte
		var priorVersion int
		err = tx.QueryRow(ctx, "SELECT version,content_hash FROM source_versions WHERE owner_id=$1 AND source_id=$2 AND external_version=$3", string(scope.OwnerID), id, in.ExternalVersion).Scan(&priorVersion, &priorHash)
		if err == nil {
			if !bytes.Equal(priorHash, hash[:]) {
				return memory.ErrConflict
			}
			result = memory.IngestResult{Ref: memory.Ref{ID: memory.ID(id), Version: priorVersion, Kind: memory.SourceKind}, Duplicate: true}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		version++
		if _, err := tx.Exec(ctx, "INSERT INTO record_versions(owner_id,record_id,version,expressed_at) VALUES($1,$2,$3,$4)", string(scope.OwnerID), id, version, in.ExpressedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, string(scope.OwnerID), id, version, in.ExternalVersion, hash[:], in.Title, in.Text, in.MediaType); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE memory_records SET version=$3,updated_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, version); err != nil {
			return err
		}
		if version > 1 {
			if err := invalidateTx(ctx, tx, scope, id); err != nil {
				return err
			}
			// Invalidate outputs that consumed assertions extracted from an older
			// version. The canonical historical claims are retained, while the
			// current view requires evidence from the latest source version.
			rows, err := tx.Query(ctx, `SELECT DISTINCT k.claim_id::text FROM claim_source_keys k
				JOIN memory_records r ON (r.owner_id,r.id)=(k.owner_id,k.claim_id)
				JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(r.owner_id,r.id,r.version)
				WHERE k.owner_id=$1 AND k.source_id=$2 AND v.actor='ai'`, string(scope.OwnerID), id)
			if err != nil {
				return err
			}
			claims := []string{}
			for rows.Next() {
				var claimID string
				if err := rows.Scan(&claimID); err != nil {
					rows.Close()
					return err
				}
				claims = append(claims, claimID)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, claimID := range claims {
				if err := invalidateTx(ctx, tx, scope, claimID); err != nil {
					return err
				}
			}
		}
		if err := enqueue(ctx, tx, scope.OwnerID, memory.ID(id), version, "source.chunk"); err != nil {
			return err
		}
		result = memory.IngestResult{Ref: memory.Ref{ID: memory.ID(id), Version: version, Kind: memory.SourceKind}}
		return nil
	}()
	return result, err
}

func enqueue(ctx context.Context, tx pgx.Tx, ownerID, id memory.ID, version int, stage string) error {
	_, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(owner_id,record_id,record_version,stage) DO NOTHING`, string(memory.NewID()), string(ownerID), string(id), version, stage)
	return err
}

func (s *Store) GetSource(ctx context.Context, scope memory.Scope, id memory.ID, version int) (memory.SourceResult, error) {
	result := memory.SourceResult{Processing: make([]memory.Processing, 0), Derived: []memory.Ref{}}
	if !scope.Valid() {
		return result, memory.ErrForbidden
	}
	var sourceID string
	var blobKey *string
	err := s.pool.QueryRow(ctx, `SELECT s.id::text,v.version,s.connector,s.external_id,v.external_version,v.title,v.body,v.media_type,
		rv.valid_from,rv.valid_to,rv.time_precision,rv.expressed_at,rv.recorded_at,rv.state,v.representation,(v.blob_key IS NOT NULL OR v.attachment_redacted),v.blob_key
		FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
		JOIN source_versions v ON (v.owner_id,v.source_id)=(s.owner_id,s.id) AND v.version=CASE WHEN $3=0 THEN r.version ELSE $3 END
		JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
		WHERE s.owner_id=$1 AND s.id=$2 AND r.state='active' AND rv.state='active'
		AND ($4 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=s.owner_id AND g.record_id=s.id AND g.principal_id=$5))`,
		string(scope.OwnerID), string(id), version, scope.IsOwner, scope.PrincipalID).Scan(&sourceID, &result.Source.Version,
		&result.Source.Connector, &result.Source.ExternalID, &result.Source.ExternalVersion, &result.Source.Title, &result.Source.Text, &result.Source.MediaType,
		&result.Source.ValidTime.From, &result.Source.ValidTime.To, &result.Source.ValidTime.Precision, &result.Source.ExpressedAt, &result.Source.RecordedAt, &result.Source.State, &result.Source.Representation, &result.Source.HasAttachment, &blobKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, memory.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	result.Source.AttachmentMissing = result.Source.HasAttachment && blobKey == nil
	if blobKey != nil {
		result.Source.AttachmentMissing = s.blobs == nil
		if s.blobs != nil {
			file, err := s.blobs.Open(ctx, scope, *blobKey)
			if err != nil {
				result.Source.AttachmentMissing = true
			} else {
				file.Close()
			}
		}
	}
	var sourceContext memory.SourceContext
	contextErr := s.pool.QueryRow(ctx, "SELECT conversation_key,parent_key,role,branch,gaps FROM source_contexts WHERE owner_id=$1 AND source_id=$2 AND source_version=$3", string(scope.OwnerID), string(id), result.Source.Version).Scan(&sourceContext.Conversation, &sourceContext.Parent, &sourceContext.Role, &sourceContext.Branch, &sourceContext.Gaps)
	if contextErr == nil {
		result.Context = &sourceContext
	} else if !errors.Is(contextErr, pgx.ErrNoRows) {
		return result, contextErr
	}
	result.Source.ID = memory.ID(sourceID)
	result.Source.Kind = memory.SourceKind
	rows, err := s.pool.Query(ctx, `SELECT id::text,stage,state,attempts,error_code,CASE WHEN stage='source.parse' THEN coalesce((SELECT v.representation FROM source_versions v WHERE v.owner_id=memory_jobs.owner_id AND v.derived_from_id=memory_jobs.record_id AND v.derived_from_version=memory_jobs.record_version ORDER BY version DESC LIMIT 1),'') ELSE '' END FROM memory_jobs
		WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 ORDER BY created_at,stage`, string(scope.OwnerID), string(id), result.Source.Version)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item memory.Processing
		var jobID string
		if err := rows.Scan(&jobID, &item.Stage, &item.State, &item.Attempts, &item.ErrorCode, &item.Method); err != nil {
			return result, err
		}
		item.ID = memory.ID(jobID)
		result.Processing = append(result.Processing, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = s.pool.Query(ctx, "SELECT v.source_id::text,v.version FROM source_versions v JOIN memory_records r ON (r.owner_id,r.id)=(v.owner_id,v.source_id) WHERE v.owner_id=$1 AND ((v.derived_from_id=$2 AND v.derived_from_version=$3) OR EXISTS(SELECT 1 FROM archive_entries ae WHERE ae.owner_id=v.owner_id AND ae.source_id=v.source_id AND ae.source_version=v.version AND ae.archive_id=$2 AND ae.archive_version=$3)) AND r.state='active' AND ($4 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=v.owner_id AND g.record_id=v.source_id AND g.principal_id=$5))", string(scope.OwnerID), string(id), result.Source.Version, scope.IsOwner, scope.PrincipalID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		ref := memory.Ref{Kind: memory.SourceKind}
		if err := rows.Scan(&ref.ID, &ref.Version); err != nil {
			return result, err
		}
		result.Derived = append(result.Derived, ref)
	}
	return result, rows.Err()
}
