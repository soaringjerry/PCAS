package claimstore

import (
	"context"
	"encoding/json"
	"errors"

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

// RememberTx writes a statement. claim.Remember decides; rememberUnit reads
// and writes.
func (s *Claims) RememberTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in Statement) (memory.Ref, error) {
	return claim.Remember(ctx, rememberUnit{store: s, tx: tx, scope: scope}, in)
}

// rememberUnit is the storage of one statement.
type rememberUnit struct {
	store *Claims
	tx    pgx.Tx
	scope memory.Scope
}

func (u rememberUnit) owner() string { return string(u.scope.OwnerID) }

func (u rememberUnit) Original(ctx context.Context, source memory.Ref) (claim.Original, error) {
	var o claim.Original
	err := u.tx.QueryRow(ctx, `SELECT v.body,s.connector,rv.expressed_at,rv.recorded_at FROM source_versions v
		JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id)
		JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
		WHERE v.owner_id=$1 AND v.source_id=$2 AND v.version=$3`, u.owner(), string(source.ID), source.Version).Scan(&o.Body, &o.Connector, &o.ExpressedAt, &o.RecordedAt)
	return o, err
}

func (u rememberUnit) Blocked(ctx context.Context, key [32]byte) (bool, error) {
	var blocked bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM claim_reimport_blocks WHERE owner_id=$1 AND key_hash=$2)", u.owner(), key[:]).Scan(&blocked)
	return blocked, err
}

func (u rememberUnit) Redirected(ctx context.Context, key [32]byte) (memory.Ref, bool, error) {
	ref := memory.Ref{Kind: memory.ClaimKind}
	var id string
	err := u.tx.QueryRow(ctx, "SELECT k.claim_id::text,r.version FROM claim_key_redirects k JOIN memory_records r ON (r.owner_id,r.id)=(k.owner_id,k.claim_id) WHERE k.owner_id=$1 AND k.key_hash=$2 AND r.state='active'", u.owner(), key[:]).Scan(&id, &ref.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.Ref{}, false, nil
	}
	ref.ID = memory.ID(id)
	return ref, err == nil, err
}

func (u rememberUnit) AtQuote(ctx context.Context, source memory.Ref, quote string) (claim.Quoted, bool, error) {
	q := claim.Quoted{Ref: memory.Ref{Kind: memory.ClaimKind}}
	var id string
	err := u.tx.QueryRow(ctx, `SELECT r.id::text,r.version,rv.actor='ai' AND c.confirmation!='confirmed' FROM evidence e
			JOIN memory_records r ON (r.owner_id,r.id)=(e.owner_id,e.target_id)
			JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
			JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
			JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(e.owner_id,e.source_id,e.source_version)
			WHERE e.owner_id=$1 AND e.source_id=$2 AND e.source_version=$3 AND e.stance='supports' AND r.state='active'
			AND substring(v.body FROM (e.locator->>'start_rune')::int+1 FOR (e.locator->>'end_rune')::int-(e.locator->>'start_rune')::int)=$4
			ORDER BY (rv.actor='user') DESC,r.updated_at DESC,r.id LIMIT 1`, u.owner(), string(source.ID), source.Version, quote).Scan(&id, &q.Ref.Version, &q.Editable)
	if errors.Is(err, pgx.ErrNoRows) {
		return claim.Quoted{}, false, nil
	}
	q.Ref.ID = memory.ID(id)
	return q, err == nil, err
}

func (u rememberUnit) Supplement(ctx context.Context, ref memory.Ref, in Statement) error {
	return supplementClaimTx(ctx, u.tx, u.scope.OwnerID, ref, in)
}

func (u rememberUnit) Prior(ctx context.Context, source memory.ID, key [32]byte) (memory.Ref, bool, error) {
	var prior string
	err := u.tx.QueryRow(ctx, `SELECT k.claim_id::text FROM claim_source_keys k WHERE k.owner_id=$1 AND k.source_id=$2 AND k.claim_key=$3`, u.owner(), string(source), stringHex(key[:])).Scan(&prior)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.Ref{}, false, nil
	}
	if err != nil {
		return memory.Ref{}, false, err
	}
	ref := memory.Ref{ID: memory.ID(prior), Kind: memory.ClaimKind}
	if err := u.tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2", u.owner(), prior).Scan(&ref.Version); err != nil {
		return memory.Ref{}, false, err
	}
	return ref, true, nil
}

func (u rememberUnit) Reassert(ctx context.Context, ref memory.Ref, in Statement, start, end int) error {
	locator := kernel.JSON(map[string]int{"start_rune": start, "end_rune": end})
	if _, err := u.tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance)
			SELECT $1,gen_random_uuid(),$2,$3,$4,$5,$6,$7,'supports'
			WHERE NOT EXISTS(SELECT 1 FROM evidence WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND target_id=$4 AND target_version=$5 AND stance='supports')`, u.owner(), string(in.Source.ID), in.Source.Version, string(ref.ID), ref.Version, locator, in.Acquisition); err != nil {
		return err
	}
	if err := sourcestore.InvalidateTx(ctx, u.tx, u.scope, string(ref.ID)); err != nil {
		return err
	}
	_, err := u.tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage)
			VALUES(gen_random_uuid(),$1,$2,$3,'memory.index') ON CONFLICT(owner_id,record_id,record_version,stage)
			DO UPDATE SET state='queued',attempts=0,available_at=now(),error_code='',lease_token=NULL,lease_until=NULL WHERE memory_jobs.state!='leased'`, u.owner(), string(ref.ID), ref.Version)
	return err
}

