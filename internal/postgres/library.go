package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var _ workspace.LibraryReader = (*Store)(nil)

func (s *Store) ReadHandover(ctx context.Context, scope memory.Scope) (workspace.Handover, error) {
	out := workspace.Handover{}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { var err error; out, err = s.HandoverTx(ctx, tx, scope); return err })
	return out, err
}

func (s *Store) libraryDeadlinesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, q workspace.DeadlineQuery) (workspace.DeadlineList, error) {
	out := workspace.DeadlineList{Items: []workspace.LibraryDeadline{}}
	// Fix display classification only on a diagnostic copy. Source validity
	// keeps actual time, and production retains the transaction's now().
	var classificationAt *time.Time
	if s.businessClock != nil {
		at := s.businessNow()
		classificationAt = &at
	}
	rows, err := tx.Query(ctx, `SELECT d.id::text,d.kind,d.at,d.recurrence,d.title,d.time_note,d.claim_id::text,
 coalesce(nullif(d.original_text,''),c.value #>> '{}'),d.at<coalesce($3::timestamptz,now())
 FROM deadlines d JOIN memory_records r ON(r.owner_id,r.id,r.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(d.owner_id,d.claim_id,d.claim_version)
 WHERE d.owner_id=$1 AND r.state='active' AND cl.retired='' AND NOT(coalesce(c.scope->>'deadline_completed','false')='true' AND c.scope->>'deadline_completed_version'=c.version::text) AND claim_source_is_current(r.owner_id,r.id,r.version,now())
 AND ($2::boolean IS NULL OR coalesce(d.at<coalesce($3::timestamptz,now()),false)=$2)
 ORDER BY CASE WHEN d.kind='recurring' THEN 0 WHEN d.at IS NULL THEN 2 ELSE 1 END,d.at NULLS LAST,d.id`, string(scope.OwnerID), q.Expired, classificationAt)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d workspace.LibraryDeadline
		var at *time.Time
		var expired *bool
		if err := rows.Scan(&d.ID, &d.Kind, &at, &d.Recurrence, &d.Title, &d.TimeNote, &d.MemoryID, &d.OriginalText, &expired); err != nil {
			return out, err
		}
		d.DateStatus = "unclear"
		if at != nil {
			v := at.UTC().Format(time.RFC3339Nano)
			d.At = &v
			d.DateStatus = "upcoming"
			d.Expired = expired != nil && *expired
			if d.Expired {
				d.DateStatus = "expired_unknown"
			}
		}
		if d.Kind == "recurring" {
			d.DateStatus = "recurring"
		}
		out.Items = append(out.Items, d)
	}
	return out, rows.Err()
}

