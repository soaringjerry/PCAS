package claimstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/claim"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memorystore"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type Statement = claim.Statement

func CreateRecord(ctx context.Context, tx pgx.Tx, owner memory.ID, id memory.ID, kind memory.Kind, actor string) error {
	if _, err := tx.Exec(ctx, "INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,1)", string(owner), string(id), string(kind)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO record_versions(owner_id,record_id,version,actor) VALUES($1,$2,1,$3)", string(owner), string(id), actor)
	return err
}

func (s *Claims) RememberTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in Statement) (memory.Ref, error) {
	var result memory.Ref
	in, err := claim.Normalize(in)
	if err != nil {
		return result, err
	}
	var original, connector string
	var expressed *time.Time
	var recorded time.Time
	if err := tx.QueryRow(ctx, `SELECT v.body,s.connector,rv.expressed_at,rv.recorded_at FROM source_versions v
		JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id)
		JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
		WHERE v.owner_id=$1 AND v.source_id=$2 AND v.version=$3`, string(scope.OwnerID), string(in.Source.ID), in.Source.Version).Scan(&original, &connector, &expressed, &recorded); err != nil {
		return result, err
	}
	in.ExpressedAt = claim.ExpressedAt(in.ExpressedAt, expressed, recorded, connector)
	start, end, found := claim.QuoteSpan(original, in.Quote)
	if !found {
		return result, memory.ErrInvalid
	}
	key := claim.Key(in)
	var blocked bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM claim_reimport_blocks WHERE owner_id=$1 AND key_hash=$2)", string(scope.OwnerID), key[:]).Scan(&blocked); err != nil {
		return result, err
	}
	if blocked {
		return result, memory.ErrBlocked
	}
	var redirected string
	err = tx.QueryRow(ctx, "SELECT k.claim_id::text,r.version FROM claim_key_redirects k JOIN memory_records r ON (r.owner_id,r.id)=(k.owner_id,k.claim_id) WHERE k.owner_id=$1 AND k.key_hash=$2 AND r.state='active'", string(scope.OwnerID), key[:]).Scan(&redirected, &result.Version)
	if err == nil {
		result.ID = memory.ID(redirected)
		result.Kind = memory.ClaimKind
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if in.Structured && in.Quote != "" && !in.AllowNewAtQuote {
		var editable bool
		var id string
		err := tx.QueryRow(ctx, `SELECT r.id::text,r.version,rv.actor='ai' AND c.confirmation!='confirmed' FROM evidence e
			JOIN memory_records r ON (r.owner_id,r.id)=(e.owner_id,e.target_id)
			JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
			JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
			JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(e.owner_id,e.source_id,e.source_version)
			WHERE e.owner_id=$1 AND e.source_id=$2 AND e.source_version=$3 AND e.stance='supports' AND r.state='active'
			AND substring(v.body FROM (e.locator->>'start_rune')::int+1 FOR (e.locator->>'end_rune')::int-(e.locator->>'start_rune')::int)=$4
			ORDER BY (rv.actor='user') DESC,r.updated_at DESC,r.id LIMIT 1`, string(scope.OwnerID), string(in.Source.ID), in.Source.Version, in.Quote).Scan(&id, &result.Version, &editable)
		if err == nil {
			result.ID, result.Kind = memory.ID(id), memory.ClaimKind
			if editable {
				if err := supplementClaimTx(ctx, tx, scope.OwnerID, result, in); err != nil {
					return result, err
				}
			}
			return result, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return result, err
		}
	}
	var prior string
	err = tx.QueryRow(ctx, `SELECT k.claim_id::text FROM claim_source_keys k WHERE k.owner_id=$1 AND k.source_id=$2 AND k.claim_key=$3`, string(scope.OwnerID), string(in.Source.ID), stringHex(key[:])).Scan(&prior)
	if err == nil {
		var version int
		if err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), prior).Scan(&version); err != nil {
			return result, err
		}
		// A repeated assertion in a newer source version is fresh evidence for
		// the same claim, not a second claim and not continued reliance on v1.
		locator := kernel.JSON(map[string]int{"start_rune": start, "end_rune": end})
		if _, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance)
			SELECT $1,gen_random_uuid(),$2,$3,$4,$5,$6,$7,'supports'
			WHERE NOT EXISTS(SELECT 1 FROM evidence WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND target_id=$4 AND target_version=$5 AND stance='supports')`, string(scope.OwnerID), string(in.Source.ID), in.Source.Version, prior, version, locator, in.Acquisition); err != nil {
			return result, err
		}
		if err := sourcestore.InvalidateTx(ctx, tx, scope, prior); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage)
			VALUES(gen_random_uuid(),$1,$2,$3,'memory.index') ON CONFLICT(owner_id,record_id,record_version,stage)
			DO UPDATE SET state='queued',attempts=0,available_at=now(),error_code='',lease_token=NULL,lease_until=NULL WHERE memory_jobs.state!='leased'`, string(scope.OwnerID), prior, version); err != nil {
			return result, err
		}
		return memory.Ref{ID: memory.ID(prior), Version: version, Kind: memory.ClaimKind}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	var subject string
	if in.Structured && in.SubjectType != "" {
		var id memory.ID
		if in.SubjectType == "self" {
			id, err = SelfEntityTx(ctx, tx, scope.OwnerID)
		} else {
			id, err = EntityTx(ctx, tx, scope.OwnerID, in.SubjectType, in.Subject)
		}
		if err != nil {
			return result, err
		}
		subject = string(id)
	} else {
		err = tx.QueryRow(ctx, `SELECT e.entity_id::text FROM entity_versions e JOIN memory_records r ON (r.owner_id,r.id,r.version)=(e.owner_id,e.entity_id,e.version)
		WHERE e.owner_id=$1 AND e.name=$2 AND e.disambiguation->>'source_id'=$3 AND r.state='active' ORDER BY r.created_at LIMIT 1`, string(scope.OwnerID), in.Subject, string(in.Source.ID)).Scan(&subject)
		if errors.Is(err, pgx.ErrNoRows) {
			subject = string(memory.NewID())
			if err := CreateRecord(ctx, tx, scope.OwnerID, memory.ID(subject), memory.EntityKind, in.Actor); err != nil {
				return result, err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO entities(owner_id,id) VALUES($1,$2)", string(scope.OwnerID), subject); err != nil {
				return result, err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name,disambiguation) VALUES($1,$2,1,'unknown',$3,$4)", string(scope.OwnerID), subject, in.Subject, kernel.JSON(map[string]string{"source_id": string(in.Source.ID)})); err != nil {
				return result, err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)", string(scope.OwnerID), subject, in.Subject); err != nil {
				return result, err
			}
		} else if err != nil {
			return result, err
		}
	}
	id := memory.NewID()
	if err := CreateRecord(ctx, tx, scope.OwnerID, id, memory.ClaimKind, in.Actor); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2 AND version=1", string(scope.OwnerID), string(id), in.ExpressedAt); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO claims(owner_id,id) VALUES($1,$2)", string(scope.OwnerID), string(id)); err != nil {
		return result, err
	}
	value, _ := json.Marshal(in.Text)
	scopeJSON, _ := json.Marshal(claim.Scope(in))
	acquisition := in.Acquisition
	if _, err := tx.Exec(ctx, `INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,scope,nature,acquisition,confirmation,change_type)
		VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,'initial')`, string(scope.OwnerID), string(id), subject, in.Predicate, value, scopeJSON, in.Nature, acquisition, in.Confirmation); err != nil {
		return result, err
	}
	if err := supplementClaimTx(ctx, tx, scope.OwnerID, memory.Ref{ID: id, Version: 1, Kind: memory.ClaimKind}, in); err != nil {
		return result, err
	}
	locator, _ := json.Marshal(map[string]int{"start_rune": start, "end_rune": end})
	if _, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,1,$6,$7,'supports')`, string(scope.OwnerID), string(memory.NewID()), string(in.Source.ID), in.Source.Version, string(id), locator, acquisition); err != nil {
		return result, err
	}
	if in.Actor == "user" {
		receipt, err := s.IngestTx(ctx, tx, scope, memory.IngestRequest{Connector: "memory-input", ExternalID: string(id), ExternalVersion: "1", Title: claim.ConfirmedTitle, Text: in.Text, MediaType: "text/plain"})
		if err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,1,'{}','direct','supports')", string(scope.OwnerID), string(memory.NewID()), string(receipt.ID), receipt.Version, string(id)); err != nil {
			return result, err
		}
	}
	// A memory was last brought up when it was said, not when it was filed:
	// history imported today must not look as fresh as something said today.
	if _, err := tx.Exec(ctx, "INSERT INTO activity(owner_id,record_id,last_effective_use_at) VALUES($1,$2,least(now(),coalesce($3,now())))", string(scope.OwnerID), string(id), in.ExpressedAt); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,$2,id FROM workspace_agents WHERE owner_id=$1 AND (document->>'enabled')::boolean`, string(scope.OwnerID), string(id)); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO claim_source_keys(owner_id,source_id,claim_key,claim_id) VALUES($1,$2,$3,$4)", string(scope.OwnerID), string(in.Source.ID), stringHex(key[:]), string(id)); err != nil {
		return result, err
	}
	if err := kernel.Enqueue(ctx, tx, scope.OwnerID, id, 1, "memory.index"); err != nil {
		return result, err
	}
	if err := memorystore.RefreshSummaryJobsTx(ctx, tx, scope.OwnerID, in.Source.ID); err != nil {
		return result, err
	}
	return memory.Ref{ID: id, Version: 1, Kind: memory.ClaimKind}, nil
}

func stringHex(value []byte) string {
	const chars = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2] = chars[b>>4]
		out[i*2+1] = chars[b&15]
	}
	return string(out)
}

// Fill structured metadata in place without a new revision or dependency
// invalidation. Callers only use this for new claims or editable AI revisions.
func supplementClaimTx(ctx context.Context, tx pgx.Tx, owner memory.ID, ref memory.Ref, in Statement) error {
	if in.EventPrecision == "" {
		in.EventPrecision = "unknown"
	}
	if _, err := tx.Exec(ctx, `UPDATE claim_revisions SET
		event_precision=CASE WHEN event_from IS NULL AND event_to IS NULL AND $4::timestamptz IS NOT NULL THEN $6 ELSE event_precision END,
		event_from=coalesce(event_from,$4),event_to=coalesce(event_to,$5)
		WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, string(owner), string(ref.ID), ref.Version, in.EventFrom, in.EventTo, in.EventPrecision); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE record_versions SET expressed_at=coalesce(expressed_at,$4) WHERE owner_id=$1 AND record_id=$2 AND version=$3`, string(owner), string(ref.ID), ref.Version, in.ExpressedAt); err != nil {
		return err
	}
	return mentionsTx(ctx, tx, owner, ref, in.Mentions)
}
