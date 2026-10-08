-- Project freshness is an append-only journal: user writes never wait on a
-- worker's handover row, and a read never repairs state. Counts remain correct
-- when transactions commit in a different order from their event IDs.
CREATE TABLE project_input_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 owner_id uuid NOT NULL, project_id uuid NOT NULL,
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX project_input_events_project_idx ON project_input_events(owner_id,project_id,id);
CREATE TABLE project_handovers (
 owner_id uuid NOT NULL, project_id uuid NOT NULL,
 body jsonb NOT NULL, written_at timestamptz NOT NULL,
 input_events bigint NOT NULL, input_hash text NOT NULL,
 PRIMARY KEY(owner_id,project_id),
 FOREIGN KEY(owner_id,project_id) REFERENCES work_items ON DELETE CASCADE
);

-- Includes old memberships so removing a scope/group still invalidates its
-- former project. This is structural membership, never semantic matching.
CREATE FUNCTION project_claim_projects(p_owner uuid,p_claim uuid) RETURNS TABLE(project_id uuid) LANGUAGE sql STABLE AS $$
 SELECT DISTINCT w.id FROM work_items w
 WHERE w.owner_id=p_owner AND w.kind='project' AND (
 EXISTS(SELECT 1 FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.scope->>'project_id'=w.id::text)
 OR EXISTS(SELECT 1 FROM entity_versions ev WHERE ev.owner_id=p_owner AND ev.entity_type='project' AND ev.disambiguation->>'work_item_id'=w.id::text AND (
 EXISTS(SELECT 1 FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.subject_id=ev.entity_id)
 OR EXISTS(SELECT 1 FROM claim_mentions m WHERE m.owner_id=p_owner AND m.claim_id=p_claim AND m.entity_id=ev.entity_id))))
$$;
CREATE FUNCTION mark_project_claim(p_owner uuid,p_claim uuid) RETURNS void LANGUAGE sql AS $$
 INSERT INTO project_input_events(owner_id,project_id) SELECT p_owner,project_id FROM project_claim_projects(p_owner,p_claim)
