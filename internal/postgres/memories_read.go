package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type memoryReadOptions struct {
	legacy   bool
	query    workspace.MemoryQuery
	id       string
	limit    int
	snapshot time.Time
	before   *memoryCursor
}
type memoryCursor struct {
	At       time.Time `json:"at"`
	ID       string    `json:"id"`
	Snapshot time.Time `json:"snapshot"`
}

const memoryJoins = ` FROM memory_records r JOIN claim_revisions c ON (c.owner_id,c.claim_id)=(r.owner_id,r.id)
 AND c.version=CASE WHEN $4 THEN (SELECT v.version FROM applicable_claim_versions($1,now(),now()) v WHERE v.claim_id=r.id) ELSE r.version END
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
 LEFT JOIN activity a ON (a.owner_id,a.record_id)=(r.owner_id,r.id)`

func memoryWhere(scope memory.Scope, currentOnly bool, opts memoryReadOptions) (string, []any) {
	args := []any{string(scope.OwnerID), scope.IsOwner, scope.PrincipalID, currentOnly}
	clauses := []string{`r.owner_id=$1 AND r.state='active' AND claim_source_is_current($1,c.claim_id,c.version,now())
 AND ($2 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$3))`}
	add := func(expr string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expr, len(args)))
	}
	if opts.id != "" {
		add("r.id=$%d::uuid", opts.id)
	}
	q := opts.query
	if q.Q != "" {
		add("strpos(lower(c.value #>> '{}'),lower($%d))>0", q.Q)
	}
	if q.Entity != "" {
		add("EXISTS(SELECT 1 FROM claim_mentions cm WHERE (cm.owner_id,cm.claim_id,cm.claim_version)=(c.owner_id,c.claim_id,c.version) AND cm.entity_id=$%d::uuid)", q.Entity)
	}
	if q.Nature != "" {
		add("c.nature=$%d", q.Nature)
	}
	if q.Project != "" {
		add("c.scope->>'project_id'=$%d", q.Project)
	}
	if q.Agent != "" {
		add("EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$%d)", q.Agent)
	}
	if q.Epistemic != "" {
		add("CASE WHEN c.confirmation='confirmed' THEN 'confirmed' WHEN c.confirmation='adopted' AND c.acquisition='direct' THEN 'sourced' ELSE 'inferred' END=$%d", q.Epistemic)
	}
	if q.From != "" {
		add("rv.expressed_at>=$%d::timestamptz", q.From)
	}
	if q.To != "" {
		add("rv.expressed_at<$%d::timestamptz", q.To)
	}
	if !opts.snapshot.IsZero() {
		add("r.created_at<=$%d", opts.snapshot)
	}
	if opts.before != nil {
		args = append(args, opts.before.At, opts.before.ID)
		// The established tie order is ascending id within descending update time.
		clauses = append(clauses, fmt.Sprintf("(r.updated_at<$%d OR (r.updated_at=$%d AND r.id>$%d::uuid))", len(args)-1, len(args)-1, len(args)))
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// JSON field access also works before 027 has added event columns. Both
// workspace cards and model callers need the same event metadata.
func memoryEventColumns() string {
	return "(to_jsonb(c)->>'event_from')::timestamptz,(to_jsonb(c)->>'event_to')::timestamptz,coalesce(to_jsonb(c)->>'event_precision','unknown')"
}

func (s *Store) memoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, effective ...bool) ([]workspace.Memory, error) {
	return s.readMemoriesTx(ctx, tx, scope, len(effective) > 0 && effective[0], memoryReadOptions{})
}

