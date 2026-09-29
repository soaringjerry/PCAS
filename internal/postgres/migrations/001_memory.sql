CREATE EXTENSION IF NOT EXISTS vector;

-- Shared identity and revision metadata, not an additional source of facts.
-- Composite foreign keys prevent references across owners.
CREATE TABLE memory_records (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('source','chunk','entity','episode','claim','relation','summary')),
    version integer NOT NULL CHECK (version > 0),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','withdrawn')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, id)
);

CREATE TABLE record_versions (
    owner_id uuid NOT NULL,
    record_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    valid_from timestamptz,
    valid_to timestamptz,
    time_precision text NOT NULL DEFAULT 'unknown' CHECK (time_precision IN ('unknown','instant','day','month','year','range')),
    expressed_at timestamptz,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','withdrawn')),
    PRIMARY KEY (owner_id, record_id, version),
    FOREIGN KEY (owner_id, record_id) REFERENCES memory_records ON DELETE CASCADE,
    CHECK (valid_from IS NULL OR valid_to IS NULL OR valid_from <= valid_to)
);
ALTER TABLE memory_records ADD CONSTRAINT current_version_exists
    FOREIGN KEY (owner_id, id, version) REFERENCES record_versions
    DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX record_versions_time_idx ON record_versions (owner_id, valid_from, valid_to, recorded_at);

CREATE TABLE record_grants (
    owner_id uuid NOT NULL,
    record_id uuid NOT NULL,
    principal_id text NOT NULL,
    PRIMARY KEY (owner_id, record_id, principal_id),
    FOREIGN KEY (owner_id, record_id) REFERENCES memory_records ON DELETE CASCADE
);

CREATE TABLE sources (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    connector text NOT NULL,
    external_id text NOT NULL,
    PRIMARY KEY (owner_id, id),
    UNIQUE (owner_id, connector, external_id),
    FOREIGN KEY (owner_id, id) REFERENCES memory_records ON DELETE CASCADE
);
CREATE TABLE source_versions (
    owner_id uuid NOT NULL,
    source_id uuid NOT NULL,
    version integer NOT NULL,
    external_version text NOT NULL,
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    title text NOT NULL DEFAULT '',
    body text NOT NULL DEFAULT '',
    media_type text NOT NULL,
    blob_key text,
    representation text NOT NULL DEFAULT 'original' CHECK (representation IN ('original','ocr','transcript')),
    derived_from_id uuid,
    derived_from_version integer,
    PRIMARY KEY (owner_id, source_id, version),
    UNIQUE (owner_id, source_id, external_version),
    FOREIGN KEY (owner_id, source_id) REFERENCES sources ON DELETE CASCADE,
    FOREIGN KEY (owner_id, source_id, version) REFERENCES record_versions ON DELETE CASCADE,
    FOREIGN KEY (owner_id, derived_from_id, derived_from_version) REFERENCES record_versions,
    CHECK ((derived_from_id IS NULL) = (derived_from_version IS NULL)),
    CHECK (representation = 'original' OR derived_from_id IS NOT NULL)
);

CREATE TABLE chunks (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    version integer NOT NULL,
    source_id uuid NOT NULL,
    source_version integer NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    start_rune integer NOT NULL CHECK (start_rune >= 0),
    end_rune integer NOT NULL CHECK (end_rune > start_rune),
    body text NOT NULL,
    search_text text NOT NULL DEFAULT '',
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple', search_text)) STORED,
    PRIMARY KEY (owner_id, id, version),
    UNIQUE (owner_id, source_id, source_version, ordinal),
    FOREIGN KEY (owner_id, id, version) REFERENCES record_versions ON DELETE CASCADE,
    FOREIGN KEY (owner_id, source_id, source_version) REFERENCES source_versions ON DELETE CASCADE
);
CREATE INDEX chunks_search_idx ON chunks USING gin (search_vector);

