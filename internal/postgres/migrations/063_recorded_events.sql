-- Queued work gets causal identities, and each new piece of queued work is a
-- recorded event. A job that a handler queues under its own lease keeps the
-- root of that handler's job and is one step deeper in the chain. Work that
-- a schedule or a request queues starts a chain.
-- This migration records. It sets no limit: a limit needs the depths that
-- the live instance shows.
ALTER TABLE memory_jobs
    ADD COLUMN root_job_id uuid,
    ADD COLUMN cause_job_id uuid,
    ADD COLUMN chain_depth integer NOT NULL DEFAULT 0 CHECK (chain_depth >= 0);

CREATE TABLE recorded_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    type text NOT NULL,
    stage text NOT NULL,
    object_id uuid NOT NULL,
    object_version integer NOT NULL,
    trigger_source text NOT NULL CHECK (trigger_source IN ('job','schedule_or_request')),
    job_id uuid NOT NULL,
    root_id uuid NOT NULL,
    cause_id uuid,
    chain_depth integer NOT NULL CHECK (chain_depth >= 0),
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recorded_events_owner_time_idx ON recorded_events (owner_id, occurred_at);
CREATE INDEX recorded_events_root_idx ON recorded_events (root_id);

CREATE FUNCTION memory_jobs_causal_identity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    cause uuid := nullif(current_setting('pcas.cause_job', true), '')::uuid;
    cause_root uuid;
    cause_depth integer;
BEGIN
    IF cause IS NOT NULL THEN
        SELECT coalesce(root_job_id, id), chain_depth INTO cause_root, cause_depth FROM memory_jobs WHERE id = cause;
    END IF;
    IF cause_root IS NULL THEN
        NEW.root_job_id := NEW.id;
        NEW.cause_job_id := NULL;
        NEW.chain_depth := 0;
    ELSE
        NEW.root_job_id := cause_root;
        NEW.cause_job_id := cause;
        NEW.chain_depth := cause_depth + 1;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER memory_jobs_causal_identity BEFORE INSERT ON memory_jobs
    FOR EACH ROW EXECUTE FUNCTION memory_jobs_causal_identity();

-- A queue write that meets an existing job row changes that row and adds no
-- event: repeated delivery is not another occurrence.
CREATE FUNCTION memory_jobs_recorded_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO recorded_events(owner_id, type, stage, object_id, object_version, trigger_source, job_id, root_id, cause_id, chain_depth)
    VALUES (NEW.owner_id, 'work.queued', split_part(NEW.stage, ':', 1), NEW.record_id, NEW.record_version,
        CASE WHEN NEW.cause_job_id IS NULL THEN 'schedule_or_request' ELSE 'job' END,
        NEW.id, NEW.root_job_id, NEW.cause_job_id, NEW.chain_depth);
    RETURN NULL;
END $$;
CREATE TRIGGER memory_jobs_recorded_event AFTER INSERT ON memory_jobs
    FOR EACH ROW EXECUTE FUNCTION memory_jobs_recorded_event();
