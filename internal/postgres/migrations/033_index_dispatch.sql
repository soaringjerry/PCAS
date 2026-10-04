-- Indexing has its own bounded worker lane. Avoid walking the extraction
-- backlog to find its candidates; existing jobs and progress are preserved.
CREATE INDEX jobs_index_dispatch_ready_idx ON memory_jobs (priority,available_at,created_at,id)
 WHERE state IN ('queued','leased')
 AND (stage IN ('source.embed','source.tokenize','memory.embed','memory.index') OR stage LIKE 'memory.embed:%');