func (s *Store) readMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, currentOnly bool, opts memoryReadOptions) ([]workspace.Memory, error) {
	result := []workspace.Memory{}
	where, args := memoryWhere(scope, currentOnly, opts)
	mentionsAvailable := "to_regclass('claim_mentions') IS NOT NULL"
	query := `SELECT r.id::text,c.version,c.nature,c.value #>> '{}',c.confirmation,c.acquisition,coalesce(c.scope->>'project_id',''),
 coalesce(a.last_effective_use_at,r.created_at),coalesce(a.stability,1),coalesce(a.half_life_seconds,2592000),coalesce(a.pinned,false),coalesce(a.reinforcement_limit,8),
 rv.expressed_at,` + memoryEventColumns() + "," + mentionsAvailable + `,
 coalesce(to_jsonb(c)->>'category','unknown'),(to_jsonb(c)->>'durable')::boolean` + memoryJoins + where + ` ORDER BY r.updated_at DESC,r.id`
	if opts.limit > 0 {
		args = append(args, opts.limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	indices := map[string]int{}
	for rows.Next() {
		var m workspace.Memory
		var last time.Time
		var expressed, from, to *time.Time
		var precision string
		var hasMentions bool
		var stability, halfLife float64
		if err := rows.Scan(&m.ID, &m.Version, &m.Kind, &m.Text, &m.Confirmation, &m.Acquisition, &m.ProjectID, &last, &stability, &halfLife, &m.Pinned, &m.ReinforcementLimit, &expressed, &from, &to, &precision, &hasMentions, &m.Category, &m.Durable); err != nil {
			rows.Close()
			return nil, err
		}
		opts.legacy = !hasMentions
		m.Epistemic = "inferred"
		if m.Confirmation == "confirmed" {
			m.Epistemic = "confirmed"
		} else if m.Confirmation == "adopted" && m.Acquisition == "direct" {
			m.Epistemic = "sourced"
		}
		m.HalfLifeDays = halfLife / 86400
		m.LastUsedAt = last.UTC().Format(time.RFC3339Nano)
		m.Exposure = math.Exp2(-math.Max(0, time.Since(last).Seconds()) / (halfLife * stability))
		if m.Pinned {
			m.Exposure = 1
		}
		m.Sources = []workspace.SourceRef{}
		m.Versions = []workspace.MemoryVersion{}
		m.VisibleTo = []string{}
		m.Mentions = []workspace.MemoryMention{}
		m.Groups = []workspace.MemoryGroup{}
		if expressed != nil {
			m.ExpressedAt = expressed.UTC().Format(time.RFC3339Nano)
		}
		if from != nil {
			m.EventFrom = from.Format(time.RFC3339Nano)
		}
		if to != nil {
			m.EventTo = to.Format(time.RFC3339Nano)
		}
		if precision != "unknown" {
			m.EventPrecision = precision
		}
		indices[m.ID] = len(result)
		ids = append(ids, m.ID)
		result = append(result, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return result, nil
	}
	// Evidence is needed by both workspace cards and model callers. Preserve the
	// source deduplication while showing the quoted location, including long-message tails.
	rows, err = tx.Query(ctx, `SELECT DISTINCT ON(e.target_id::text,v.source_id::text,v.version) e.target_id::text,v.source_id::text,v.version,v.title,substring(v.body FROM greatest(0,coalesce((e.locator->>'start_rune')::int,0))+1 FOR 400),rv.recorded_at
 FROM evidence e JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(e.owner_id,e.source_id,e.source_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
 WHERE e.owner_id=$1 AND e.target_id=ANY($2::uuid[]) AND ($3 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=v.owner_id AND g.record_id=v.source_id AND g.principal_id=$4))
 ORDER BY e.target_id::text,v.source_id::text,v.version,coalesce((e.locator->>'start_rune')::int,0)`, string(scope.OwnerID), ids, scope.IsOwner, scope.PrincipalID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var ref workspace.SourceRef
		var at time.Time
		if err := rows.Scan(&id, &ref.SourceID, &ref.Version, &ref.Label, &ref.Excerpt, &at); err != nil {
			rows.Close()
			return nil, err
		}
		ref.At = at.UTC().Format(time.RFC3339Nano)
		i := indices[id]
		result[i].Sources = append(result[i].Sources, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Model callers need mentions below, but not history or grant details.
	if !currentOnly {
		rows, err = tx.Query(ctx, `SELECT c.claim_id::text,rv.recorded_at,rv.actor,c.value #>> '{}',c.reason FROM claim_revisions c JOIN record_versions rv
	 ON(rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version) WHERE c.owner_id=$1 AND c.claim_id=ANY($2::uuid[]) ORDER BY c.claim_id,c.version`, string(scope.OwnerID), ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var v workspace.MemoryVersion
			var at time.Time
			if err := rows.Scan(&id, &at, &v.By, &v.Text, &v.Reason); err != nil {
				rows.Close()
				return nil, err
			}
			v.At = at.UTC().Format(time.RFC3339Nano)
			i := indices[id]
			result[i].Versions = append(result[i].Versions, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(ctx, `SELECT record_id::text,principal_id FROM record_grants WHERE owner_id=$1 AND record_id=ANY($2::uuid[]) ORDER BY record_id,principal_id`, string(scope.OwnerID), ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, agent string
			if err := rows.Scan(&id, &agent); err != nil {
				rows.Close()
				return nil, err
			}
			i := indices[id]
			result[i].VisibleTo = append(result[i].VisibleTo, agent)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if opts.legacy {
		return result, nil
	}

	rows, err = tx.Query(ctx, `SELECT cm.claim_id::text,cm.claim_version,cm.entity_id::text,ev.name,cm.role FROM claim_mentions cm
 JOIN memory_records er ON(er.owner_id,er.id)=(cm.owner_id,cm.entity_id) AND er.state='active'
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version)
 WHERE cm.owner_id=$1 AND cm.claim_id=ANY($2::uuid[]) ORDER BY cm.claim_id,cm.role,ev.name,cm.entity_id`, string(scope.OwnerID), ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var version int
		var mention workspace.MemoryMention
		if err := rows.Scan(&id, &version, &mention.EntityID, &mention.Name, &mention.Role); err != nil {
			rows.Close()
			return nil, err
		}
		i := indices[id]
		if result[i].Version == version {
			result[i].Mentions = append(result[i].Mentions, mention)
		}
	}
	err = rows.Err()
	rows.Close()
	return result, err
}

func validateMemoryQuery(q *workspace.MemoryQuery) error {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 {
		return memory.ErrInvalid
	}
	q.Limit = min(q.Limit, 100)
	if q.Entity != "" && !memory.ID(q.Entity).Valid() || q.Project != "" && !memory.ID(q.Project).Valid() {
		return memory.ErrInvalid
	}
	if q.Nature != "" && !oneOf(q.Nature, "fact", "preference", "decision", "intention", "plan") {
		return memory.ErrInvalid
	}
	if q.Epistemic != "" && !oneOf(q.Epistemic, "confirmed", "sourced", "inferred") {
		return memory.ErrInvalid
	}
	var from, to time.Time
	for _, item := range []struct {
		raw string
		out *time.Time
	}{{q.From, &from}, {q.To, &to}} {
		if item.raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, item.raw)
			if err != nil {
				parsed, err = time.Parse("2006-01-02", item.raw)
			}
			if err != nil {
				return memory.ErrInvalid
			}
			*item.out = parsed
		}
	}
	if !from.IsZero() && !to.IsZero() && !to.After(from) {
		return memory.ErrInvalid
	}
	return nil
}

func (s *Store) ListMemories(ctx context.Context, scope memory.Scope, q workspace.MemoryQuery) (workspace.MemoryPage, error) {
	out := workspace.MemoryPage{Items: []workspace.Memory{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if err := validateMemoryQuery(&q); err != nil {
		return out, err
	}
	opts := memoryReadOptions{query: q, limit: q.Limit + 1}
	if q.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || len(data) > 1024 {
			return out, memory.ErrInvalid
		}
		var cursor memoryCursor
		if strictJSON(data, &cursor) != nil || !memory.ID(cursor.ID).Valid() || cursor.At.IsZero() || cursor.Snapshot.IsZero() {
			return out, memory.ErrInvalid
		}
		opts.before = &cursor
		opts.snapshot = cursor.Snapshot
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if opts.snapshot.IsZero() {
			if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&opts.snapshot); err != nil {
				return err
			}
		}
		countOpts := opts
		countOpts.before = nil
		where, args := memoryWhere(scope, false, countOpts)
		if err := tx.QueryRow(ctx, "SELECT count(*)"+memoryJoins+where, args...).Scan(&out.Total); err != nil {
			return err
		}
		items, err := s.readMemoriesTx(ctx, tx, scope, false, opts)
		if err != nil {
			return err
		}
		if len(items) > q.Limit {
			items = items[:q.Limit]
			last := items[len(items)-1]
			var at time.Time
			if err := tx.QueryRow(ctx, "SELECT updated_at FROM memory_records WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), last.ID).Scan(&at); err != nil {
				return err
			}
			data, err := json.Marshal(memoryCursor{At: at, ID: last.ID, Snapshot: opts.snapshot})
			if err != nil {
				return err
			}
			out.Next = base64.RawURLEncoding.EncodeToString(data)
		}
		out.Items = items
		return nil
	})
	return out, err
}

func (s *Store) GetMemory(ctx context.Context, scope memory.Scope, id string) (workspace.Memory, error) {
	var out workspace.Memory
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(id).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		items, err := s.readMemoriesTx(ctx, tx, scope, false, memoryReadOptions{id: id})
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return memory.ErrNotFound
		}
		out = items[0]
		return nil
	})
	return out, err
}

func (s *Store) MemoryFacets(ctx context.Context, scope memory.Scope) (workspace.MemoryFacets, error) {
	out := workspace.MemoryFacets{People: []workspace.MemoryFacet{}, Places: []workspace.MemoryFacet{}, Groups: []workspace.MemoryGroupFacet{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `WITH counts AS(
 SELECT cm.entity_id,cm.role,ev.name,count(DISTINCT cm.claim_id) AS n FROM claim_mentions cm
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(cm.owner_id,cm.claim_id,cm.claim_version) AND r.state='active'
 JOIN memory_records er ON(er.owner_id,er.id)=(cm.owner_id,cm.entity_id) AND er.state='active'
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version)
 WHERE cm.owner_id=$1 AND cm.role IN('person','place') AND claim_source_is_current($1,cm.claim_id,cm.claim_version,now())
 GROUP BY cm.entity_id,cm.role,ev.name), ranked AS(
 SELECT *,row_number() OVER(PARTITION BY role ORDER BY n DESC,name,entity_id) AS ordinal FROM counts)
 SELECT entity_id::text,name,n,role FROM ranked WHERE ordinal<=50 ORDER BY role,ordinal`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var f workspace.MemoryFacet
		var role string
		if err := rows.Scan(&f.EntityID, &f.Name, &f.Count, &role); err != nil {
			return out, err
		}
		if role == "person" {
			out.People = append(out.People, f)
		} else {
			out.Places = append(out.Places, f)
		}
	}
	return out, rows.Err()
}
