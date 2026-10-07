-- Memory-version triggers updated the owner's row, so every background write
-- queued behind snapshot reads of that row and behind each other. Count the
-- changes in an append-only journal instead, as snapshot delivery already does.
CREATE TABLE workspace_library_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 owner_id uuid NOT NULL
);
CREATE INDEX workspace_library_events_owner_idx ON workspace_library_events(owner_id);

CREATE OR REPLACE FUNCTION bump_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  INSERT INTO workspace_library_events(owner_id) SELECT DISTINCT owner_id FROM new_rows;
 ELSIF TG_OP='DELETE' THEN
  INSERT INTO workspace_library_events(owner_id) SELECT DISTINCT owner_id FROM old_rows;
 ELSIF TG_TABLE_NAME='claims' THEN
  INSERT INTO workspace_library_events(owner_id) SELECT DISTINCT owner_id FROM (
   SELECT owner_id,id,retired,retired_by,retired_at FROM new_rows
   EXCEPT SELECT owner_id,id,retired,retired_by,retired_at FROM old_rows
  ) changed;
 ELSE
  INSERT INTO workspace_library_events(owner_id) SELECT DISTINCT owner_id FROM (
   SELECT * FROM new_rows EXCEPT SELECT * FROM old_rows
  ) changed;
 END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION bump_record_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND (OLD.state,OLD.version) IS NOT DISTINCT FROM (NEW.state,NEW.version) THEN RETURN NULL; END IF;
 IF OLD.kind IN('claim','entity') OR (OLD.kind='source' AND EXISTS(SELECT 1 FROM evidence WHERE owner_id=OLD.owner_id AND source_id=OLD.id)) THEN
  INSERT INTO workspace_library_events(owner_id) VALUES(OLD.owner_id);
 END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION bump_agent_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.document->'memoryKinds',OLD.document->'includeInferred',OLD.document->'enabled') IS DISTINCT FROM
 (NEW.document->'memoryKinds',NEW.document->'includeInferred',NEW.document->'enabled') THEN
  INSERT INTO workspace_library_events(owner_id) VALUES(NEW.owner_id);
 END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION bump_temporal_library_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  INSERT INTO workspace_library_events(owner_id)
   SELECT DISTINCT n.owner_id FROM new_rows n JOIN memory_records r ON(r.owner_id,r.id)=(n.owner_id,n.record_id)
   WHERE r.kind IN('claim','entity') OR (r.kind='source' AND EXISTS(SELECT 1 FROM evidence e WHERE e.owner_id=r.owner_id AND e.source_id=r.id));
 ELSE
  INSERT INTO workspace_library_events(owner_id)
   SELECT DISTINCT n.owner_id FROM (SELECT * FROM new_rows EXCEPT SELECT * FROM old_rows) n
   JOIN memory_records r ON(r.owner_id,r.id)=(n.owner_id,n.record_id)
   WHERE r.kind IN('claim','entity') OR (r.kind='source' AND EXISTS(SELECT 1 FROM evidence e WHERE e.owner_id=r.owner_id AND e.source_id=r.id));
 END IF;
 RETURN NULL;
END $$;

-- A stage that only stopped for the day's budget, or one whose call came back
-- unusable, must not read as healthy.
CREATE OR REPLACE FUNCTION record_background_job_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE outcome text; stage_name text;
BEGIN
 stage_name=split_part(NEW.stage,':',1);
 IF NEW.state='done' AND OLD.state='leased' THEN
  -- The work it was leased for failed in this same write; the slot being
  -- handed back is not a success.
  IF current_setting('pcas.stage_failed',true)=stage_name THEN RETURN NULL; END IF;
  outcome='success';
 ELSIF OLD.state='leased' AND NEW.state='queued' AND NEW.error_code<>'' THEN outcome='deferred';
 ELSIF OLD.state='leased' AND NEW.state IN('blocked','failed') THEN outcome='failure';
 ELSE RETURN NULL; END IF;
 INSERT INTO background_stage_events(owner_id,job_id,stage,outcome,reason)
 VALUES(NEW.owner_id,NEW.id,stage_name,outcome,NEW.error_code);
 IF outcome='deferred' THEN
  INSERT INTO background_job_deferrals(job_id,started_at) VALUES(NEW.id,clock_timestamp()) ON CONFLICT(job_id) DO NOTHING;
  IF NEW.attempts>=OLD.attempts THEN
   INSERT INTO background_stage_events(owner_id,job_id,stage,outcome,reason)
   VALUES(NEW.owner_id,NEW.id,stage_name,'failure',NEW.error_code);
  END IF;
 ELSE DELETE FROM background_job_deferrals WHERE job_id=NEW.id; END IF;
 RETURN NULL;
END $$;

-- New and rewritten tables have no statistics yet; without them the planner
-- picked plans that ran past the background write limit.
ANALYZE memory_jobs; ANALYZE claims; ANALYZE workspace_owners; ANALYZE background_usage; ANALYZE background_markers;
ANALYZE background_stage_events; ANALYZE entity_alias_candidates; ANALYZE entity_alias_receipts; ANALYZE entity_scan_members;
ANALYZE assistant_requirements; ANALYZE deadlines; ANALYZE memory_comparison_members; ANALYZE memory_comparison_batches; ANALYZE memory_group_progress;
