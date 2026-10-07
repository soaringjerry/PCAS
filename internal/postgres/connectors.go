package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
)

const connectionColumns = `id::text,name,kind,url,token_env,interval_seconds,enabled,version,status,last_sync,next_sync,imported,error_code,gaps`

func scanConnection(row pgx.Row) (connectors.Connection, error) {
	var c connectors.Connection
	err := row.Scan(&c.ID, &c.Name, &c.Kind, &c.URL, &c.TokenEnv, &c.IntervalSeconds, &c.Enabled, &c.Version, &c.Status, &c.LastSync, &c.NextSync, &c.Imported, &c.Error, &c.Gaps)
	if c.Kind == "folder" {
		c.Folder = string(c.ID)
	}
	return c, err
}
func (s *Store) ListConnections(ctx context.Context, scope memory.Scope) ([]connectors.Connection, error) {
	out := []connectors.Connection{}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, "SELECT "+connectionColumns+" FROM connector_configs WHERE owner_id=$1 ORDER BY name,id", string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) ConfigureConnection(ctx context.Context, scope memory.Scope, in connectors.ConfigureRequest) (connectors.Configured, error) {
	out := connectors.Configured{}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || !oneOf(in.Kind, "webhook", "poll", "folder") || in.IntervalSeconds < 15 || in.IntervalSeconds > 86400 {
		return out, memory.ErrInvalid
	}
	if in.Kind == "poll" {
		u, e := url.Parse(in.URL)
		if e != nil || !oneOf(u.Scheme, "https", "http") || u.Host == "" || u.User != nil || u.Fragment != "" || len(in.URL) > 4096 {
			return out, memory.ErrInvalid
		}
	} else if in.URL != "" || in.TokenEnv != "" {
		return out, memory.ErrInvalid
	}
	if in.TokenEnv != "" && (!strings.HasPrefix(in.TokenEnv, "PCAS_CONNECTOR_") || strings.ContainsAny(in.TokenEnv, " \n\r=\x00")) {
		return out, memory.ErrInvalid
	}
	var hash []byte
	if in.ID == "" {
		in.ID = memory.NewID()
		if in.ExpectedVersion != 0 {
			return out, memory.ErrInvalid
		}
		if in.Kind == "webhook" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return out, err
			}
			out.WebhookToken = hex.EncodeToString(b)
			h := sha256.Sum256([]byte(out.WebhookToken))
			hash = h[:]
		}
	} else if !in.ID.Valid() || in.ExpectedVersion < 1 {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if in.ExpectedVersion == 0 {
			_, err := tx.Exec(ctx, `INSERT INTO connector_configs(owner_id,id,name,kind,url,token_env,token_hash,interval_seconds,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, string(scope.OwnerID), string(in.ID), in.Name, in.Kind, in.URL, in.TokenEnv, hash, in.IntervalSeconds, in.Enabled)
			if err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE connector_configs SET name=$3,url=$4,token_env=$5,interval_seconds=$6,enabled=$7,version=version+1,lease_token=NULL,lease_until=NULL,next_sync=now(),status='idle' WHERE owner_id=$1 AND id=$2 AND version=$8 AND kind=$9`, string(scope.OwnerID), string(in.ID), in.Name, in.URL, in.TokenEnv, in.IntervalSeconds, in.Enabled, in.ExpectedVersion, in.Kind)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return memory.ErrConflict
			}
		}
		var err error
		out.Connection, err = scanConnection(tx.QueryRow(ctx, "SELECT "+connectionColumns+" FROM connector_configs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(in.ID)))
		return err
	})
	return out, err
}
func (s *Store) SyncConnection(ctx context.Context, scope memory.Scope, id memory.ID, version int) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	if !id.Valid() {
		return memory.ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE connector_configs SET next_sync=now(),status='queued' WHERE owner_id=$1 AND id=$2 AND version=$3 AND enabled AND kind IN ('poll','folder')`, string(scope.OwnerID), string(id), version)
	if err == nil && tag.RowsAffected() != 1 {
		return memory.ErrConflict
	}
	return err
}
func (s *Store) AuthenticateConnection(ctx context.Context, id memory.ID, token string) (memory.Scope, error) {
	scope := memory.Scope{}
	if !id.Valid() || len(token) != 64 {
		return scope, memory.ErrForbidden
	}
	var hash []byte
	err := s.pool.QueryRow(ctx, "SELECT owner_id::text,token_hash FROM connector_configs WHERE id=$1 AND enabled AND kind='webhook'", string(id)).Scan(&scope.OwnerID, &hash)
	h := sha256.Sum256([]byte(token))
	if err != nil || subtle.ConstantTimeCompare(hash, h[:]) != 1 {
		return memory.Scope{}, memory.ErrForbidden
	}
	scope.IsOwner = true
	scope.PrincipalID = "connector:" + string(id)
	return scope, nil
}
func (s *Store) ImportBatch(ctx context.Context, scope memory.Scope, id memory.ID, batch connectors.Batch) (connectors.Result, error) {
	out := connectors.Result{Refs: []memory.Ref{}, Gaps: []string{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !id.Valid() || len(batch.Records) > 100 {
		return out, memory.ErrInvalid
	}
	batch, err := connectors.Normalize(batch)
	if err != nil {
		return out, memory.ErrInvalid
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var enabled bool
		if err := tx.QueryRow(ctx, "SELECT enabled FROM connector_configs WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), string(id)).Scan(&enabled); err != nil {
			return memory.ErrNotFound
		}
		if !enabled {
			return memory.ErrForbidden
		}
		var e error
		out, e = s.importBatchTx(ctx, tx, scope, "connection:"+string(id), batch, nil)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE connector_configs SET last_sync=now(),status='idle',error_code='',imported=imported+$3,gaps=$4 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id), out.Imported, asJSON(out.Gaps))
		return e
	})
	return out, err
}
func (s *Store) importBatchTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, namespace string, batch connectors.Batch, archive *memory.Ref) (connectors.Result, error) {
	out := connectors.Result{Refs: []memory.Ref{}, Gaps: append([]string{}, batch.Gaps...)}
	if err := s.ensureOwner(ctx, tx, scope); err != nil {
		return out, err
	}
	type episodeBatch struct {
		Title string
		Refs  []memory.Ref
	}
	episodes := map[string]*episodeBatch{}
	for _, r := range batch.Records {
		result, err := s.ingestTx(ctx, tx, scope, archiveIngestRequest(namespace, r))
		if errors.Is(err, memory.ErrBlocked) {
			out.Blocked++
			continue
		}
		if err != nil {
			return out, err
		}
		out.Refs = append(out.Refs, result.Ref)
		if result.Duplicate {
			conversation := ""
			if r.ConversationID != "" {
				conversation = namespace + ":" + r.ConversationID
			}
			gaps := r.MissingAttachments
			if gaps == nil {
				gaps = []string{}
			}
			var matches bool
			err := tx.QueryRow(ctx, `SELECT conversation_key=$4 AND parent_key=$5 AND role=$6 AND branch=$7 AND gaps=$8::jsonb FROM source_contexts WHERE owner_id=$1 AND source_id=$2 AND source_version=$3`, string(scope.OwnerID), string(result.ID), result.Version, conversation, r.ParentID, r.Role, r.Branch, asJSON(gaps)).Scan(&matches)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return out, err
			}
			if err == nil && !matches {
				return out, memory.ErrConflict
			}
		}
		if archive != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO archive_entries(owner_id,archive_id,archive_version,source_id,source_version) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, string(scope.OwnerID), string(archive.ID), archive.Version, string(result.ID), result.Version); err != nil {
				return out, err
			}
		}
		if result.Duplicate {
			out.Duplicates++
			continue
		}
		out.Imported++
		conversation := ""
		if r.ConversationID != "" {
			conversation = namespace + ":" + r.ConversationID
		}
		gaps := r.MissingAttachments
		if gaps == nil {
			gaps = []string{}
		}
		_, err = tx.Exec(ctx, `INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,parent_key,role,branch,gaps) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, string(scope.OwnerID), string(result.ID), result.Version, conversation, r.ParentID, r.Role, r.Branch, asJSON(gaps))
		if err != nil {
			return out, err
		}
		if archive != nil {
			_, err = tx.Exec(ctx, `UPDATE source_versions SET representation='extracted',derived_from_id=$4,derived_from_version=$5 WHERE owner_id=$1 AND source_id=$2 AND version=$3`, string(scope.OwnerID), string(result.ID), result.Version, string(archive.ID), archive.Version)
			if err != nil {
				return out, err
			}
		}
		episodeKey := r.EpisodeKey
		if episodeKey != "" {
			episodeKey = "explicit:" + episodeKey
		} else {
			episodeKey = conversation
		}
		if episodeKey != "" {
			if episodes[episodeKey] == nil {
				episodes[episodeKey] = &episodeBatch{Title: r.EpisodeTitle}
			}
			episodes[episodeKey].Refs = append(episodes[episodeKey].Refs, result.Ref)
		}
	}
	for key, e := range episodes {
		if err := s.linkEpisodeTx(ctx, tx, scope, key, e.Title, e.Refs); err != nil {
			return out, err
		}
	}
	if out.Blocked > 0 {
		out.Gaps = append(out.Gaps, "已删除的来源被阻止重新导入")
	}
	return out, nil
}

