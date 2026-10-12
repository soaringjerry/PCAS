package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// activityDayPage is the default number of events in one response. The live
// instance recorded 3,759 events on 2026-10-09 and 814 on 2026-10-10, most of
// them background stage outcomes. The response gives the number that it
// leaves out, and offset reads the next page. The totals always cover the
// whole day.
const activityDayPage = 500

// activityDaySQL answers, for one owner and one local day: what the system
// did, what it cost, and the recorded reasons. It reads the authoritative
// records and writes nothing.
//
//	$1 owner   $2 day start   $3 day end   $4 limit   $5 offset
//
// Kinds of event:
//
//	call      one model invocation from the call journal, with its settled cost
//	usage     recorded usage that no call record names (written before the gateway)
//	action    one applied change; "undone" when it was reversed later
//	undo      a reversal on the day when it happened, linked to its action
//	run       deputy work that was created on this day
//	stage     a background outcome: success, deferred, failure, or overflow
//	decision  a recorded date review decision, changed or unchanged
//
// Cost is summed from calls and unlinked usage only, one row for each
// invocation. An action is never joined to a cost, so one call that led to
// several actions is counted once. A call that reserved nothing submitted
// nothing and costs zero. Any other call without recorded usage has a null
// cost and costKnown false; its held reservation is shown separately.
const activityDaySQL = `WITH calls AS (
 SELECT c.id,c.execution_id,c.root_execution_id,c.causation_id,c.created_at AS at,c.function_name,c.stage,c.outcome,c.error_code,c.accounting_state,
        c.provider_id,c.model,c.retry_of_id,c.actual_mode->'unsupportedFallback' AS fallback,
        coalesce(u.cost,CASE WHEN c.accounting_state='not_reserved' THEN 0 END) AS cost,coalesce(u.cost_estimated,false) AS cost_estimated,u.input_tokens,u.output_tokens,(to_jsonb(u)->>'duration_ms')::bigint AS duration_ms,
        CASE WHEN c.accounting_state IN ('held','reserved','pending','failed') THEN b.reserved_cost END AS held
 FROM model_calls c
 LEFT JOIN model_usage u ON (u.owner_id,u.id)=(c.owner_id,c.usage_id)
 LEFT JOIN background_usage b ON (b.owner_id,b.id)=(c.owner_id,c.reservation_id)
 WHERE c.owner_id=$1 AND c.created_at>=$2 AND c.created_at<$3
), unlinked AS (
 SELECT u.id,u.at,u.purpose,u.agent_id,u.model,u.cost,u.cost_estimated,u.input_tokens,u.output_tokens,(to_jsonb(u)->>'duration_ms')::bigint AS duration_ms,
        coalesce(u.run_id,u.job_id,(SELECT t.request_id FROM desk_turns t WHERE t.owner_id=u.owner_id AND t.id=u.turn_id)) AS execution_id
 FROM model_usage u
 WHERE u.owner_id=$1 AND u.at>=$2 AND u.at<$3 AND NOT EXISTS(SELECT 1 FROM model_calls c WHERE c.owner_id=u.owner_id AND c.usage_id=u.id)
), events AS (
 SELECT at,'call' AS kind,id::text AS event_id,execution_id::text AS execution_id,root_execution_id::text AS root_execution_id,function_name AS function,outcome,
        stage AS summary,error_code AS reason,cost,cost IS NOT NULL AS cost_known,
        jsonb_strip_nulls(jsonb_build_object('provider',provider_id,'model',model,'accounting',accounting_state,'inputTokens',input_tokens,'outputTokens',output_tokens,
          'durationMs',duration_ms,'costEstimated',NULLIF(cost_estimated,false),'heldReservation',held,'retryOf',retry_of_id,'cause',causation_id,'unsupportedFallback',fallback)) AS evidence
 FROM calls
 UNION ALL
 SELECT at,'usage',id::text,execution_id::text,execution_id::text,purpose,'recorded','','no_call_record',cost,true,
        jsonb_strip_nulls(jsonb_build_object('provider',agent_id,'model',model,'inputTokens',input_tokens,'outputTokens',output_tokens,'durationMs',duration_ms,'costEstimated',NULLIF(cost_estimated,false)))
 FROM unlinked
 UNION ALL
 SELECT a.created_at,'action',a.id::text,t.request_id::text,t.request_id::text,a.source,
        CASE WHEN a.undone_at IS NOT NULL THEN 'undone' ELSE 'executed' END,
        a.summary,'',NULL::numeric,false,
        jsonb_strip_nulls(jsonb_build_object('changes',jsonb_array_length(a.changes),'undoOf',a.undo_of,'undoneAt',a.undone_at,'turn',a.turn_id))
 FROM action_log a LEFT JOIN desk_turns t ON (t.owner_id,t.id)=(a.owner_id,a.turn_id)
 WHERE a.owner_id=$1 AND a.created_at>=$2 AND a.created_at<$3
 UNION ALL
 SELECT a.undone_at,'undo',a.id::text||':undo',t.request_id::text,t.request_id::text,a.source,'executed',a.summary,'',NULL::numeric,false,
        jsonb_build_object('undoOf',a.id,'actionAt',a.created_at)
 FROM action_log a LEFT JOIN desk_turns t ON (t.owner_id,t.id)=(a.owner_id,a.turn_id)
 WHERE a.owner_id=$1 AND a.undone_at>=$2 AND a.undone_at<$3
 UNION ALL
 SELECT r.created_at,'run',r.id::text,r.id::text,r.id::text,'deputy',r.status,coalesce(r.document->>'kind',''),coalesce(r.document->>'error',''),NULL::numeric,false,
        jsonb_strip_nulls(jsonb_build_object('agent',r.agent_id,'thing',r.thing_id))
 FROM agent_runs r WHERE r.owner_id=$1 AND r.created_at>=$2 AND r.created_at<$3
 UNION ALL
 SELECT e.at,'stage',e.id::text,e.job_id::text,e.job_id::text,e.stage,e.outcome,'',e.reason,NULL::numeric,false,jsonb_build_object('count',e.count)
 FROM background_stage_events e WHERE e.owner_id=$1 AND e.at>=$2 AND e.at<$3
 UNION ALL
 SELECT d.created_at,'decision',d.claim_id::text||':'||d.claim_version||':'||d.class,NULL,NULL,'date_tidy',d.decision,d.title,d.reason,NULL::numeric,false,
        jsonb_strip_nulls(jsonb_build_object('claim',d.claim_id,'claimVersion',d.claim_version,'class',d.class,'item',d.item_id,'action',d.action_id))
 FROM date_tidy_checks d WHERE d.owner_id=$1 AND d.created_at>=$2 AND d.created_at<$3
), invocations AS (
 SELECT function_name AS function,outcome,cost,cost_estimated,held,input_tokens,output_tokens FROM calls
 UNION ALL SELECT purpose,'recorded',cost,cost_estimated,NULL,input_tokens,output_tokens FROM unlinked
), page AS (
 SELECT * FROM events ORDER BY at,kind,event_id LIMIT $4 OFFSET $5
)
SELECT jsonb_build_object(
 'events',coalesce((SELECT jsonb_agg(jsonb_build_object('at',at,'kind',kind,'eventId',event_id,'executionId',execution_id,'rootExecutionId',root_execution_id,'function',function,
   'outcome',outcome,'summary',summary,'reason',reason,'cost',cost,'costKnown',cost_known,'evidence',evidence) ORDER BY at,kind,event_id) FROM page),'[]'::jsonb),
 'eventCount',(SELECT count(*) FROM events),
 'kinds',coalesce((SELECT jsonb_object_agg(kind,n) FROM (SELECT kind,count(*) AS n FROM events GROUP BY kind) k),'{}'::jsonb),
 'cost',jsonb_build_object(
   'settled',coalesce((SELECT sum(cost) FROM invocations),0),
   'invocations',(SELECT count(*) FROM invocations),
   'estimatedCostInvocations',(SELECT count(*) FROM invocations WHERE cost_estimated),
   'unknownCostInvocations',(SELECT count(*) FROM invocations WHERE cost IS NULL),
   'heldReservations',coalesce((SELECT sum(held) FROM invocations),0),
   'byFunction',coalesce((SELECT jsonb_agg(jsonb_build_object('function',function,'invocations',n,'returned',returned,'settled',settled,'unknownCost',unknown,'inputTokens',input,'outputTokens',output) ORDER BY settled DESC,function)
     FROM (SELECT function,count(*) AS n,count(*) FILTER(WHERE outcome IN ('returned','recorded')) AS returned,coalesce(sum(cost),0) AS settled,count(*) FILTER(WHERE cost IS NULL) AS unknown,
                  coalesce(sum(input_tokens),0) AS input,coalesce(sum(output_tokens),0) AS output FROM invocations GROUP BY function) f),'[]'::jsonb)))`

