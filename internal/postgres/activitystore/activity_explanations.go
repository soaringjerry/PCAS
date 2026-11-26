package activitystore

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

// activityExplanationsSQL reads currently failed/blocked jobs (with any
// terminal recovery pause from model_calls) alongside recent automatic
// topic-project decisions (including skips) and date-review decisions.
const activityExplanationsSQL = `WITH paused AS (
 SELECT j.id,j.stage,split_part(j.stage,':',1) AS base_stage,j.record_id,j.record_version,
        j.state,j.error_code,j.attempts,j.chain_depth,j.root_job_id,j.cause_job_id,j.created_at,j.updated_at,
        mc.recovery_state,mc.recovery_reason,mc.attempt_number,mc.attempt_limit
 FROM memory_jobs j
 LEFT JOIN LATERAL (
   SELECT c.recovery_state,c.recovery_reason,c.attempt_number,c.attempt_limit
   FROM model_calls c
   WHERE c.owner_id=j.owner_id AND c.execution_id=j.id AND c.recovery_state<>'replaced'
   ORDER BY c.created_at DESC,c.id DESC
   LIMIT 1
 ) mc ON true
 WHERE j.owner_id=$1 AND j.state IN ('failed','blocked')
 ORDER BY j.updated_at DESC,j.id DESC
 LIMIT 100
), topics AS (
 SELECT c.topic_id,coalesce(ev.name,'') AS topic_name,coalesce(ev.entity_type,'topic') AS topic_kind,
        c.page,coalesce(c.output->>'decision','') AS decision,coalesce(c.output->>'reason','') AS reason,
        coalesce(c.output->>'name','') AS proposed_name,
        coalesce(l.project_id::text,NULLIF(c.output->>'projectId','')) AS project_id,
        l.action_id,coalesce(l.created_project,false) AS created_project,l.undone_at,l.created_at AS linked_at
 FROM topic_project_checks c
 LEFT JOIN memory_records r ON (r.owner_id,r.id)=(c.owner_id,c.topic_id)
 LEFT JOIN entity_versions ev ON (ev.owner_id,ev.entity_id,ev.version)=(r.owner_id,r.id,r.version)
 LEFT JOIN topic_project_links l ON (l.owner_id,l.topic_id,l.input_hash)=(c.owner_id,c.topic_id,c.input_hash)
 WHERE c.owner_id=$1
 ORDER BY l.created_at DESC NULLS LAST,c.topic_id,c.page DESC
 LIMIT 50
), dates AS (
 SELECT d.claim_id,d.claim_version,d.class,d.decision,d.reason,d.entry_title,d.title,d.item_id,d.action_id,d.created_at
 FROM date_tidy_checks d
 WHERE d.owner_id=$1
 ORDER BY d.created_at DESC,d.claim_id
 LIMIT 50
)
SELECT jsonb_build_object(
 'pausedJobs',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'jobId',id::text,'stage',stage,'baseStage',base_stage,'recordId',record_id::text,'recordVersion',record_version,
   'state',state,'errorCode',NULLIF(error_code,''),'attempts',attempts,
   'chainDepth',chain_depth,'rootJobId',root_job_id::text,'causeJobId',cause_job_id::text,
   'createdAt',created_at,'updatedAt',updated_at,
   'recoveryState',recovery_state,'recoveryReason',NULLIF(recovery_reason,''),
   'attempt',attempt_number,'attemptLimit',attempt_limit,
   'requiresOwnerRecovery',(error_code='chain_limit' OR coalesce(recovery_state,'') IN ('exhausted','budget_exhausted'))
 )) ORDER BY updated_at DESC,id DESC) FROM paused),'[]'::jsonb),
 'topicDecisions',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'topicId',topic_id::text,'topicName',NULLIF(topic_name,''),'topicKind',topic_kind,
   'page',page,'decision',decision,'reason',NULLIF(reason,''),'proposedName',NULLIF(proposed_name,''),
   'projectId',project_id,'actionId',action_id::text,'createdProject',created_project,
   'undoneAt',undone_at,'linkedAt',linked_at
 )) ORDER BY linked_at DESC NULLS LAST,topic_id,page DESC) FROM topics),'[]'::jsonb),
 'dateDecisions',coalesce((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
   'claimId',claim_id::text,'claimVersion',claim_version,'class',class,'decision',decision,
   'reason',NULLIF(reason,''),'entryTitle',NULLIF(entry_title,''),'title',NULLIF(title,''),
   'itemId',item_id::text,'actionId',action_id::text,'createdAt',created_at
 )) ORDER BY created_at DESC,claim_id) FROM dates),'[]'::jsonb)
)`

// ActivityExplanations returns failed/blocked background jobs (including
// recovery-exhausted and chain-limited pauses) and recent automatic
// topic-project and date-review decisions with their recorded reasons.
func (s *Activity) ActivityExplanations(ctx context.Context, scope memory.Scope) (json.RawMessage, error) {
	if err := kernel.RequireOwner(scope); err != nil {
		return nil, err
	}
	var out json.RawMessage
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, activityExplanationsSQL, string(scope.OwnerID)).Scan(&out)
	})
	return out, err
}
