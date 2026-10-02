ALTER TABLE claim_revisions
    ADD COLUMN event_from timestamptz,
    ADD COLUMN event_to timestamptz,
    ADD COLUMN event_precision text NOT NULL DEFAULT 'unknown'
        CHECK (event_precision IN ('unknown','day','month','year','range')),
    ADD CONSTRAINT claim_revisions_event_range_check
        CHECK (event_from IS NULL OR event_to IS NULL OR event_from <= event_to);

CREATE TABLE claim_mentions (
    owner_id uuid NOT NULL,
    claim_id uuid NOT NULL,
    claim_version integer NOT NULL,
    entity_id uuid NOT NULL,
    role text NOT NULL CHECK (role IN ('person','place','organization','thing')),
    PRIMARY KEY (owner_id, claim_id, claim_version, entity_id, role),
    FOREIGN KEY (owner_id, claim_id, claim_version) REFERENCES claim_revisions ON DELETE CASCADE,
    FOREIGN KEY (owner_id, entity_id) REFERENCES entities ON DELETE CASCADE
);
CREATE INDEX claim_mentions_entity_role_idx ON claim_mentions (owner_id, entity_id, role);

CREATE INDEX record_versions_expressed_at_idx ON record_versions (owner_id, expressed_at)
    WHERE expressed_at IS NOT NULL;
CREATE INDEX claim_revisions_event_from_idx ON claim_revisions (owner_id, event_from)
    WHERE event_from IS NOT NULL;

CREATE TABLE source_extractions (
    owner_id uuid NOT NULL,
    source_id uuid NOT NULL,
    source_version integer NOT NULL,
    extractor integer NOT NULL,
    state text NOT NULL CHECK (state IN ('done','empty','failed')),
    items integer NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, source_id, source_version),
    FOREIGN KEY (owner_id, source_id, source_version) REFERENCES source_versions ON DELETE CASCADE
);

ALTER TABLE memory_jobs ADD COLUMN priority smallint NOT NULL DEFAULT 0;

CREATE TABLE model_usage (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    at timestamptz NOT NULL DEFAULT now(),
    purpose text NOT NULL CHECK (purpose IN ('secretary','deputy','answer','extraction','embedding')),
    agent_id text,
    model text NOT NULL,
    input_tokens integer NOT NULL,
    output_tokens integer NOT NULL,
    cost numeric(14,6) NOT NULL,
    turn_id uuid,
    run_id uuid,
    job_id uuid,
    memory_refs jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(memory_refs) = 'array'),
    plan jsonb,
    PRIMARY KEY (owner_id, id)
);
CREATE INDEX model_usage_owner_at_idx ON model_usage (owner_id, at);

CREATE TABLE import_batches (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    archive_id uuid NOT NULL,
    name text NOT NULL,
    state text NOT NULL CHECK (state IN ('importing','paused','done','failed')),
    total integer NOT NULL,
    stored integer NOT NULL,
    earliest timestamptz,
    latest timestamptz,
    error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, id),
    FOREIGN KEY (owner_id, archive_id) REFERENCES sources ON DELETE CASCADE
);
