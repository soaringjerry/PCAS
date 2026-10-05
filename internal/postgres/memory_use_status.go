package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// This opt-in path keeps the shared read contract, but resolves membership only
// for built cards. The general status view also serves scheduling for unbuilt
// groups; repeatedly expanding that directory during a turn is unnecessary.
type useStatusReadKey struct{}
type useStatusRead struct{ Index []workspace.StatusCardRef }

// Match status_current_members exactly: current stored version, live source,
// active entity, subject OR mention membership, and the four self categories.
// UNION deduplicates subject/mention membership before counting group members.
const useStatusMembers = `WITH candidates AS MATERIALIZED (
 SELECT sc.owner_id,sc.key,cm.claim_id,cm.claim_version FROM status_cards sc
 JOIN memory_records er ON er.owner_id=sc.owner_id AND sc.key='entity:'||er.id::text AND er.kind='entity' AND er.state='active'
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version) AND ev.entity_type IN('project','topic','area','person')
 JOIN claim_mentions cm ON(cm.owner_id,cm.entity_id)=(sc.owner_id,er.id)
 WHERE sc.owner_id=$1 AND sc.built_at IS NOT NULL
 UNION
 SELECT sc.owner_id,sc.key,c.claim_id,c.version FROM status_cards sc
 JOIN memory_records er ON er.owner_id=sc.owner_id AND sc.key='entity:'||er.id::text AND er.kind='entity' AND er.state='active'
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version) AND ev.entity_type IN('project','topic','area','person')
 JOIN claim_revisions c ON(c.owner_id,c.subject_id)=(sc.owner_id,er.id)
 WHERE sc.owner_id=$1 AND sc.built_at IS NOT NULL
 UNION
 SELECT sc.owner_id,sc.key,c.claim_id,c.version FROM status_cards sc
 JOIN claim_revisions c ON c.owner_id=sc.owner_id AND sc.key='self:'||c.category
 WHERE sc.owner_id=$1 AND sc.built_at IS NOT NULL AND c.category IN('identity','taste','rule','goal')
), members AS MATERIALIZED (
 SELECT c.* FROM candidates c
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(c.owner_id,c.claim_id,c.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 WHERE r.state='active' AND rv.state='active' AND cl.retired=''
 AND claim_source_is_current(r.owner_id,r.id,r.version,now())
), eligible AS (SELECT owner_id,key FROM members GROUP BY owner_id,key HAVING count(*)>=3) `

func (s *Store) useStatusIndexTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) ([]workspace.StatusCardRef, error) {
	out := []workspace.StatusCardRef{}
	rows, err := tx.Query(ctx, useStatusMembers+`SELECT sc.key,sc.kind,sc.name,count(m.claim_id),sc.built_at,
 sc.stale OR sc.rule<$2 OR count(i.claim_id)!=count(m.claim_id)
 FROM status_cards sc JOIN eligible e USING(owner_id,key)
 LEFT JOIN status_card_items i ON(i.owner_id,i.key)=(sc.owner_id,sc.key)
 LEFT JOIN members m ON(m.owner_id,m.key,m.claim_id,m.claim_version)=(i.owner_id,i.key,i.claim_id,i.claim_version)
 GROUP BY sc.owner_id,sc.key ORDER BY CASE sc.kind WHEN 'self' THEN 0 WHEN 'project' THEN 1 WHEN 'person' THEN 2 WHEN 'topic' THEN 3 ELSE 4 END,sc.name,sc.key`, string(scope.OwnerID), CardVersion)
	if err != nil {
		return out, err
	}
	// A card rebuilt after this read is fresh again; the repair must not undo that.
	builtAt := map[string]*time.Time{}
	for rows.Next() {
		var ref workspace.StatusCardRef
		var built *time.Time
		if err := rows.Scan(&ref.Key, &ref.Kind, &ref.Name, &ref.Count, &built, &ref.Stale); err != nil {
			rows.Close()
			return out, err
		}
		if built != nil {
			ref.BuiltAt = built.UTC().Format(time.RFC3339Nano)
		}
		builtAt[ref.Key] = built
		out = append(out, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	var readOnly string
	if err := tx.QueryRow(ctx, "SHOW transaction_read_only").Scan(&readOnly); err != nil {
		return out, err
	}
	if readOnly == "off" {
		for _, ref := range out {
			if ref.Stale {
				if _, err := tx.Exec(ctx, "UPDATE status_cards SET stale=true WHERE owner_id=$1 AND key=$2 AND NOT stale AND built_at IS NOT DISTINCT FROM $3", string(scope.OwnerID), ref.Key, builtAt[ref.Key]); err != nil {
					return out, err
				}
			}
		}
	}
	return out, nil
}

func (s *Store) useStatusCardsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, keys []string, index []workspace.StatusCardRef) ([]workspace.StatusCard, error) {
	out := []workspace.StatusCard{}
	if len(keys) == 0 {
		return out, nil
	}
	if index == nil {
		var err error
		index, err = s.useStatusIndexTx(ctx, tx, scope)
		if err != nil {
			return out, err
		}
	}
	type item struct {
		key, field, id, applies string
		version                 int
	}
	entries := []item{}
	eligibleKeys := map[string]bool{}
	ids := []string{}
	rows, err := tx.Query(ctx, useStatusMembers+`SELECT e.key,i.field,i.claim_id::text,i.claim_version,i.applies_to FROM eligible e
 LEFT JOIN (SELECT i.* FROM status_card_items i JOIN members m ON(m.owner_id,m.key,m.claim_id,m.claim_version)=(i.owner_id,i.key,i.claim_id,i.claim_version)) i ON(i.owner_id,i.key)=(e.owner_id,e.key)
 WHERE e.owner_id=$1 AND e.key=ANY($2::text[]) ORDER BY e.key,i.position,i.field`, string(scope.OwnerID), keys)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var key string
		var field, id, applies *string
		var version *int
		if err := rows.Scan(&key, &field, &id, &version, &applies); err != nil {
			rows.Close()
			return out, err
		}
		eligibleKeys[key] = true
		if id != nil {
			entries = append(entries, item{key, *field, *id, *applies, *version})
			ids = append(ids, *id)
		}
	}

	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	ms, err := s.readMemoriesTx(ctx, tx, scope, false, memoryReadOptions{ids: ids})
	if err != nil {
		return out, err
	}
	byID := map[string]workspace.Memory{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	for _, ref := range index {
		if !oneOf(ref.Key, keys...) || !eligibleKeys[ref.Key] {
			continue
		}
		card := workspace.StatusCard{Key: ref.Key, Kind: ref.Kind, Name: ref.Name, BuiltAt: ref.BuiltAt, Stale: ref.Stale, Fields: []workspace.StatusCardField{}}
		for _, field := range statusFields {
			f := workspace.StatusCardField{Field: field, Items: []workspace.Memory{}}
			for _, i := range entries {
				if i.key != ref.Key || i.field != field {
					continue
				}
				m, ok := byID[i.id]
				if !ok || m.Version != i.version {
					continue
				}
				m.AppliesTo = i.applies
				f.Items = append(f.Items, m)
			}
			if len(f.Items) > 0 {
				card.Count += len(f.Items)
				card.Fields = append(card.Fields, f)
			}
		}
		out = append(out, card)
	}
	return out, nil
}