$$;
CREATE FUNCTION project_input_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d jsonb; o uuid; c uuid; t uuid;
BEGIN
 IF TG_OP='UPDATE' AND to_jsonb(OLD) IS NOT DISTINCT FROM to_jsonb(NEW) THEN RETURN NULL; END IF;
 IF TG_TABLE_NAME='claims' AND TG_OP='UPDATE' AND (to_jsonb(OLD)->'retired',to_jsonb(OLD)->'retired_by') IS NOT DISTINCT FROM (to_jsonb(NEW)->'retired',to_jsonb(NEW)->'retired_by') THEN RETURN NULL; END IF;
 IF TG_TABLE_NAME='memory_records' AND TG_OP='UPDATE' AND (to_jsonb(OLD)->'state',to_jsonb(OLD)->'version') IS NOT DISTINCT FROM (to_jsonb(NEW)->'state',to_jsonb(NEW)->'version') THEN RETURN NULL; END IF;
 IF TG_TABLE_NAME='agent_runs' THEN
  IF coalesce(to_jsonb(OLD)->>'status','')<>'done' AND coalesce(to_jsonb(NEW)->>'status','')<>'done' THEN RETURN NULL; END IF;
  IF TG_OP='UPDATE' AND (to_jsonb(OLD)->'document'->'output',to_jsonb(OLD)->'document'->'adopted',to_jsonb(OLD)->'document'->'finishedAt') IS NOT DISTINCT FROM
  (to_jsonb(NEW)->'document'->'output',to_jsonb(NEW)->'document'->'adopted',to_jsonb(NEW)->'document'->'finishedAt') THEN RETURN NULL; END IF;
 END IF;
 FOR d IN SELECT x FROM (SELECT CASE WHEN TG_OP<>'INSERT' THEN to_jsonb(OLD) END x UNION SELECT CASE WHEN TG_OP<>'DELETE' THEN to_jsonb(NEW) END) a WHERE x IS NOT NULL LOOP
  o=(d->>'owner_id')::uuid;
  IF TG_TABLE_NAME='work_items' THEN
   t=CASE WHEN d->>'kind'='project' THEN (d->>'id')::uuid ELSE (d->>'project_id')::uuid END;
   IF t IS NOT NULL THEN INSERT INTO project_input_events(owner_id,project_id) VALUES(o,t); END IF;
  ELSIF TG_TABLE_NAME IN('work_documents','agent_runs') THEN
   INSERT INTO project_input_events(owner_id,project_id) SELECT o,CASE WHEN w.kind='project' THEN w.id ELSE w.project_id END FROM work_items w
   WHERE w.owner_id=o AND w.id=(d->>'thing_id')::uuid AND (w.kind='project' OR w.project_id IS NOT NULL);
  ELSIF TG_TABLE_NAME='entity_versions' THEN
   INSERT INTO project_input_events(owner_id,project_id) SELECT o,w.id FROM work_items w WHERE w.owner_id=o AND w.kind='project' AND w.id::text=d->'disambiguation'->>'work_item_id';
   FOR c IN SELECT claim_id FROM claim_revisions WHERE owner_id=o AND subject_id=(d->>'entity_id')::uuid UNION SELECT claim_id FROM claim_mentions WHERE owner_id=o AND entity_id=(d->>'entity_id')::uuid LOOP PERFORM mark_project_claim(o,c); END LOOP;
  ELSIF TG_TABLE_NAME='memory_records' THEN
   IF d->>'kind'='claim' THEN PERFORM mark_project_claim(o,(d->>'id')::uuid);
   ELSIF d->>'kind'='source' THEN
    FOR c IN SELECT DISTINCT target_id FROM evidence WHERE owner_id=o AND source_id=(d->>'id')::uuid LOOP PERFORM mark_project_claim(o,c); END LOOP;
   ELSIF d->>'kind'='entity' THEN
    FOR c IN SELECT claim_id FROM claim_revisions WHERE owner_id=o AND subject_id=(d->>'id')::uuid UNION SELECT claim_id FROM claim_mentions WHERE owner_id=o AND entity_id=(d->>'id')::uuid LOOP PERFORM mark_project_claim(o,c); END LOOP;
   END IF;
  ELSE
   c=coalesce(d->>'claim_id',d->>'target_id',d->>'id')::uuid;
   PERFORM mark_project_claim(o,c);
  END IF;
 END LOOP;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['work_items','work_documents','agent_runs','claims','claim_revisions','claim_mentions','entity_versions','memory_records','deadlines','evidence'] LOOP
  EXECUTE format('CREATE TRIGGER project_input_change AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION project_input_changed()',t);
 END LOOP;
 FOREACH t IN ARRAY ARRAY['project_handovers','project_input_events'] LOOP
  EXECUTE format('CREATE TRIGGER snapshot_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
  EXECUTE format('CREATE TRIGGER snapshot_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
  EXECUTE format('CREATE TRIGGER snapshot_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
 END LOOP;
END $$;
INSERT INTO project_input_events(owner_id,project_id) SELECT owner_id,id FROM work_items WHERE kind='project';
ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check CHECK(purpose IN
 ('secretary','deputy','answer','extraction','embedding','vision','organize','compare','entity_compare','entity_candidates','card','handover','selector','reader','selfcheck','alias_scan','alias_confirm','query_embedding','transcription','project_handover','effort'));

-- Progress adoption is now a canonical project memory. Undo hides its source;
-- it does not remove shared pre-existing claims or destroy the paid run result.
CREATE TABLE project_adoption_sources (
 owner_id uuid NOT NULL,run_id uuid NOT NULL,action_id text NOT NULL,source_id uuid NOT NULL,
 PRIMARY KEY(owner_id,run_id,action_id),
 FOREIGN KEY(owner_id,run_id) REFERENCES agent_runs ON DELETE CASCADE,
 FOREIGN KEY(owner_id,source_id) REFERENCES sources ON DELETE CASCADE
);
CREATE FUNCTION sync_project_adoption_source() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE adopted text;
BEGIN
 IF TG_OP<>'DELETE' THEN adopted=NEW.document->'adopted'->>'actionId'; END IF;
 UPDATE memory_records r SET state=CASE WHEN m.action_id=adopted THEN 'active' ELSE 'withdrawn' END,updated_at=clock_timestamp()
 FROM project_adoption_sources m WHERE m.owner_id=OLD.owner_id AND m.run_id=OLD.id AND (r.owner_id,r.id)=(m.owner_id,m.source_id)
 AND r.state IS DISTINCT FROM CASE WHEN m.action_id=adopted THEN 'active' ELSE 'withdrawn' END;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
CREATE TRIGGER sync_project_adoption BEFORE UPDATE OR DELETE ON agent_runs FOR EACH ROW EXECUTE FUNCTION sync_project_adoption_source();
