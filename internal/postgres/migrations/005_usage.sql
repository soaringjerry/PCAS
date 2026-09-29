ALTER TABLE background_usage DROP CONSTRAINT background_usage_pkey;
ALTER TABLE background_usage ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY;
CREATE INDEX background_usage_job_idx ON background_usage(job_id,created_at);
CREATE INDEX background_usage_budget_idx ON background_usage(owner_id,created_at);