func archiveIngestRequest(namespace string, r connectors.Record) memory.IngestRequest {
	return memory.IngestRequest{Connector: namespace, ExternalID: r.ID, ExternalVersion: r.Version, Title: r.Title, Text: r.Text, MediaType: r.MediaType, ExpressedAt: r.ExpressedAt}
}

func (s *Store) linkEpisodeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, key, title string, refs []memory.Ref) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", string(scope.OwnerID)+":episode:"+key); err != nil {
		return err
	}
	var id string
	var version int
	err := tx.QueryRow(ctx, `SELECT e.episode_id::text,r.version FROM episode_keys e JOIN memory_records r ON (r.owner_id,r.id)=(e.owner_id,e.episode_id) WHERE e.owner_id=$1 AND e.external_key=$2 AND r.state='active'`, string(scope.OwnerID), key).Scan(&id, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		id = string(memory.NewID())
		version = 1
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'episode',1)", []any{string(scope.OwnerID), id}},
			{"INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,1)", []any{string(scope.OwnerID), id}},
			{"INSERT INTO episodes(owner_id,id,version,title) VALUES($1,$2,1,$3)", []any{string(scope.OwnerID), id, title}},
			{"INSERT INTO episode_keys(owner_id,external_key,episode_id) VALUES($1,$2,$3)", []any{string(scope.OwnerID), key, id}},
		} {
			if _, err = tx.Exec(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	// Each membership revision retains the previous episode version for history.
	if version > 0 {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM episode_members WHERE owner_id=$1 AND episode_id=$2 AND episode_version=$3", string(scope.OwnerID), id, version).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			previous := version
			version++
			if _, err := tx.Exec(ctx, "INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,$3)", string(scope.OwnerID), id, version); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO episodes(owner_id,id,version,title) VALUES($1,$2,$3,$4)", string(scope.OwnerID), id, version, title); err != nil {
				return err
			}
			replacements := []string{}
			for _, ref := range refs {
				replacements = append(replacements, string(ref.ID))
			}
			if _, err := tx.Exec(ctx, `INSERT INTO episode_members(owner_id,episode_id,episode_version,member_id,member_version) SELECT owner_id,episode_id,$4,member_id,member_version FROM episode_members WHERE owner_id=$1 AND episode_id=$2 AND episode_version=$3 AND NOT(member_id=ANY($5::uuid[]))`, string(scope.OwnerID), id, previous, version, replacements); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE memory_records SET version=$3,updated_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, version); err != nil {
				return err
			}
			if err := invalidateTx(ctx, tx, scope, id); err != nil {
				return err
			}
		}
	}
	for _, ref := range refs {
		if _, err = tx.Exec(ctx, `INSERT INTO episode_members(owner_id,episode_id,episode_version,member_id,member_version) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, string(scope.OwnerID), id, version, string(ref.ID), ref.Version); err != nil {
			return err
		}
	}
	return enqueue(ctx, tx, scope.OwnerID, memory.ID(id), version, "memory.index")
}
func (s *Store) ImportArchive(ctx context.Context, scope memory.Scope, name string, data []byte) (connectors.Result, error) {
	return s.ingestArchive(ctx, scope, name, data, "archive")
}
func (s *Store) ingestArchive(ctx context.Context, scope memory.Scope, name string, data []byte, namespace string) (connectors.Result, error) {
	// The upload default changes; existing folder connector imports keep their
	// automatic organization behavior.
	return s.importArchiveReader(ctx, scope, name, bytes.NewReader(data), namespace, namespace == "archive")
}

var _ connectors.API = (*Store)(nil)

func redactArchivesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, sql := range []string{
		`INSERT INTO blob_cleanup_jobs(owner_id,blob_key) SELECT owner_id,blob_key FROM source_versions WHERE owner_id=$1 AND source_id=ANY($2::uuid[]) AND blob_key IS NOT NULL ON CONFLICT DO NOTHING`,
		`INSERT INTO source_contexts(owner_id,source_id,source_version,gaps) SELECT owner_id,source_id,version,'["原始归档因包含已删除内容而清理；其余独立记录可展开"]'::jsonb FROM source_versions WHERE owner_id=$1 AND source_id=ANY($2::uuid[]) AND NOT attachment_redacted ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET gaps=source_contexts.gaps||excluded.gaps`,
		`UPDATE source_versions SET blob_key=NULL,attachment_redacted=true WHERE owner_id=$1 AND source_id=ANY($2::uuid[])`,
		`UPDATE memory_jobs SET state='done',lease_token=NULL,lease_until=NULL,error_code='archive_redacted' WHERE owner_id=$1 AND record_id=ANY($2::uuid[]) AND stage='source.parse' AND state!='leased'`,
	} {
		if _, err := tx.Exec(ctx, sql, string(scope.OwnerID), ids); err != nil {
			return err
		}
	}
	return nil
}
