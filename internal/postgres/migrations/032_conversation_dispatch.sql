-- Materialize stage priority at enqueue: history extraction can start while
-- search indexes are still being built; fresh input keeps its higher priority.
CREATE FUNCTION imported_job_priority(stage text) RETURNS integer LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN stage LIKE 'source.extract:conversation:%' THEN 9
   WHEN stage IN ('source.embed','source.tokenize','memory.embed','memory.index','memory.summary') THEN 20
   ELSE 10 END
$$;

CREATE OR REPLACE FUNCTION memory_jobs_history_priority() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF job_is_imported_history(NEW.owner_id,NEW.record_id,NEW.record_version) THEN
   NEW.priority := greatest(NEW.priority,imported_job_priority(NEW.stage));
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION archive_entries_job_priority() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE memory_jobs SET priority=greatest(priority,imported_job_priority(stage))
   WHERE owner_id=NEW.owner_id AND record_id=NEW.source_id AND record_version=NEW.source_version
     AND state IN ('queued','leased') AND priority<imported_job_priority(stage);
 RETURN NEW;
END $$;

UPDATE memory_jobs j SET priority=CASE
 WHEN j.stage LIKE 'source.extract:conversation:%' THEN least(j.priority,9)
 ELSE greatest(j.priority,imported_job_priority(j.stage)) END
 WHERE j.state IN ('queued','leased')
   AND (j.stage LIKE 'source.extract:conversation:%' OR
        j.stage IN ('source.embed','source.tokenize','memory.embed','memory.index','memory.summary'))
   AND job_is_imported_history(j.owner_id,j.record_id,j.record_version);

-- Existing segment jobs already carry durable continuation. Their redundant
-- message entrances must not occupy the beginning of every candidate page.
-- No source extraction ledger or original text is changed here.
UPDATE memory_jobs root SET state='done',error_code='',updated_at=now()
 FROM source_contexts own
 WHERE root.stage='source.extract' AND root.state='queued'
   AND (own.owner_id,own.source_id,own.source_version)=(root.owner_id,root.record_id,root.record_version)
   AND own.conversation_key<>''
   AND EXISTS (
     SELECT 1 FROM source_contexts sibling
     JOIN memory_jobs family ON (family.owner_id,family.record_id,family.record_version)=
       (sibling.owner_id,sibling.source_id,sibling.source_version)
     JOIN memory_records live ON (live.owner_id,live.id,live.version)=
       (family.owner_id,family.record_id,family.record_version) AND live.state='active'
     WHERE sibling.owner_id=own.owner_id AND sibling.conversation_key=own.conversation_key
       AND family.stage LIKE 'source.extract:conversation:%' AND family.state IN ('queued','leased')
   );

-- One ordering for ready jobs and expired leases, also used for keyset paging.
CREATE INDEX jobs_dispatch_ready_idx ON memory_jobs (priority,available_at,created_at,id)
 WHERE state IN ('queued','leased');
