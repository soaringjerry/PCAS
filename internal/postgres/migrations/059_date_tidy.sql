-- The background tidies the dates read from memory (phase 3.6): each undated,
-- clock-less or long-past date is judged once per memory version. The verdict
-- is kept so "keep" is not asked again and the observatory can show why.
ALTER TABLE action_log DROP CONSTRAINT action_log_source_check;
ALTER TABLE action_log ADD CONSTRAINT action_log_source_check
 CHECK(source IN('command','desk','worker','background_extraction','background_topic','background_tidy'));

CREATE TABLE date_tidy_checks(
 owner_id uuid NOT NULL,
 claim_id uuid NOT NULL,
 claim_version integer NOT NULL,
 class text NOT NULL CHECK(class IN('unclear','habit','past')),
 decision text NOT NULL CHECK(decision IN('keep','drop','task','idea')),
 reason text NOT NULL,
 entry_title text NOT NULL DEFAULT '',
 title text NOT NULL DEFAULT '',
 item_id uuid,
 action_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(owner_id,claim_id,claim_version,class)
);
CREATE INDEX date_tidy_checks_recent ON date_tidy_checks(owner_id,created_at DESC);

ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check CHECK(purpose IN
 ('secretary','deputy','answer','extraction','embedding','vision','organize','compare','entity_compare','entity_candidates','card','handover','selector','reader','selfcheck','alias_scan','alias_confirm','query_embedding','transcription','project_handover','effort','topic_project','date_tidy'));
