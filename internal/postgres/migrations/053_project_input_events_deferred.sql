-- Phase 3 follow-up: the per-row project input triggers made every organize
-- and compare write on a few-thousand-memory library take seconds (4.8 s on the
-- 5,000-memory acceptance fixture), past the 2-second background write cap.
-- The triggers now fire once per row at commit, after the statement has
-- settled the claim's groups, and each claim is looked up once per transaction.
CREATE OR REPLACE FUNCTION mark_project_claim(p_owner uuid,p_claim uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 CREATE TEMP TABLE IF NOT EXISTS project_marked_claims(owner_id uuid NOT NULL,claim_id uuid NOT NULL,PRIMARY KEY(owner_id,claim_id)) ON COMMIT DROP;
 INSERT INTO project_marked_claims(owner_id,claim_id) VALUES(p_owner,p_claim) ON CONFLICT DO NOTHING;
 IF NOT FOUND THEN RETURN; END IF;
 INSERT INTO project_input_events(owner_id,project_id) SELECT p_owner,project_id FROM project_claim_projects(p_owner,p_claim);
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['work_items','work_documents','agent_runs','claims','claim_revisions','claim_mentions','entity_versions','memory_records','deadlines','evidence'] LOOP
  EXECUTE format('DROP TRIGGER IF EXISTS project_input_change ON %I',t);
  EXECUTE format('CREATE CONSTRAINT TRIGGER project_input_change AFTER INSERT OR UPDATE OR DELETE ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION project_input_changed()',t);
 END LOOP;
END $$;

-- A recorded undo (phase 3 D5: undoing a revise adoption is itself an action so
-- it can be undone again) must not count as a newer action against the older
-- actions it sits on; continuous reverse undo A -> B -> undo B -> undo A stays
-- possible. The marker says which action an entry undid.
ALTER TABLE action_log ADD COLUMN undo_of uuid;

-- The lookup itself started from every project and scanned entity_versions by a
-- JSON expression for each one (30 projects x ~1,000 entities per claim). Start
-- from the claim instead: its scope, its subject and its mentioned entities are
-- a handful of indexed rows, then map those entities to project work items.
CREATE OR REPLACE FUNCTION project_claim_projects(p_owner uuid,p_claim uuid) RETURNS TABLE(project_id uuid) LANGUAGE sql STABLE AS $$
 SELECT DISTINCT w.id FROM work_items w
 WHERE w.owner_id=p_owner AND w.kind='project' AND w.id::text IN (
  SELECT c.scope->>'project_id' FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.scope->>'project_id' IS NOT NULL
  UNION
  SELECT ev.disambiguation->>'work_item_id' FROM entity_versions ev
  WHERE ev.owner_id=p_owner AND ev.entity_type='project' AND ev.disambiguation->>'work_item_id' IS NOT NULL AND ev.entity_id IN (
   SELECT c.subject_id FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.subject_id IS NOT NULL
   UNION SELECT m.entity_id FROM claim_mentions m WHERE m.owner_id=p_owner AND m.claim_id=p_claim))
$$;
