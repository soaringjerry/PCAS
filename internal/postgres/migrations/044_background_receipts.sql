-- Preserve every legacy marker before removing bookkeeping from the work queue.
CREATE TABLE background_markers (
 owner_id uuid NOT NULL REFERENCES workspace_owners ON DELETE CASCADE,
 record_id uuid NOT NULL, record_version integer NOT NULL, stage text NOT NULL,
 attempts integer NOT NULL DEFAULT 0, data jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner_id,record_id,record_version,stage)
);
INSERT INTO background_markers(owner_id,record_id,record_version,stage,attempts,data,created_at)
 SELECT owner_id,record_id,record_version,stage,attempts,to_jsonb(j),created_at FROM memory_jobs j
 WHERE state='done' AND (stage LIKE 'memory.entity_result:%'
 OR stage LIKE 'memory.entity_candidates:%:seen:%' OR stage LIKE 'memory.entity_candidates:%:pair:%'
 OR stage LIKE 'memory.compare:%:restored:%' OR stage LIKE 'memory.compare_restored:%'
 OR stage LIKE 'memory.compare:%:attempt%');
CREATE TABLE entity_alias_candidates (
 owner_id uuid NOT NULL REFERENCES workspace_owners ON DELETE CASCADE,
 left_id uuid NOT NULL,right_id uuid NOT NULL,name_hash text NOT NULL,rule integer NOT NULL,
 source_marker text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner_id,left_id,right_id,name_hash,rule),CHECK(left_id<right_id)
);
CREATE TABLE entity_alias_receipts (
 owner_id uuid NOT NULL REFERENCES workspace_owners ON DELETE CASCADE,
 left_id uuid NOT NULL,right_id uuid NOT NULL,name_hash text NOT NULL,rule integer NOT NULL,
 same boolean,output text NOT NULL DEFAULT '',attempts integer NOT NULL DEFAULT 0,
 retry_after timestamptz NOT NULL DEFAULT '-infinity',completed_at timestamptz,
 PRIMARY KEY(owner_id,left_id,right_id,name_hash,rule),CHECK(left_id<right_id)
);
CREATE FUNCTION entity_name_hash(p_owner uuid,p_left uuid,p_right uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT md5(a.name||E'\n'||b.name)
 FROM memory_records ar JOIN entity_versions a ON(a.owner_id,a.entity_id,a.version)=(ar.owner_id,ar.id,ar.version)
 JOIN memory_records br ON br.owner_id=ar.owner_id AND br.id=greatest(p_left,p_right)
 JOIN entity_versions b ON(b.owner_id,b.entity_id,b.version)=(br.owner_id,br.id,br.version)
 WHERE ar.owner_id=p_owner AND ar.id=least(p_left,p_right)
$$;
-- Old receipts did not retain prompt names. Adopt the names at migration as
-- their baseline and preserve the original row losslessly in background_markers.
INSERT INTO entity_alias_receipts(owner_id,left_id,right_id,name_hash,rule,same,completed_at,output)
 SELECT m.owner_id,least(split_part(stage,':',3)::uuid,split_part(stage,':',4)::uuid),
 greatest(split_part(stage,':',3)::uuid,split_part(stage,':',4)::uuid),
 entity_name_hash(m.owner_id,least(split_part(stage,':',3)::uuid,split_part(stage,':',4)::uuid),greatest(split_part(stage,':',3)::uuid,split_part(stage,':',4)::uuid)),
 split_part(stage,':',2)::integer,false,created_at,'legacy receipt; original preserved in background_markers'
 FROM background_markers m WHERE stage ~ '^memory.entity_result:[0-9]+:[0-9a-f-]{36}:[0-9a-f-]{36}:'
 AND entity_name_hash(m.owner_id,least(split_part(stage,':',3)::uuid,split_part(stage,':',4)::uuid),greatest(split_part(stage,':',3)::uuid,split_part(stage,':',4)::uuid)) IS NOT NULL
 ON CONFLICT DO NOTHING;
INSERT INTO entity_alias_candidates(owner_id,left_id,right_id,name_hash,rule,source_marker)
 SELECT m.owner_id,least(split_part(stage,':',4)::uuid,split_part(stage,':',6)::uuid),
 greatest(split_part(stage,':',4)::uuid,split_part(stage,':',6)::uuid),
 entity_name_hash(m.owner_id,least(split_part(stage,':',4)::uuid,split_part(stage,':',6)::uuid),greatest(split_part(stage,':',4)::uuid,split_part(stage,':',6)::uuid)),
 split_part(stage,':',2)::integer,stage FROM background_markers m
 WHERE stage ~ '^memory.entity_candidates:[0-9]+:pair:[0-9a-f-]{36}:[0-9]+:[0-9a-f-]{36}:[0-9]+$'
 AND entity_name_hash(m.owner_id,least(split_part(stage,':',4)::uuid,split_part(stage,':',6)::uuid),greatest(split_part(stage,':',4)::uuid,split_part(stage,':',6)::uuid)) IS NOT NULL
 ON CONFLICT DO NOTHING;
DELETE FROM memory_jobs j USING background_markers m WHERE (j.owner_id,j.record_id,j.record_version,j.stage)=(m.owner_id,m.record_id,m.record_version,m.stage) AND j.state='done';
CREATE TABLE entity_scan_members (
 owner_id uuid NOT NULL REFERENCES workspace_owners ON DELETE CASCADE,entity_id uuid NOT NULL,scope text NOT NULL,
 position bigint NOT NULL, PRIMARY KEY(owner_id,entity_id,scope),UNIQUE(owner_id,scope,position)
);
INSERT INTO entity_scan_members(owner_id,entity_id,scope,position)
 SELECT r.owner_id,r.id,sc.scope,row_number() OVER(PARTITION BY r.owner_id,sc.scope ORDER BY ev.entity_type,ev.name,r.id)-1 FROM memory_records r JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(r.owner_id,r.id,r.version)
 CROSS JOIN (VALUES('person'),('place'),('organization'),('topic'),('project'),('place_topic'),('organization_topic')) sc(scope)
 WHERE r.state='active' AND (ev.entity_type=sc.scope OR (sc.scope='place_topic' AND ev.entity_type IN('place','topic')) OR (sc.scope='organization_topic' AND ev.entity_type IN('organization','topic')));
ALTER TABLE background_model_results ADD COLUMN input_estimated boolean NOT NULL DEFAULT false;
ALTER TABLE background_model_results ADD COLUMN output_estimated boolean NOT NULL DEFAULT false;
ALTER TABLE background_model_results ADD COLUMN cost_estimated boolean NOT NULL DEFAULT false;
ALTER TABLE model_usage ADD COLUMN input_estimated boolean NOT NULL DEFAULT false;
ALTER TABLE model_usage ADD COLUMN output_estimated boolean NOT NULL DEFAULT false;
ALTER TABLE model_usage ADD COLUMN cost_estimated boolean NOT NULL DEFAULT false;
-- No card job should regain dispatch eligibility after an old lease expires.
UPDATE memory_jobs SET state='done',error_code='status_cards_removed',lease_token=NULL,lease_until=NULL
 WHERE stage LIKE 'memory.card:%' AND state<>'done';
UPDATE memory_jobs SET priority=4 WHERE stage LIKE 'memory.organize:%' AND state IN('queued','leased') AND priority<>4;

CREATE INDEX memory_background_pending_owner_stage_idx ON memory_jobs(owner_id,stage text_pattern_ops) WHERE state IN('queued','leased');
ALTER TABLE memory_group_progress ADD COLUMN completed boolean NOT NULL DEFAULT false;

ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check CHECK (purpose IN
 ('secretary','deputy','answer','extraction','embedding','vision','organize','compare','entity_compare','entity_candidates','card','handover','selector','reader','selfcheck','alias_scan','alias_confirm','query_embedding','transcription'));

-- A failed attempted call remains queued. Record its failure as well as its
-- deferral; quota/write-contention deferrals decrement attempts and are not failures.
CREATE OR REPLACE FUNCTION record_background_job_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE outcome text;
BEGIN
 IF NEW.state='done' AND OLD.state='leased' THEN outcome='success';
 ELSIF OLD.state='leased' AND NEW.state='queued' AND NEW.error_code<>'' THEN outcome='deferred';
 ELSIF OLD.state='leased' AND NEW.state IN('blocked','failed') THEN outcome='failure';
 ELSE RETURN NULL; END IF;
 INSERT INTO background_stage_events(owner_id,job_id,stage,outcome,reason)
 VALUES(NEW.owner_id,NEW.id,split_part(NEW.stage,':',1),outcome,NEW.error_code);
 IF outcome='deferred' THEN
  INSERT INTO background_job_deferrals(job_id,started_at) VALUES(NEW.id,clock_timestamp()) ON CONFLICT(job_id) DO NOTHING;
  IF NEW.attempts>=OLD.attempts THEN
   INSERT INTO background_stage_events(owner_id,job_id,stage,outcome,reason)
   VALUES(NEW.owner_id,NEW.id,split_part(NEW.stage,':',1),'failure',NEW.error_code);
  END IF;
 ELSE DELETE FROM background_job_deferrals WHERE job_id=NEW.id; END IF;
 RETURN NULL;
END $$;
