-- Drop children first so no CASCADE can reach objects outside this contract.
DROP TABLE IF EXISTS source_scope_assignments;
DROP TABLE IF EXISTS source_scope_revisions;
DROP TABLE IF EXISTS source_authorizations;
DROP TABLE IF EXISTS attempt_typed_dependencies;
DROP TABLE IF EXISTS context_attempts;
DROP TABLE IF EXISTS context_artifact_dependencies;

ALTER TABLE run_dependencies DROP COLUMN IF EXISTS memory_kind;
ALTER TABLE derived_dependencies DROP COLUMN IF EXISTS dependency_kind;
ALTER TABLE desk_turns DROP COLUMN IF EXISTS context_task;
ALTER TABLE action_log DROP COLUMN IF EXISTS context_task;
ALTER TABLE action_log DROP COLUMN IF EXISTS context_stale;

DELETE FROM schema_migrations WHERE name IN (
    '022_phase2_context_contract.sql',
    '023_context_policy_undo.sql',
    '024_context_execution_recovery.sql',
    '025_secretary_action_lineage.sql'
);