func (u rememberUnit) Self(ctx context.Context) (memory.ID, error) {
	return SelfEntityTx(ctx, u.tx, u.scope.OwnerID)
}

func (u rememberUnit) Entity(ctx context.Context, kind, name string) (memory.ID, error) {
	return EntityTx(ctx, u.tx, u.scope.OwnerID, kind, name)
}

func (u rememberUnit) SourceEntity(ctx context.Context, name string, source memory.ID) (memory.ID, bool, error) {
	var id string
	err := u.tx.QueryRow(ctx, `SELECT e.entity_id::text FROM entity_versions e JOIN memory_records r ON (r.owner_id,r.id,r.version)=(e.owner_id,e.entity_id,e.version)
		WHERE e.owner_id=$1 AND e.name=$2 AND e.disambiguation->>'source_id'=$3 AND r.state='active' ORDER BY r.created_at LIMIT 1`, u.owner(), name, string(source)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return memory.ID(id), err == nil, err
}

func (u rememberUnit) NewSourceEntity(ctx context.Context, name string, source memory.ID, actor string) (memory.ID, error) {
	id := memory.NewID()
	if err := CreateRecord(ctx, u.tx, u.scope.OwnerID, id, memory.EntityKind, actor); err != nil {
		return "", err
	}
	if _, err := u.tx.Exec(ctx, "INSERT INTO entities(owner_id,id) VALUES($1,$2)", u.owner(), string(id)); err != nil {
		return "", err
	}
	if _, err := u.tx.Exec(ctx, "INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name,disambiguation) VALUES($1,$2,1,'unknown',$3,$4)", u.owner(), string(id), name, kernel.JSON(map[string]string{"source_id": string(source)})); err != nil {
		return "", err
	}
	_, err := u.tx.Exec(ctx, "INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)", u.owner(), string(id), name)
	return id, err
}

func (u rememberUnit) Create(ctx context.Context, in Statement, subject memory.ID, start, end int, key [32]byte) (memory.Ref, error) {
	id := memory.NewID()
	ref := memory.Ref{ID: id, Version: 1, Kind: memory.ClaimKind}
	if err := CreateRecord(ctx, u.tx, u.scope.OwnerID, id, memory.ClaimKind, in.Actor); err != nil {
		return ref, err
	}
	if _, err := u.tx.Exec(ctx, "UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2 AND version=1", u.owner(), string(id), in.ExpressedAt); err != nil {
		return ref, err
	}
	if _, err := u.tx.Exec(ctx, "INSERT INTO claims(owner_id,id) VALUES($1,$2)", u.owner(), string(id)); err != nil {
		return ref, err
	}
	value, _ := json.Marshal(in.Text)
	scopeJSON, _ := json.Marshal(claim.Scope(in))
	if _, err := u.tx.Exec(ctx, `INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,scope,nature,acquisition,confirmation,change_type)
		VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,'initial')`, u.owner(), string(id), string(subject), in.Predicate, value, scopeJSON, in.Nature, in.Acquisition, in.Confirmation); err != nil {
		return ref, err
	}
	if err := supplementClaimTx(ctx, u.tx, u.scope.OwnerID, ref, in); err != nil {
		return ref, err
	}
	locator, _ := json.Marshal(map[string]int{"start_rune": start, "end_rune": end})
	if _, err := u.tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,1,$6,$7,'supports')`, u.owner(), string(memory.NewID()), string(in.Source.ID), in.Source.Version, string(id), locator, in.Acquisition); err != nil {
		return ref, err
	}
	// A memory was last brought up when it was said, not when it was filed:
	// history imported today must not look as fresh as something said today.
	if _, err := u.tx.Exec(ctx, "INSERT INTO activity(owner_id,record_id,last_effective_use_at) VALUES($1,$2,least(now(),coalesce($3,now())))", u.owner(), string(id), in.ExpressedAt); err != nil {
		return ref, err
	}
	if _, err := u.tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,$2,id FROM workspace_agents WHERE owner_id=$1 AND (document->>'enabled')::boolean`, u.owner(), string(id)); err != nil {
		return ref, err
	}
	if _, err := u.tx.Exec(ctx, "INSERT INTO claim_source_keys(owner_id,source_id,claim_key,claim_id) VALUES($1,$2,$3,$4)", u.owner(), string(in.Source.ID), stringHex(key[:]), string(id)); err != nil {
		return ref, err
	}
	if err := kernel.Enqueue(ctx, u.tx, u.scope.OwnerID, id, 1, "memory.index"); err != nil {
		return ref, err
	}
	return ref, memorystore.RefreshSummaryJobsTx(ctx, u.tx, u.scope.OwnerID, in.Source.ID)
}

func (u rememberUnit) Confirm(ctx context.Context, ref memory.Ref, title, text string) error {
	receipt, err := u.store.IngestTx(ctx, u.tx, u.scope, memory.IngestRequest{Connector: "memory-input", ExternalID: string(ref.ID), ExternalVersion: "1", Title: title, Text: text, MediaType: "text/plain"})
	if err != nil {
		return err
	}
	_, err = u.tx.Exec(ctx, "INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,1,'{}','direct','supports')", u.owner(), string(memory.NewID()), string(receipt.ID), receipt.Version, string(ref.ID))
	return err
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
