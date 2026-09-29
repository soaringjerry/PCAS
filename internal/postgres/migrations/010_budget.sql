-- Billing reservations contain no memory body and survive source deletion.
ALTER TABLE background_usage DROP CONSTRAINT background_usage_job_id_fkey;
ALTER TABLE background_usage ALTER COLUMN job_id DROP NOT NULL;
