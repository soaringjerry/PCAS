-- Indexes for the chain depth limit and the retention of recorded events.
-- The limit of 200 is ChainDepthLimit in internal/postgres/event_limits.go;
-- the index predicate and the query use the same number.
CREATE INDEX memory_jobs_deep_chain_idx ON memory_jobs (id) WHERE state = 'queued' AND chain_depth > 200;
CREATE INDEX memory_jobs_open_root_idx ON memory_jobs (root_job_id) WHERE state IN ('queued', 'leased');
CREATE INDEX recorded_events_time_idx ON recorded_events (occurred_at);
