ALTER TABLE memory_jobs ADD COLUMN backfill_queued_at timestamptz;
CREATE INDEX jobs_backfill_queued_at_idx ON memory_jobs (backfill_queued_at)
    WHERE backfill_queued_at IS NOT NULL;
CREATE INDEX jobs_priority_ready_idx ON memory_jobs (priority, available_at, created_at, id)
    WHERE state = 'queued';
