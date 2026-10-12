package claimstore

import (
	"context"
	"errors"
	"strings"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

const EntityWhitespace = " \t\n\r\v\f\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

type claimMention = extraction.Mention

// Serialize entity lookup and creation per owner, including all aliases. This
// protects self and typed names even when callers do not hold the owner row lock.
func EntityTx(ctx context.Context, tx pgx.Tx, owner memory.ID, kind, name string) (memory.ID, error) {
	name = strings.TrimSpace(name)
	if name == "" || !kernel.OneOf(kind, "self", "person", "place", "organization", "thing", "project", "topic", "area") {
		return "", memory.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", string(owner)+":entities"); err != nil {
		return "", err
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT e.entity_id::text FROM entity_versions e
		JOIN memory_records r ON (r.owner_id,r.id,r.version)=(e.owner_id,e.entity_id,e.version)
		WHERE e.owner_id=$1 AND e.entity_type=$2 AND r.state='active'
		AND ($2='self' OR EXISTS(SELECT 1 FROM aliases a WHERE a.owner_id=e.owner_id AND a.entity_id=e.entity_id
		  AND lower(btrim(a.alias,$4))=lower($3))) ORDER BY r.created_at,r.id LIMIT 1`, string(owner), kind, name, EntityWhitespace).Scan(&id)
	if err == nil {
		return memoryread.ResolveMergedEntityTx(ctx, tx, owner, memory.ID(id))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	ref := memory.NewID()
	if err := CreateRecord(ctx, tx, owner, ref, memory.EntityKind, "ai"); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO entities(owner_id,id) VALUES($1,$2)", string(owner), string(ref)); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,$3,$4)", string(owner), string(ref), kind, name); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)", string(owner), string(ref), name); err != nil {
		return "", err
	}
	return ref, nil
}

func SelfEntityTx(ctx context.Context, tx pgx.Tx, owner memory.ID) (memory.ID, error) {
	return EntityTx(ctx, tx, owner, "self", "我")
}

func mentionsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, ref memory.Ref, mentions []claimMention) error {
	for _, mention := range mentions {
		id, err := EntityTx(ctx, tx, owner, mention.Role, mention.Name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, string(owner), string(ref.ID), ref.Version, string(id), mention.Role); err != nil {
			return err
		}
	}
	return nil
}
