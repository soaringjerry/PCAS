package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// This transaction hook uses typed reverse lineage, including source evidence
// into independently granted claims. The retained owner source is not erased by
// permission revocation. Existing owner/source gates are held by the caller.
func invalidateTypedContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.ContextInvalidation) (resultErr error) {
	if len(in.RecordIDs) == 0 {
		return nil
	}
	ids := []string{}
	for _, id := range in.RecordIDs {
		if !id.Valid() {
			return memory.ErrInvalid
		}
		ids = append(ids, string(id))
	}
	rows, err := tx.Query(ctx, `WITH RECURSIVE affected(id) AS (
 SELECT unnest($2::uuid[])
 UNION
 SELECT edge.child FROM affected a JOIN (
  SELECT source_id parent,target_id child FROM evidence WHERE owner_id=$1
  UNION SELECT dependency_id,view_id FROM derived_dependencies WHERE owner_id=$1
  UNION SELECT derived_from_id,source_id FROM source_versions WHERE owner_id=$1 AND derived_from_id IS NOT NULL
  UNION SELECT archive_id,source_id FROM archive_entries WHERE owner_id=$1
 ) edge ON edge.parent=a.id
) SELECT id::text FROM affected`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	ids = []string{}
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
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'"); err != nil {
		return err
	}
	// Invalidation side effects are not undoable document edits. Keep the
	// caller's policy mutation buffer while excluding erased generated prose.
	var actionBuffer string
	if err = tx.QueryRow(ctx, "SELECT coalesce(current_setting('pcas.action_changes',true),'')").Scan(&actionBuffer); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('pcas.action_changes','',true)"); err != nil {
		return err
	}
	defer func() {
		_, e := tx.Exec(ctx, "SELECT set_config('pcas.action_changes',$1,true)", actionBuffer)
		if resultErr == nil {
			resultErr = e
		}
	}()
	var recipient any
	if in.Recipient != nil {
		recipient = asJSON(in.Recipient)
	}
	additional, err := purgeSecretaryArtifactsTx(ctx, tx, scope, ids, recipient)
	if err != nil {
		return err
	}
	ids = append(ids, additional...)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "context-diagnostics:"+string(scope.OwnerID)); err != nil {
		return err
	}
	state := "revoked"
	if in.Reason == memory.ContextDeleted {
		state = "deleted"
	}
	// The snapshot is the sole diagnostic body; manifest/dependency metadata is
	// controlled references and codes and survives with an explicit loss reason.
	_, err = tx.Exec(ctx, `UPDATE context_attempts a SET snapshot=NULL,snapshot_bytes=0,snapshot_state=$4,state='invalidated',invalidation_reason=$5
 WHERE owner_id=$1 AND ($3::jsonb IS NULL OR recipient=$3)
 AND EXISTS(SELECT 1 FROM attempt_typed_dependencies d WHERE d.owner_id=a.owner_id AND d.attempt_id=a.id AND d.dependency_id=ANY($2::uuid[]))
 OR (owner_id=$1 AND EXISTS(SELECT 1 FROM action_log l WHERE l.owner_id=a.owner_id AND l.context_stale AND (a.manifest->'desk_actions') ? l.id::text))`, string(scope.OwnerID), ids, recipient, state, string(in.Reason))
	if err != nil {
		return err
	}
	// Legacy consumers had no destination/view lineage. Invalidate their copies
	// conservatively too; current callers with separate authorization regenerate.
	if err = recountContextMetadataTx(ctx, tx, scope.OwnerID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE derived_views SET stale=true WHERE owner_id=$1 AND id=ANY($2::uuid[])`, string(scope.OwnerID), ids); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE agent_runs r SET document=document||jsonb_build_object('staleContext',true,'brief','','output','') WHERE owner_id=$1 AND (
 EXISTS(SELECT 1 FROM run_dependencies d WHERE d.owner_id=r.owner_id AND d.run_id=r.id AND d.memory_id=ANY($2::uuid[])) OR
 EXISTS(SELECT 1 FROM context_artifact_dependencies d WHERE d.owner_id=r.owner_id AND d.parent_kind IN ('run','manual_package') AND d.parent_id=r.id::text AND d.dependency_id=ANY($2::uuid[]) AND ($3::jsonb IS NULL OR d.recipient=$3)))`, string(scope.OwnerID), ids, recipient); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE desk_turns t SET answer='',response=jsonb_set(jsonb_set(jsonb_set(coalesce(response,'{}'::jsonb),'{turn,reply}','"（这条回答依据的记忆已变更）"'::jsonb),'{turn,cards}','[]'::jsonb),'{turn,ask}','null'::jsonb)
 WHERE owner_id=$1 AND (EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(dependencies)='array' THEN dependencies ELSE '[]'::jsonb END) d WHERE d->>'id'=ANY($2::text[])) OR EXISTS(SELECT 1 FROM context_artifact_dependencies d WHERE d.owner_id=t.owner_id AND d.parent_kind='desk_turn' AND d.parent_id=t.id::text AND d.dependency_id=ANY($2::uuid[]) AND ($3::jsonb IS NULL OR d.recipient=$3)))`, string(scope.OwnerID), ids, recipient); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE training_samples t SET stale=true,state='excluded',document=document||jsonb_build_object('stale',true,'state','excluded','prompt','','response','') WHERE owner_id=$1 AND (memory_id=ANY($2::uuid[]) OR run_id IN(SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[])) OR EXISTS(SELECT 1 FROM context_artifact_dependencies d WHERE d.owner_id=t.owner_id AND d.dependency_id=ANY($2::uuid[]) AND ((d.parent_kind='training_sample' AND d.parent_id=t.id::text) OR (d.parent_kind IN('run','manual_package') AND d.parent_id=t.run_id::text))))`, string(scope.OwnerID), ids); err != nil {
		return err
	}
	// Clear generated writing using the existing block ownership repair. It
	// preserves owner originals and prevents Brief/State/artifact bypasses.
	if _, err = purgeArtifactsTx(ctx, tx, scope, ids); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE adopted_artifacts a SET body='' WHERE owner_id=$1 AND (run_id IN(SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[])) OR EXISTS(SELECT 1 FROM context_artifact_dependencies d WHERE d.owner_id=a.owner_id AND d.dependency_id=ANY($2::uuid[]) AND ((d.parent_kind IN('run','manual_package') AND d.parent_id=a.run_id::text) OR (d.parent_kind='artifact' AND d.parent_id=a.artifact_id))))`, string(scope.OwnerID), ids)
	return err
}
