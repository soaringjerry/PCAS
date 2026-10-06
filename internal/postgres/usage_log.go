package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// modelUsage mirrors model_usage. References contain identity only, never text.
type modelUsage struct {
	Tier            string
	OwnerID         memory.ID
	ID              memory.ID
	At              time.Time
	Purpose         string
	AgentID         string
	Model           string
	InputTokens     int
	OutputTokens    int
	InputEstimated  bool
	OutputEstimated bool
	CostEstimated   bool
	Cost            float64
	TurnID          string
	RunID           string
	JobID           string
	MemoryRefs      []memory.Ref
	Plan            json.RawMessage
}

// A returned model call has already incurred usage. Persist it independently
// before validating or committing its result, even during caller cancellation.
func (s *Store) recordUsage(ctx context.Context, usage modelUsage) error {
	usageCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return pgx.BeginFunc(usageCtx, s.pool, func(tx pgx.Tx) error {
		return recordUsageTx(usageCtx, tx, usage)
	})
}

func recordUsageTx(ctx context.Context, tx pgx.Tx, usage modelUsage) error {
	refs := usage.MemoryRefs
	if refs == nil {
		refs = []memory.Ref{}
	}
	if usage.ID == "" {
		usage.ID = memory.NewID()
	}
	if usage.At.IsZero() {
		usage.At = time.Now().UTC()
	}
	// Persist only validated group keys; arbitrary plan text never reaches storage.
	_, err := tx.Exec(ctx, `INSERT INTO model_usage
 (owner_id,id,at,purpose,agent_id,model,input_tokens,output_tokens,cost,turn_id,run_id,job_id,memory_refs,tier,plan,input_estimated,output_estimated,cost_estimated)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
 ON CONFLICT (owner_id,id) DO NOTHING`, string(usage.OwnerID), string(usage.ID), usage.At,
		usage.Purpose, nullString(usage.AgentID), usage.Model, usage.InputTokens, usage.OutputTokens,
		usage.Cost, nullString(usage.TurnID), nullString(usage.RunID), nullString(usage.JobID), asJSON(refs), usage.Tier, safeUsePlan(usage.Plan), usage.InputEstimated, usage.OutputEstimated, usage.CostEstimated)
	return err
}

// UsageSummary groups calls using calendar dates in the owner's stored zone.
// Date boundaries are constructed separately, so DST days can be 23 or 25 hours.
func (s *Store) UsageSummary(ctx context.Context, scope memory.Scope, from, to string) (json.RawMessage, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	var out json.RawMessage
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if errors.Is(err, memory.ErrNotFound) {
			settings.Timezone = "UTC"
		} else if err != nil {
			return err
		}
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return workspace.ErrTimezone
		}
		start, end, err := usageDates(from, to, loc, time.Now())
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `WITH totals AS (
   SELECT (at AT TIME ZONE $2)::date AS day,purpose,count(*) AS calls,
    sum(input_tokens) AS input,sum(output_tokens) AS output,sum(cost) AS cost,
 count(*) FILTER(WHERE input_estimated OR output_estimated) AS estimated_tokens, count(*) FILTER(WHERE cost_estimated) AS estimated_cost
   FROM model_usage WHERE owner_id=$1 AND at >= $3 AND at < $4
   GROUP BY 1,2
  ), days AS (
   SELECT day,jsonb_agg(jsonb_build_object('purpose',purpose,'calls',calls,
    'inputTokens',input,'outputTokens',output,'cost',cost,'estimatedTokenCalls',estimated_tokens,'estimatedCostCalls',estimated_cost) ORDER BY purpose) AS purposes
   FROM totals GROUP BY day
  ) SELECT jsonb_build_object('days',coalesce(jsonb_agg(jsonb_build_object(
   'date',day::text,'purposes',purposes) ORDER BY day),'[]'::jsonb)) FROM days`,
			string(scope.OwnerID), settings.Timezone, start, end).Scan(&out)
	})
	return out, err
}

func usageDates(from, to string, loc *time.Location, now time.Time) (time.Time, time.Time, error) {
	today := now.In(loc)
	end := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	start := end.AddDate(0, 0, -29)
	var err error
	if from != "" {
		start, err = time.ParseInLocation(time.DateOnly, from, loc)
		if err != nil {
			return start, end, memory.ErrInvalid
		}
	}
	if to != "" {
		end, err = time.ParseInLocation(time.DateOnly, to, loc)
		if err != nil {
			return start, end, memory.ErrInvalid
		}
	}
	if start.After(end) {
		return start, end, memory.ErrInvalid
	}
	return start, end.AddDate(0, 0, 1), nil
}

type usageCursor struct {
	At time.Time `json:"at"`
	ID memory.ID `json:"id"`
}