// DayActivity returns one local day of the owner's recorded work. An empty
// date selects the current local day.
func (s *Store) DayActivity(ctx context.Context, scope memory.Scope, date string, limit, offset int) (json.RawMessage, error) {
	if err := kernel.RequireOwner(scope); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = activityDayPage
	}
	if offset < 0 {
		return nil, memory.ErrInvalid
	}
	var out json.RawMessage
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		settings, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if errors.Is(err, memory.ErrNotFound) {
			settings.Timezone = "UTC"
		} else if err != nil {
			return err
		}
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return workspace.ErrTimezone
		}
		now := time.Now().In(loc)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		if date != "" {
			if start, err = time.ParseInLocation(time.DateOnly, date, loc); err != nil {
				return memory.ErrInvalid
			}
		}
		var body []byte
		if err := tx.QueryRow(ctx, activityDaySQL, string(scope.OwnerID), start, start.AddDate(0, 0, 1), limit, offset).Scan(&body); err != nil {
			return err
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		var count int
		var events []json.RawMessage
		if err := json.Unmarshal(result["eventCount"], &count); err != nil {
			return err
		}
		if err := json.Unmarshal(result["events"], &events); err != nil {
			return err
		}
		result["date"], result["timezone"] = kernel.JSON(start.Format(time.DateOnly)), kernel.JSON(settings.Timezone)
		result["omittedEvents"] = kernel.JSON(max(count-offset-len(events), 0))
		// These records have no link that the system wrote. The query does not
		// invent one from similar times.
		result["gaps"] = kernel.JSON([]string{
			"an action has no recorded link to the model call that proposed it; both carry the execution identity when the action came from a secretary turn",
			"a background action and a date review decision carry no execution identity",
			"usage written before the call journal has no outcome or reason",
		})
		out = kernel.JSON(result)
		return nil
	})
	return out, err
}
