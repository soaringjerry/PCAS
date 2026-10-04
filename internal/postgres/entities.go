package postgres

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

const entityWhitespace = " \t\n\r\v\f\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

type claimMention struct {
	Name string
	Role string
}

// Serialize entity lookup and creation per owner, including all aliases. This
// protects self and typed names even when callers do not hold the owner row lock.
func entityTx(ctx context.Context, tx pgx.Tx, owner memory.ID, kind, name string) (memory.ID, error) {
	name = strings.TrimSpace(name)
	if name == "" || !oneOf(kind, "self", "person", "place", "organization", "thing", "project", "topic", "area") {
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
		  AND lower(btrim(a.alias,$4))=lower($3))) ORDER BY r.created_at,r.id LIMIT 1`, string(owner), kind, name, entityWhitespace).Scan(&id)
	if err == nil {
		return memory.ID(id), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	ref := memory.NewID()
	if err := createRecord(ctx, tx, owner, ref, memory.EntityKind, "ai"); err != nil {
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

func selfEntityTx(ctx context.Context, tx pgx.Tx, owner memory.ID) (memory.ID, error) {
	return entityTx(ctx, tx, owner, "self", "我")
}

func groundedMentions(names []string, role, text string) []claimMention {
	out := []claimMention{}
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if name == "" || utf8.RuneCountInString(name) > 40 || seen[key] || !strings.Contains(text, name) || memory.AmbiguousEntityName(name) || role == "person" && oneOf(name, "我", "我们") {
			continue
		}
		seen[key] = true
		out = append(out, claimMention{Name: name, Role: role})
		if len(out) == 8 {
			break
		}
	}
	return out
}

func mentionsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, ref memory.Ref, mentions []claimMention) error {
	for _, mention := range mentions {
		id, err := entityTx(ctx, tx, owner, mention.Role, mention.Name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, string(owner), string(ref.ID), ref.Version, string(id), mention.Role); err != nil {
			return err
		}
	}
	return nil
}
