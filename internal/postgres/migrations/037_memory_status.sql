ALTER TABLE claims
    ADD COLUMN retired text NOT NULL DEFAULT '' CHECK (retired IN ('','superseded','duplicate')),
    ADD COLUMN retired_by uuid,
    ADD COLUMN retired_at timestamptz,
    ADD COLUMN compared integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT claims_retired_target_check CHECK (retired = '' OR retired_by IS NOT NULL);

-- Retired targets deliberately have no FK: deleting a kept claim may leave
-- duplicates as history, without making them current again (X2-9).
CREATE INDEX claims_owner_retired_compared_idx ON claims (owner_id, retired, compared);

CREATE TABLE entity_merges (
    owner_id uuid NOT NULL,
    merged_id uuid NOT NULL,
    kept_id uuid NOT NULL,
    merged_at timestamptz NOT NULL DEFAULT now(),
    rule integer NOT NULL,
    undone_at timestamptz,
    PRIMARY KEY (owner_id, merged_id),
    FOREIGN KEY (owner_id, merged_id) REFERENCES entities ON DELETE CASCADE,
    FOREIGN KEY (owner_id, kept_id) REFERENCES entities ON DELETE CASCADE
);

CREATE TABLE status_cards (
    owner_id uuid NOT NULL,
    key text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('project','topic','area','person','self')),
    entity_id uuid,
    name text NOT NULL,
    rule integer NOT NULL,
    built_at timestamptz,
    stale boolean NOT NULL DEFAULT true,
    PRIMARY KEY (owner_id, key),
    FOREIGN KEY (owner_id, entity_id) REFERENCES entities ON DELETE CASCADE
);

CREATE TABLE status_card_items (
    owner_id uuid NOT NULL,
    key text NOT NULL,
    field text NOT NULL CHECK (field IN ('status','deadline','decided','blocker','next','preference','people')),
    position integer NOT NULL,
    claim_id uuid NOT NULL,
    claim_version integer NOT NULL,
    PRIMARY KEY (owner_id, key, field, position),
    FOREIGN KEY (owner_id, key) REFERENCES status_cards ON DELETE CASCADE,
    FOREIGN KEY (owner_id, claim_id, claim_version) REFERENCES claim_revisions ON DELETE CASCADE
);

CREATE TABLE deadlines (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    claim_id uuid NOT NULL,
    claim_version integer NOT NULL,
    kind text NOT NULL CHECK (kind IN ('deadline','appointment','recurring')),
    at timestamptz,
    recurrence text NOT NULL DEFAULT '',
    title text NOT NULL,
    time_note text NOT NULL DEFAULT '',
    PRIMARY KEY (owner_id, id),
    FOREIGN KEY (owner_id, claim_id, claim_version) REFERENCES claim_revisions ON DELETE CASCADE
);

CREATE TABLE handovers (
    owner_id uuid PRIMARY KEY,
    body text NOT NULL,
    rule integer NOT NULL,
    built_at timestamptz,
    stale boolean NOT NULL DEFAULT true,
    depends jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(depends) = 'array')
);

ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check
    CHECK (purpose IN ('secretary','deputy','answer','extraction','embedding','vision','organize','compare','card','handover','reader','selfcheck'));
ALTER TABLE model_usage ADD COLUMN tier text NOT NULL DEFAULT ''
    CHECK (tier IN ('','light','medium','heavy'));
