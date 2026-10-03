-- Work on imported history yields to fresh input (priority 10 instead of 0).
-- That used to be worked out for every waiting job on every claim, which with
-- tens of thousands of jobs queued made each claim take seconds. It is now
-- settled once, when the job is queued, so a claim can follow the index.

CREATE FUNCTION job_is_imported_history(owner uuid, record uuid, version integer) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT EXISTS(SELECT 1 FROM import_batches b WHERE b.owner_id=owner AND b.archive_id=record)
      OR EXISTS(SELECT 1 FROM archive_entries ae WHERE ae.owner_id=owner AND ae.source_id=record AND ae.source_version=version)
      OR EXISTS(SELECT 1 FROM episode_members em JOIN archive_entries ae
           ON (ae.owner_id,ae.source_id,ae.source_version)=(em.owner_id,em.member_id,em.member_version)
           WHERE em.owner_id=owner AND em.episode_id=record AND em.episode_version=version)
$$;

CREATE FUNCTION memory_jobs_history_priority() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.priority < 10 AND job_is_imported_history(NEW.owner_id, NEW.record_id, NEW.record_version) THEN
    NEW.priority := 10;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER memory_jobs_history_priority BEFORE INSERT ON memory_jobs
  FOR EACH ROW EXECUTE FUNCTION memory_jobs_history_priority();

-- A message's first job is queued just before the message is listed under its
-- archive, so that job is settled when the listing arrives.
CREATE FUNCTION archive_entries_job_priority() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE memory_jobs SET priority=10
   WHERE owner_id=NEW.owner_id AND record_id=NEW.source_id AND record_version=NEW.source_version
     AND state IN ('queued','leased') AND priority<10;
  RETURN NEW;
END $$;
CREATE TRIGGER archive_entries_job_priority AFTER INSERT ON archive_entries
  FOR EACH ROW EXECUTE FUNCTION archive_entries_job_priority();

UPDATE memory_jobs j SET priority=10
 WHERE j.state IN ('queued','leased') AND j.priority<10
   AND job_is_imported_history(j.owner_id, j.record_id, j.record_version);
