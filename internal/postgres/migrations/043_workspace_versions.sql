-- Separate library freshness and snapshot delivery from command concurrency.
ALTER TABLE workspace_owners ADD COLUMN library_revision bigint NOT NULL DEFAULT 1;
ALTER TABLE workspace_owners ADD COLUMN snapshot_revision bigint NOT NULL DEFAULT 1;

CREATE FUNCTION bump_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  UPDATE workspace_owners SET library_revision=library_revision+1
  WHERE owner_id IN(SELECT DISTINCT owner_id FROM new_rows);
 ELSIF TG_OP='DELETE' THEN
  UPDATE workspace_owners SET library_revision=library_revision+1
  WHERE owner_id IN(SELECT DISTINCT owner_id FROM old_rows);
 ELSIF TG_TABLE_NAME='claims' THEN
  UPDATE workspace_owners SET library_revision=library_revision+1
  WHERE owner_id IN(SELECT DISTINCT owner_id FROM (
   SELECT owner_id,id,retired,retired_by,retired_at FROM new_rows
   EXCEPT SELECT owner_id,id,retired,retired_by,retired_at FROM old_rows
  ) changed);
 ELSE
  UPDATE workspace_owners SET library_revision=library_revision+1
  WHERE owner_id IN(SELECT DISTINCT owner_id FROM (
   SELECT * FROM new_rows EXCEPT SELECT * FROM old_rows
  ) changed);
 END IF;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['claims','claim_revisions','claim_mentions','entity_versions','aliases','record_grants','context_exclusions','evidence','deadlines','assistant_requirements'] LOOP
  EXECUTE format('CREATE TRIGGER library_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_library_version()',t);
  EXECUTE format('CREATE TRIGGER library_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_library_version()',t);
  EXECUTE format('CREATE TRIGGER library_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_library_version()',t);
 END LOOP;
END $$;
CREATE FUNCTION bump_record_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND (OLD.state,OLD.version) IS NOT DISTINCT FROM (NEW.state,NEW.version) THEN RETURN NULL; END IF;
 IF OLD.kind IN('claim','entity') OR (OLD.kind='source' AND EXISTS(SELECT 1 FROM evidence WHERE owner_id=OLD.owner_id AND source_id=OLD.id)) THEN
  UPDATE workspace_owners SET library_revision=library_revision+1 WHERE owner_id=OLD.owner_id;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER library_record_update AFTER UPDATE ON memory_records FOR EACH ROW EXECUTE FUNCTION bump_record_library_version();
CREATE TRIGGER library_record_delete AFTER DELETE ON memory_records FOR EACH ROW EXECUTE FUNCTION bump_record_library_version();
CREATE FUNCTION bump_agent_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.document->'memoryKinds',OLD.document->'includeInferred',OLD.document->'enabled') IS DISTINCT FROM
 (NEW.document->'memoryKinds',NEW.document->'includeInferred',NEW.document->'enabled') THEN
  UPDATE workspace_owners SET library_revision=library_revision+1 WHERE owner_id=NEW.owner_id;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER library_agent_update AFTER UPDATE ON workspace_agents FOR EACH ROW EXECUTE FUNCTION bump_agent_library_version();

CREATE FUNCTION bump_workspace_snapshot_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  UPDATE workspace_owners SET snapshot_revision=snapshot_revision+1 WHERE owner_id IN(SELECT DISTINCT owner_id FROM new_rows);
 ELSIF TG_OP='DELETE' THEN
  UPDATE workspace_owners SET snapshot_revision=snapshot_revision+1 WHERE owner_id IN(SELECT DISTINCT owner_id FROM old_rows);
 ELSE
  UPDATE workspace_owners SET snapshot_revision=snapshot_revision+1 WHERE owner_id IN(SELECT DISTINCT owner_id FROM (SELECT * FROM new_rows EXCEPT SELECT * FROM old_rows) changed);
 END IF;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['sources','source_versions','claims','work_items','memory_jobs','workspace_notices','workspace_reviews','source_extractions','capture_candidates','agent_runs','workspace_agents','work_documents','activity','training_samples','background_usage'] LOOP
  IF to_regclass(t) IS NULL THEN CONTINUE; END IF;
  EXECUTE format('CREATE TRIGGER snapshot_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
  EXECUTE format('CREATE TRIGGER snapshot_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
  EXECUTE format('CREATE TRIGGER snapshot_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
 END LOOP;
END $$;

-- Temporal/trust changes affect visible memories even without a content revision.
CREATE FUNCTION bump_temporal_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  UPDATE workspace_owners SET library_revision=library_revision+1 WHERE owner_id IN(
   SELECT DISTINCT n.owner_id FROM new_rows n JOIN memory_records r ON(r.owner_id,r.id)=(n.owner_id,n.record_id)
   WHERE r.kind IN('claim','entity') OR (r.kind='source' AND EXISTS(SELECT 1 FROM evidence e WHERE e.owner_id=r.owner_id AND e.source_id=r.id)));
 ELSE
  UPDATE workspace_owners SET library_revision=library_revision+1 WHERE owner_id IN(
   SELECT DISTINCT n.owner_id FROM (SELECT * FROM new_rows EXCEPT SELECT * FROM old_rows) n
   JOIN memory_records r ON(r.owner_id,r.id)=(n.owner_id,n.record_id)
   WHERE r.kind IN('claim','entity') OR (r.kind='source' AND EXISTS(SELECT 1 FROM evidence e WHERE e.owner_id=r.owner_id AND e.source_id=r.id)));
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER library_temporal_insert AFTER INSERT ON record_versions REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_temporal_library_version();
CREATE TRIGGER library_temporal_update AFTER UPDATE ON record_versions REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_temporal_library_version();
