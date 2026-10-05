ALTER TABLE claim_revisions
    ADD COLUMN category text NOT NULL DEFAULT 'unknown'
        CHECK (category IN ('identity','taste','rule','goal','progress','event','opinion','other_person','unknown')),
    ADD COLUMN durable boolean;

ALTER TABLE claims
    ADD COLUMN organized integer NOT NULL DEFAULT 0,
    ADD COLUMN organize_attempts smallint NOT NULL DEFAULT 0;

ALTER TABLE claim_mentions DROP CONSTRAINT claim_mentions_role_check;
ALTER TABLE claim_mentions ADD CONSTRAINT claim_mentions_role_check
    CHECK (role IN ('person','place','organization','thing','project','topic','area'));

ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check
    CHECK (purpose IN ('secretary','deputy','answer','extraction','embedding','vision','organize'));

CREATE INDEX claims_owner_organized_idx ON claims (owner_id, organized);
CREATE INDEX claim_revisions_owner_category_idx ON claim_revisions (owner_id, category);