func (s *Store) ListDeadlines(ctx context.Context, scope memory.Scope, q workspace.DeadlineQuery) (workspace.DeadlineList, error) {
	out := workspace.DeadlineList{Items: []workspace.LibraryDeadline{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { var err error; out, err = s.libraryDeadlinesTx(ctx, tx, scope, q); return err })
	return out, err
}

func (s *Store) assistantRequirementsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (workspace.AssistantRequirementList, error) {
	out := workspace.AssistantRequirementList{Items: []workspace.AssistantRequirement{}}
	rows, err := tx.Query(ctx, `SELECT m.claim_id::text,c.value #>> '{}',coalesce(a.unrestricted,false),coalesce(a.scope,'适用范围待归类')
 FROM status_current_members m JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(m.owner_id,m.claim_id,m.claim_version)
 LEFT JOIN assistant_requirements a ON(a.owner_id,a.claim_id,a.claim_version)=(m.owner_id,m.claim_id,m.claim_version)
 WHERE m.owner_id=$1 AND m.key='self:rule' ORDER BY m.claim_id`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r workspace.AssistantRequirement
		if err := rows.Scan(&r.MemoryID, &r.Text, &r.Unrestricted, &r.Scope); err != nil {
			return out, err
		}
		out.Items = append(out.Items, r)
	}
	return out, rows.Err()
}

func (s *Store) ListAssistantRequirements(ctx context.Context, scope memory.Scope) (workspace.AssistantRequirementList, error) {
	out := workspace.AssistantRequirementList{Items: []workspace.AssistantRequirement{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { var err error; out, err = s.assistantRequirementsTx(ctx, tx, scope); return err })
	return out, err
}

func validLibraryKey(key string) bool {
	if strings.HasPrefix(key, "entity:") {
		return memory.ID(strings.TrimPrefix(key, "entity:")).Valid()
	}
	return oneOf(key, "self:identity", "self:taste", "self:rule", "self:goal")
}

type groupMemoryCursor struct {
	memoryCursor
	Key string `json:"key"`
}

func (s *Store) ListGroupMemories(ctx context.Context, scope memory.Scope, key string, q workspace.GroupMemoryQuery) (workspace.MemoryPage, error) {
	out := workspace.MemoryPage{Items: []workspace.Memory{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !validLibraryKey(key) || q.Limit < 0 || q.Limit > 100 {
		return out, memory.ErrInvalid
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	cursor := groupMemoryCursor{Key: key}
	if q.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || strictJSON(raw, &cursor) != nil || cursor.Key != key || !memory.ID(cursor.ID).Valid() || cursor.At.IsZero() || cursor.Snapshot.IsZero() {
			return out, memory.ErrInvalid
		}
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if cursor.Snapshot.IsZero() {
			if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&cursor.Snapshot); err != nil {
				return err
			}
		}
		ids := []string{}
		rows, err := tx.Query(ctx, `SELECT m.claim_id::text FROM status_current_members m JOIN memory_records r ON(r.owner_id,r.id)=(m.owner_id,m.claim_id)
 WHERE m.owner_id=$1 AND m.key=$2 AND r.updated_at<=$3 ORDER BY r.updated_at DESC,r.id`, string(scope.OwnerID), key, cursor.Snapshot)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		out.Total = len(ids)
		opts := memoryReadOptions{ids: ids, snapshot: cursor.Snapshot, limit: q.Limit + 1}
		if q.Cursor != "" {
			opts.before = &cursor.memoryCursor
		}
		items, err := s.readMemoriesTx(ctx, tx, scope, false, opts)
		if err != nil {
			return err
		}
		if len(items) > q.Limit {
			items = items[:q.Limit]
			last := items[len(items)-1]
			cursor.ID = last.ID
			if err := tx.QueryRow(ctx, "SELECT updated_at FROM memory_records WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), last.ID).Scan(&cursor.At); err != nil {
				return err
			}
			raw, err := json.Marshal(cursor)
			if err != nil {
				return err
			}
			out.Next = base64.RawURLEncoding.EncodeToString(raw)
		}
		out.Items = items
		return nil
	})
	return out, err
}

func (s *Store) memoryGroupsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (workspace.MemoryGroupList, error) {
	out := workspace.MemoryGroupList{Items: []workspace.LibraryMemoryGroup{}}
	rows, err := tx.Query(ctx, `SELECT key,kind,name,count(*) FROM status_current_members WHERE owner_id=$1 GROUP BY key,kind,name
 ORDER BY CASE WHEN key='self:rule' THEN 0 WHEN kind='self' THEN 1 ELSE 2 END,kind,name,key`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var g workspace.LibraryMemoryGroup
		if err := rows.Scan(&g.Key, &g.Kind, &g.Name, &g.Count); err != nil {
			return out, err
		}
		out.Items = append(out.Items, g)
	}
	return out, rows.Err()
}

func (s *Store) ListMemoryGroups(ctx context.Context, scope memory.Scope) (workspace.MemoryGroupList, error) {
	out := workspace.MemoryGroupList{Items: []workspace.LibraryMemoryGroup{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { var err error; out, err = s.memoryGroupsTx(ctx, tx, scope); return err })
	return out, err
}
