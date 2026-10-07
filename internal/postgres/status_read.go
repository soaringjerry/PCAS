package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const CompareVersion = 2

var CardVersion = 1
var HandoverVersion = 2

var statusFields = []string{"status", "deadline", "decided", "blocker", "next", "preference", "people"}

type statusNoRepairKey struct{}

func (s *Store) HandoverTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (workspace.Handover, error) {
	out := workspace.Handover{}
	var built *time.Time
	err := tx.QueryRow(ctx, `SELECT h.body,h.built_at,h.stale OR h.rule<$2 OR h.input_hash IS DISTINCT FROM library_handover_hash(h.owner_id)
 FROM handovers h WHERE h.owner_id=$1`, string(scope.OwnerID), HandoverVersion).Scan(&out.Body, &built, &out.Stale)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if built != nil {
		out.BuiltAt = built.UTC().Format(time.RFC3339Nano)
	}
	return out, err
}

// The members view is costly; these reads work it out once instead of once per card or item.
const statusMembersOnce = `WITH members AS MATERIALIZED(SELECT key,claim_id,claim_version FROM status_current_members WHERE owner_id=$1),
 sizes AS(SELECT key,count(*) AS n FROM members GROUP BY key)`

const statusEligibleGroups = `SELECT owner_id,key,kind,entity_id,name,count(*) AS members FROM status_current_members GROUP BY owner_id,key,kind,entity_id,name HAVING count(*)>=3`
const statusValidItems = `SELECT i.* FROM status_card_items i JOIN status_current_members m
 ON(m.owner_id,m.key,m.claim_id,m.claim_version)=(i.owner_id,i.key,i.claim_id,i.claim_version)`

func (s *Store) StatusCardIndexTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) ([]workspace.StatusCardRef, error) {
	if _, ok := ctx.Value(useStatusReadKey{}).(useStatusRead); ok {
		return s.useStatusIndexTx(ctx, tx, scope)
	}
	out := []workspace.StatusCardRef{}
	rows, err := tx.Query(ctx, statusMembersOnce+`,items AS(SELECT i.key,count(m.claim_id) AS valid,count(*)-count(m.claim_id) AS invalid FROM status_card_items i
 LEFT JOIN members m ON(m.key,m.claim_id,m.claim_version)=(i.key,i.claim_id,i.claim_version) WHERE i.owner_id=$1 GROUP BY i.key)
 SELECT sc.key,sc.kind,sc.name,coalesce(it.valid,0),sc.built_at,sc.stale OR sc.rule<$2 OR coalesce(it.invalid,0)>0
 FROM status_cards sc JOIN sizes g ON g.key=sc.key AND g.n>=3 LEFT JOIN items it ON it.key=sc.key
 WHERE sc.owner_id=$1 AND sc.built_at IS NOT NULL
 ORDER BY CASE sc.kind WHEN 'self' THEN 0 WHEN 'project' THEN 1 WHEN 'person' THEN 2 WHEN 'topic' THEN 3 ELSE 4 END,sc.name,sc.key`, string(scope.OwnerID), CardVersion)
	if err != nil {
		return out, err
	}
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
		out = append(out, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}

	return out, nil
}

func (s *Store) StatusCardsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, keys []string) ([]workspace.StatusCard, error) {
	if opt, ok := ctx.Value(useStatusReadKey{}).(useStatusRead); ok {
		return s.useStatusCardsTx(ctx, tx, scope, keys, opt.Index)
	}
	out := []workspace.StatusCard{}
	if len(keys) == 0 {
		return out, nil
	}
	index, err := s.StatusCardIndexTx(ctx, tx, scope)
	if err != nil {
		return out, err
	}
	wanted := map[string]bool{}
	for _, k := range keys {
		wanted[k] = true
	}
	type item struct {
		key, field, id, applies string
		version                 int
	}
	entries := []item{}
	ids := []string{}
	rows, err := tx.Query(ctx, `SELECT key,field,claim_id::text,claim_version,applies_to FROM (`+statusValidItems+`) i
 WHERE owner_id=$1 AND key=ANY($2::text[]) ORDER BY key,position,field`, string(scope.OwnerID), keys)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.key, &i.field, &i.id, &i.version, &i.applies); err != nil {
			rows.Close()
			return out, err
		}
		entries = append(entries, i)
		ids = append(ids, i.id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	memories, err := s.readMemoriesTx(ctx, tx, scope, false, memoryReadOptions{ids: ids})
	if err != nil {
		return out, err
	}
	byID := map[string]workspace.Memory{}
	for _, m := range memories {
		trust, err := statusTrustTx(ctx, tx, scope.OwnerID, m)
		if err != nil {
			return out, err
		}
		m.Trust = trust
		byID[m.ID] = m
	}
	for _, ref := range index {
		if !wanted[ref.Key] {
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

func (s *Store) DeadlinesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, now time.Time, limit int) ([]workspace.Deadline, error) {
	out := []workspace.Deadline{}
	if limit <= 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT d.id::text,d.kind,d.at,d.recurrence,d.title,d.time_note,d.claim_id::text FROM deadlines d
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 WHERE d.owner_id=$1 AND r.state='active' AND rv.state='active' AND cl.retired=''
 AND claim_source_is_current(d.owner_id,d.claim_id,d.claim_version,$2)
 AND (d.kind='recurring' OR d.at>=$2) AND ($3 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$4))
 ORDER BY d.at NULLS LAST,d.title,d.id LIMIT $5`, string(scope.OwnerID), now, scope.IsOwner, scope.PrincipalID, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d workspace.Deadline
		var at *time.Time
		if err := rows.Scan(&d.ID, &d.Kind, &at, &d.Recurrence, &d.Title, &d.TimeNote, &d.MemoryID); err != nil {
			return out, err
		}
		if at != nil {
			v := at.UTC().Format(time.RFC3339Nano)
			d.At = &v
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) About(ctx context.Context, scope memory.Scope, key string) (workspace.About, error) {
	out := workspace.About{Cards: []workspace.StatusCard{}, Deadlines: []workspace.Deadline{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out.Handover, err = s.HandoverTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		if key != "" {
			out.Cards, err = s.StatusCardsTx(ctx, tx, scope, []string{key})
		} else {
			var index []workspace.StatusCardRef
			index, err = s.StatusCardIndexTx(ctx, tx, scope)
			for _, ref := range index {
				out.Cards = append(out.Cards, workspace.StatusCard{Key: ref.Key, Kind: ref.Kind, Name: ref.Name, Count: ref.Count, BuiltAt: ref.BuiltAt, Stale: ref.Stale, Fields: []workspace.StatusCardField{}})
			}
		}
		if err != nil {
			return err
		}
		dates, err := s.libraryDeadlinesTx(ctx, tx, scope, workspace.DeadlineQuery{})
		if err != nil {
			return err
		}
		for _, d := range dates.Items {
			note := d.TimeNote
			if d.Expired {
				note = strings.TrimSpace(note + " 已过期，不知是否完成")
			}
			out.Deadlines = append(out.Deadlines, workspace.Deadline{ID: d.ID, Kind: d.Kind, At: d.At, Recurrence: d.Recurrence, Title: d.Title, TimeNote: note, MemoryID: d.MemoryID})
		}
		return tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE sc.built_at IS NOT NULL AND sc.rule>=$2),count(*) FROM (`+statusEligibleGroups+`) g LEFT JOIN status_cards sc USING(owner_id,key) WHERE g.owner_id=$1`, string(scope.OwnerID), CardVersion).Scan(&out.Building.Done, &out.Building.Total)
	})
	return out, err
}