type usageCallRef struct {
	memory.Ref
	Text    string `json:"text"`
	Deleted bool   `json:"deleted"`
}

type usageCall struct {
	InputEstimated  bool            `json:"inputEstimated"`
	OutputEstimated bool            `json:"outputEstimated"`
	CostEstimated   bool            `json:"costEstimated"`
	Tier            string          `json:"tier"`
	Plan            json.RawMessage `json:"plan,omitempty"`
	ID              memory.ID       `json:"id"`
	At              time.Time       `json:"at"`
	Purpose         string          `json:"purpose"`
	AgentID         *string         `json:"agentId"`
	Model           string          `json:"model"`
	InputTokens     int             `json:"inputTokens"`
	OutputTokens    int             `json:"outputTokens"`
	Cost            float64         `json:"cost"`
	TurnID          *string         `json:"turnId"`
	RunID           *string         `json:"runId"`
	JobID           *string         `json:"jobId"`
	Refs            []usageCallRef  `json:"refs"`
}

func parseUsageCursor(before string) (usageCursor, error) {
	var cursor usageCursor
	if before == "" {
		return cursor, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(before)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.At.IsZero() || !cursor.ID.Valid() {
		return cursor, memory.ErrInvalid
	}
	return cursor, nil
}

// UsageCalls resolves all reference previews with one set query. The logged
// version stays unchanged, while text comes from the record's current version.
func (s *Store) UsageCalls(ctx context.Context, scope memory.Scope, limit int, before string) (json.RawMessage, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 {
		return nil, memory.ErrInvalid
	}
	if limit > 100 {
		limit = 100
	}
	cursor, err := parseUsageCursor(before)
	if err != nil {
		return nil, err
	}
	items := []usageCall{}
	next := ""
	err = pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text,at,purpose,agent_id,model,input_tokens,output_tokens,cost,
   turn_id::text,run_id::text,job_id::text,memory_refs,tier,plan,input_estimated,output_estimated,cost_estimated FROM model_usage
   WHERE owner_id=$1 AND ($2::timestamptz IS NULL OR (at,id)<($2,$3::uuid))
   ORDER BY at DESC,id DESC LIMIT $4`, string(scope.OwnerID), usageBefore(cursor), nullString(string(cursor.ID)), limit+1)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var call usageCall
			var refs []memory.Ref
			if err := rows.Scan(&call.ID, &call.At, &call.Purpose, &call.AgentID, &call.Model, &call.InputTokens,
				&call.OutputTokens, &call.Cost, &call.TurnID, &call.RunID, &call.JobID, &refs, &call.Tier, &call.Plan, &call.InputEstimated, &call.OutputEstimated, &call.CostEstimated); err != nil {
				rows.Close()
				return err
			}
			call.Refs = make([]usageCallRef, 0, len(refs))
			for _, ref := range refs {
				call.Refs = append(call.Refs, usageCallRef{Ref: ref, Deleted: true})
				ids = append(ids, string(ref.ID))
			}
			items = append(items, call)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(items) > limit {
			items = items[:limit]
			last := items[len(items)-1]
			next = base64.RawURLEncoding.EncodeToString(asJSON(usageCursor{At: last.At, ID: last.ID}))
		}
		if len(ids) == 0 {
			return nil
		}
		rows, err = tx.Query(ctx, `SELECT r.id::text,r.kind,
   CASE r.kind WHEN 'claim' THEN left(c.value #>> '{}',80) WHEN 'source' THEN left(sv.body,80) END
   FROM memory_records r
   JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(r.owner_id,r.id,r.version) AND v.state='active'
   LEFT JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
   LEFT JOIN source_versions sv ON (sv.owner_id,sv.source_id,sv.version)=(r.owner_id,r.id,r.version)
   WHERE r.owner_id=$1 AND r.id=ANY($2::uuid[]) AND r.state='active' AND r.kind IN ('claim','source')`, string(scope.OwnerID), ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		texts := map[string]string{}
		for rows.Next() {
			var id, kind string
			var text *string
			if err := rows.Scan(&id, &kind, &text); err != nil {
				return err
			}
			if text != nil {
				texts[id+":"+kind] = *text
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range items {
			for j := range items[i].Refs {
				ref := &items[i].Refs[j]
				if text, ok := texts[string(ref.ID)+":"+string(ref.Kind)]; ok {
					ref.Text = text
					ref.Deleted = false
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Items []usageCall `json:"items"`
		Next  string      `json:"next"`
	}{Items: items, Next: next})
}

func usageBefore(cursor usageCursor) any {
	if cursor.At.IsZero() {
		return nil
	}
	return cursor.At
}