CREATE TABLE entities (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    PRIMARY KEY (owner_id, id),
    FOREIGN KEY (owner_id, id) REFERENCES memory_records ON DELETE CASCADE
);
CREATE TABLE entity_versions (
    owner_id uuid NOT NULL,
    entity_id uuid NOT NULL,
    version integer NOT NULL,
    entity_type text NOT NULL,
    name text NOT NULL,
    disambiguation jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(disambiguation) = 'object'),
    PRIMARY KEY (owner_id, entity_id, version),
    FOREIGN KEY (owner_id, entity_id) REFERENCES entities ON DELETE CASCADE,
    FOREIGN KEY (owner_id, entity_id, version) REFERENCES record_versions ON DELETE CASCADE
);
CREATE TABLE aliases (
    owner_id uuid NOT NULL,
    entity_id uuid NOT NULL,
    entity_version integer NOT NULL,
    alias text NOT NULL,
    PRIMARY KEY (owner_id, entity_id, entity_version, alias),
    FOREIGN KEY (owner_id, entity_id, entity_version) REFERENCES entity_versions ON DELETE CASCADE
);
CREATE INDEX aliases_lookup_idx ON aliases (owner_id, lower(alias));

CREATE TABLE episodes (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    version integer NOT NULL,
    title text NOT NULL,
    details jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(details) = 'object'),
    PRIMARY KEY (owner_id, id, version),
    FOREIGN KEY (owner_id, id, version) REFERENCES record_versions ON DELETE CASCADE
);
CREATE TABLE episode_members (
    owner_id uuid NOT NULL,
    episode_id uuid NOT NULL,
    episode_version integer NOT NULL,
    member_id uuid NOT NULL,
    member_version integer NOT NULL,
    PRIMARY KEY (owner_id, episode_id, episode_version, member_id, member_version),
    FOREIGN KEY (owner_id, episode_id, episode_version) REFERENCES episodes ON DELETE CASCADE,
    FOREIGN KEY (owner_id, member_id, member_version) REFERENCES record_versions
);

CREATE TABLE claims (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    PRIMARY KEY (owner_id, id),
    FOREIGN KEY (owner_id, id) REFERENCES memory_records ON DELETE CASCADE
);
CREATE TABLE claim_revisions (
    owner_id uuid NOT NULL,
    claim_id uuid NOT NULL,
    version integer NOT NULL,
    subject_id uuid NOT NULL,
    predicate text NOT NULL,
    value jsonb NOT NULL,
    scope jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(scope) = 'object'),
    nature text NOT NULL CHECK (nature IN ('fact','preference','intention','plan','decision')),
    acquisition text NOT NULL CHECK (acquisition IN ('direct','reported','inferred','execution')),
    confirmation text NOT NULL CHECK (confirmation IN ('unknown','candidate','adopted','confirmed','disputed')),
    change_type text NOT NULL CHECK (change_type IN ('initial','evidence','supplement','change','correction')),
    reason text NOT NULL DEFAULT '',
    PRIMARY KEY (owner_id, claim_id, version),
    FOREIGN KEY (owner_id, claim_id) REFERENCES claims ON DELETE CASCADE,
    FOREIGN KEY (owner_id, claim_id, version) REFERENCES record_versions ON DELETE CASCADE,
    FOREIGN KEY (owner_id, subject_id) REFERENCES entities
);
CREATE INDEX claims_subject_attribute_idx ON claim_revisions (owner_id, subject_id, predicate);
CREATE INDEX claims_scope_idx ON claim_revisions USING gin (scope);

CREATE TABLE relations (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    version integer NOT NULL,
    from_id uuid NOT NULL,
    from_version integer NOT NULL,
    to_id uuid NOT NULL,
    to_version integer NOT NULL,
    relation_type text NOT NULL CHECK (relation_type IN ('belongs_to','depends_on','causes','follows','replaces','corrects')),
    PRIMARY KEY (owner_id, id, version),
    FOREIGN KEY (owner_id, id, version) REFERENCES record_versions ON DELETE CASCADE,
    FOREIGN KEY (owner_id, from_id, from_version) REFERENCES record_versions,
    FOREIGN KEY (owner_id, to_id, to_version) REFERENCES record_versions
);
CREATE INDEX relations_from_idx ON relations (owner_id, from_id, relation_type);
CREATE INDEX relations_to_idx ON relations (owner_id, to_id, relation_type);

