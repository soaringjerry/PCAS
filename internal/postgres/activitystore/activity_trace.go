package activitystore

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

// executionTraceSQL returns the recorded calls, causal job events, actions,
// deputy runs, and stage outcomes linked by recorded identities to one anchor
// UUID ($2). It never infers links from timestamps.
const executionTraceSQL = `WITH seed AS (
 SELECT $2::uuid AS id
 UNION
 SELECT t.request_id FROM action_log a JOIN desk_turns t ON (t.owner_id,t.id)=(a.owner_id,a.turn_id)
  WHERE a.owner_id=$1 AND a.id=$2 AND t.request_id IS NOT NULL
 UNION
 SELECT c.execution_id FROM model_calls c WHERE c.owner_id=$1 AND c.id=$2
 UNION
 SELECT c.root_execution_id FROM model_calls c WHERE c.owner_id=$1 AND (c.id=$2 OR c.execution_id=$2)
 UNION
 SELECT coalesce(j.root_job_id,j.id) FROM memory_jobs j WHERE j.owner_id=$1 AND j.id=$2
 UNION
 SELECT e.root_id FROM recorded_events e WHERE e.owner_id=$1 AND (e.id=$2 OR e.job_id=$2)
), anchors AS (
 SELECT DISTINCT id FROM seed WHERE id IS NOT NULL
), chain_events AS (
 SELECT e.id,e.type,e.stage,e.object_id,e.object_version,e.trigger_source,e.job_id,e.root_id,e.cause_id,e.chain_depth,e.occurred_at,
        j.state AS job_state,j.error_code AS job_error_code,j.attempts AS job_attempts
 FROM recorded_events e
 LEFT JOIN memory_jobs j ON (j.owner_id,j.id)=(e.owner_id,e.job_id)
 WHERE e.owner_id=$1 AND (e.root_id IN (SELECT id FROM anchors) OR e.job_id IN (SELECT id FROM anchors))
 ORDER BY e.chain_depth,e.occurred_at,e.id
 LIMIT 250
), job_ids AS (
 SELECT id FROM anchors
 UNION
 SELECT job_id FROM chain_events
), calls AS (
 SELECT c.id,c.execution_id,c.root_execution_id,c.causation_id,c.retry_of_id,c.attempt_number,c.attempt_limit,
        c.recovery_state,c.recovery_reason,c.function_name,c.stage,c.provider_id,c.model,
        c.prompt_name,c.instruction_hash,c.schema_name,c.schema_hash,c.context_builder_version,
        c.required_capabilities,c.actual_mode,c.input_manifest,c.outcome,c.error_code,c.accounting_state,
        c.usage_id,c.created_at,c.started_at,c.finished_at,
        coalesce(u.cost,CASE WHEN c.accounting_state='not_reserved' THEN 0 END) AS cost,
        coalesce(u.cost_estimated,false) AS cost_estimated,
        u.input_tokens,u.output_tokens,(to_jsonb(u)->>'duration_ms')::bigint AS duration_ms,
        CASE WHEN c.accounting_state IN ('held','reserved','pending','failed') THEN b.reserved_cost END AS held
 FROM model_calls c
 LEFT JOIN model_usage u ON (u.owner_id,u.id)=(c.owner_id,c.usage_id)
 LEFT JOIN background_usage b ON (b.owner_id,b.id)=(c.owner_id,c.reservation_id)
 WHERE c.owner_id=$1 AND (c.id IN (SELECT id FROM anchors) OR c.execution_id IN (SELECT id FROM job_ids) OR c.root_execution_id IN (SELECT id FROM anchors))
 ORDER BY c.created_at,c.id
 LIMIT 200
), actions AS (
 SELECT a.id,a.source,a.summary,a.created_at,a.undone_at,a.expired_at,a.undo_of,a.turn_id,t.thing_id,
        jsonb_array_length(a.changes) AS changes_count,
        (SELECT jsonb_agg(DISTINCT ch->>'id') FROM jsonb_array_elements(a.changes) ch WHERE ch->>'table'='work_items') AS items
 FROM action_log a
 LEFT JOIN desk_turns t ON (t.owner_id,t.id)=(a.owner_id,a.turn_id)
 WHERE a.owner_id=$1 AND (a.id=$2 OR (t.request_id IS NOT NULL AND t.request_id IN (SELECT id FROM anchors)))
 ORDER BY a.created_at,a.id
 LIMIT 100
), runs AS (
 SELECT r.id,r.thing_id,r.agent_id,r.status,r.created_at,r.document
 FROM agent_runs r
 WHERE r.owner_id=$1 AND r.id IN (SELECT id FROM anchors)
 ORDER BY r.created_at,r.id
 LIMIT 50
), stages AS (
 SELECT e.id,e.at,e.stage,e.outcome,e.reason,e.count,e.job_id
 FROM background_stage_events e
 WHERE e.owner_id=$1 AND e.job_id IS NOT NULL AND e.job_id IN (SELECT id FROM job_ids)
 ORDER BY e.at,e.id
 LIMIT 100
)
SELECT jsonb_build_object(
 'anchorId',$2::text,
 'calls',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'id',id::text,'executionId',execution_id::text,'rootExecutionId',root_execution_id::text,
   'causationId',causation_id::text,'retryOfId',retry_of_id::text,
   'attempt',attempt_number,'attemptLimit',attempt_limit,
   'recoveryState',recovery_state,'recoveryReason',NULLIF(recovery_reason,''),
   'function',function_name,'stage',stage,'provider',provider_id,'model',model,
   'promptName',prompt_name,'instructionHash',instruction_hash,
   'schemaName',schema_name,'schemaHash',schema_hash,
   'contextBuilderVersion',context_builder_version,
   'requiredCapabilities',required_capabilities,'actualMode',actual_mode,'inputManifest',input_manifest,
   'outcome',outcome,'errorCode',NULLIF(error_code,''),'accountingState',accounting_state,
   'usageId',usage_id::text,'cost',cost,'costKnown',cost IS NOT NULL,'costEstimated',NULLIF(cost_estimated,false),
   'heldReservation',held,'inputTokens',input_tokens,'outputTokens',output_tokens,'durationMs',duration_ms,
   'createdAt',created_at,'startedAt',started_at,'finishedAt',finished_at
 )) ORDER BY created_at,id) FROM calls),'[]'::jsonb),
 'events',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'id',id::text,'type',type,'stage',stage,'objectId',object_id::text,'objectVersion',object_version,
   'triggerSource',trigger_source,'jobId',job_id::text,'rootId',root_id::text,'causeId',cause_id::text,
   'chainDepth',chain_depth,'occurredAt',occurred_at,
   'jobState',job_state,'jobErrorCode',NULLIF(job_error_code,''),'jobAttempts',job_attempts
 )) ORDER BY chain_depth,occurred_at,id) FROM chain_events),'[]'::jsonb),
 'actions',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'id',id::text,'source',source,'summary',summary,
   'outcome',CASE WHEN undone_at IS NOT NULL THEN 'undone' ELSE 'executed' END,
   'createdAt',created_at,'undoneAt',undone_at,'expiredAt',expired_at,
   'undoOf',undo_of::text,'turnId',turn_id::text,'thingId',thing_id::text,
   'changes',changes_count,'items',items
 )) ORDER BY created_at,id) FROM actions),'[]'::jsonb),
 'runs',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'id',id::text,'thingId',thing_id::text,'agentId',agent_id,'status',status,
   'kind',NULLIF(document->>'kind',''),'prompt',NULLIF(document->>'prompt',''),
   'error',NULLIF(document->>'error',''),'staleContext',coalesce((document->>'staleContext')::boolean,false),
   'cost',coalesce((document->>'cost')::numeric,0),'createdAt',created_at,
   'finishedAt',NULLIF(document->>'finishedAt','')
 )) ORDER BY created_at,id) FROM runs),'[]'::jsonb),
 'stages',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'id',id::text,'at',at,'stage',stage,'outcome',outcome,'reason',NULLIF(reason,''),'count',count,'jobId',job_id::text
 )) ORDER BY at,id) FROM stages),'[]'::jsonb)
)`

// ExecutionTrace reads recorded calls, causal job events, actions, deputy runs,
// and stage outcomes linked to one execution, call, action, run, or job ID.
func (s *Activity) ExecutionTrace(ctx context.Context, scope memory.Scope, id string) (json.RawMessage, error) {
	if err := kernel.RequireOwner(scope); err != nil {
		return nil, err
	}
	if !memory.ID(id).Valid() {
		return nil, memory.ErrInvalid
	}
	var out json.RawMessage
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		var body []byte
		if err := tx.QueryRow(ctx, executionTraceSQL, string(scope.OwnerID), id).Scan(&body); err != nil {
			return err
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		var calls, actions []json.RawMessage
		_ = json.Unmarshal(result["calls"], &calls)
		_ = json.Unmarshal(result["actions"], &actions)
		gaps := []string{}
		if len(actions) > 0 && len(calls) == 0 {
			gaps = append(gaps, "this action has no recorded execution link to a model call")
		}
		gaps = append(gaps,
			"input_manifest stores reference identities and coverage counts only; private prompt text stays outside the call journal",
		)
		result["gaps"] = kernel.JSON(gaps)
		out = kernel.JSON(result)
		return nil
	})
	return out, err
}