CREATE TABLE evidence (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    source_id uuid NOT NULL,
    source_version integer NOT NULL,
    target_id uuid NOT NULL,
    target_version integer NOT NULL,
    locator jsonb NOT NULL CHECK (jsonb_typeof(locator) = 'object'),
    acquisition text NOT NULL CHECK (acquisition IN ('direct','reported','inferred','execution')),
    stance text NOT NULL CHECK (stance IN ('supports','refutes')),
    PRIMARY KEY (owner_id, id),
    FOREIGN KEY (owner_id, source_id, source_version) REFERENCES source_versions,
    FOREIGN KEY (owner_id, target_id, target_version) REFERENCES record_versions ON DELETE CASCADE
);
CREATE INDEX evidence_target_idx ON evidence (owner_id, target_id, target_version);
CREATE INDEX evidence_source_idx ON evidence (owner_id, source_id, source_version);

CREATE TABLE activity (
    owner_id uuid NOT NULL,
    record_id uuid NOT NULL,
    last_effective_use_at timestamptz,
    stability double precision NOT NULL DEFAULT 1 CHECK (stability > 0),
    half_life_seconds double precision NOT NULL DEFAULT 2592000 CHECK (half_life_seconds > 0),
    pinned boolean NOT NULL DEFAULT false,
    PRIMARY KEY (owner_id, record_id),
    FOREIGN KEY (owner_id, record_id) REFERENCES memory_records ON DELETE CASCADE
);
CREATE TABLE use_events (
    owner_id uuid NOT NULL,
    event_key text NOT NULL,
    record_id uuid NOT NULL,
    record_version integer NOT NULL,
    kind text NOT NULL CHECK (kind IN ('user_mention','confirmation','adoption')),
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (owner_id, event_key),
    FOREIGN KEY (owner_id, record_id, record_version) REFERENCES record_versions ON DELETE CASCADE
);

CREATE TABLE derived_views (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    version integer NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('summary','context_cache')),
    body text NOT NULL,
    stale boolean NOT NULL DEFAULT false,
    PRIMARY KEY (owner_id, id, version),
    FOREIGN KEY (owner_id, id, version) REFERENCES record_versions ON DELETE CASCADE
);
CREATE TABLE derived_dependencies (
    owner_id uuid NOT NULL,
    view_id uuid NOT NULL,
    view_version integer NOT NULL,
    dependency_id uuid NOT NULL,
    dependency_version integer NOT NULL,
    PRIMARY KEY (owner_id, view_id, view_version, dependency_id, dependency_version),
    FOREIGN KEY (owner_id, view_id, view_version) REFERENCES derived_views ON DELETE CASCADE,
    FOREIGN KEY (owner_id, dependency_id, dependency_version) REFERENCES record_versions
);
CREATE INDEX dependencies_reverse_idx ON derived_dependencies (owner_id, dependency_id, dependency_version);

-- Dimension/model selection is a provider decision. Add ANN indexes with a
-- typed expression/partition once selected; never mix embedding spaces.
CREATE TABLE embeddings (
    owner_id uuid NOT NULL,
    record_id uuid NOT NULL,
    record_version integer NOT NULL,
    model text NOT NULL,
    dimensions integer NOT NULL CHECK (dimensions > 0),
    embedding vector NOT NULL,
    PRIMARY KEY (owner_id, record_id, record_version, model),
    FOREIGN KEY (owner_id, record_id, record_version) REFERENCES record_versions ON DELETE CASCADE,
    CHECK (vector_dims(embedding) = dimensions)
);
CREATE INDEX embeddings_scope_model_idx ON embeddings (owner_id, model, dimensions);

-- No original text, external identifier or deleted attachment path is retained.
CREATE TABLE reimport_blocks (
    owner_id uuid NOT NULL,
    source_key_hash bytea NOT NULL CHECK (octet_length(source_key_hash) = 32),
    blocked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, source_key_hash)
);

-- Transactional event queue and stage status. Jobs contain references, no raw
-- text. Leases are fenced: an expired worker cannot commit or acknowledge work.
CREATE TABLE memory_jobs (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    record_id uuid NOT NULL,
    record_version integer NOT NULL,
    stage text NOT NULL,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','leased','blocked','failed','done')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    lease_token uuid,
    error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, record_id, record_version, stage),
    FOREIGN KEY (owner_id, record_id, record_version) REFERENCES record_versions ON DELETE CASCADE,
    CHECK ((state = 'leased') = (lease_until IS NOT NULL AND lease_token IS NOT NULL))
);
CREATE INDEX jobs_ready_idx ON memory_jobs (available_at, created_at) WHERE state = 'queued';
CREATE INDEX jobs_lease_idx ON memory_jobs (lease_until) WHERE state = 'leased';
