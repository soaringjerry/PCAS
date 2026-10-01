-- Additive phase-two contracts. Existing consumers remain compatible; policy
-- enforcement, lifecycle cleanup and final adapter capture are separate work.
-- Never grant legacy sources implicitly or call providers during migration.

CREATE TABLE source_scope_revisions (
    owner_id uuid NOT NULL,
    source_id uuid NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id,source_id),
    FOREIGN KEY (owner_id,source_id) REFERENCES sources ON DELETE CASCADE
);
-- No revision row/assignments means unscoped, not globally authorized. Clearing
-- assignments retains source_scope_revisions so old requests cannot cause ABA.
CREATE TABLE source_scope_assignments (
    owner_id uuid NOT NULL,
    source_id uuid NOT NULL,
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    scope_kind text NOT NULL CHECK (scope_kind IN ('studio','global_constraint','unscoped')),
    studio_id uuid,
    PRIMARY KEY (owner_id,id),
    UNIQUE NULLS NOT DISTINCT (owner_id,source_id,scope_kind,studio_id),
    FOREIGN KEY (owner_id,source_id) REFERENCES source_scope_revisions ON DELETE CASCADE,
    CHECK ((scope_kind='studio') = (studio_id IS NOT NULL))
);
CREATE INDEX source_scope_studio_idx ON source_scope_assignments(owner_id,studio_id,source_id) WHERE scope_kind='studio';

CREATE TABLE source_authorizations (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    source_id uuid NOT NULL,
    principal_id text NOT NULL CHECK (principal_id<>''),
    role text NOT NULL CHECK (role<>''),
    model text NOT NULL CHECK (model<>''),
    provider text NOT NULL CHECK (provider<>''),
    protocol text NOT NULL CHECK (protocol<>''),
    channel text NOT NULL CHECK (channel<>''),
    route_fingerprint text NOT NULL CHECK (route_fingerprint ~ '^[0-9a-f]{64}$'),
    purpose text NOT NULL CHECK (purpose IN ('knowledge','execution_evidence','raw_audit')),
    scope_kind text NOT NULL CHECK (scope_kind IN ('studio','owner_global','unscoped')),
    studio_id uuid,
    include_global_constraints boolean NOT NULL DEFAULT false,
    revision integer NOT NULL CHECK (revision > 0),
    -- Explicit deny takes priority for this tuple's source-derived claims and
    -- summaries. No policy row does not deny independent claim permissions.
    revoked boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id,id),
    UNIQUE NULLS NOT DISTINCT (owner_id,source_id,principal_id,role,model,provider,protocol,channel,route_fingerprint,purpose,scope_kind,studio_id,include_global_constraints),
    FOREIGN KEY (owner_id,source_id) REFERENCES sources ON DELETE CASCADE,
    CHECK ((scope_kind='studio') = (studio_id IS NOT NULL)),
    CHECK (scope_kind='studio' OR NOT include_global_constraints)
);
CREATE INDEX source_authorizations_access_idx ON source_authorizations(owner_id,source_id,principal_id) WHERE NOT revoked;

-- Preserve actual kinds on legacy dependencies. The joins use the canonical
-- identity record rather than inventing claim kinds. Existing write statements
-- can omit this new nullable column; upgraded readers reject missing kinds.
ALTER TABLE run_dependencies ADD COLUMN memory_kind text CHECK (memory_kind IN ('source','chunk','entity','episode','claim','relation','summary'));
UPDATE run_dependencies d SET memory_kind=r.kind FROM memory_records r WHERE (r.owner_id,r.id)=(d.owner_id,d.memory_id);
ALTER TABLE derived_dependencies ADD COLUMN dependency_kind text CHECK (dependency_kind IN ('source','chunk','entity','episode','claim','relation','summary'));
UPDATE derived_dependencies d SET dependency_kind=r.kind FROM memory_records r WHERE (r.owner_id,r.id)=(d.owner_id,d.dependency_id);

-- Attempts are independent of business transactions. In particular there is no
-- FK to owner/source/claim/run/desk: such FK checks can wait on the very business
-- transaction that is waiting for its independent attempt writer. Only the
-- attempt's own dependency rows below cascade with its diagnostic lifetime.
CREATE TABLE context_attempts (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    operation_id text NOT NULL CHECK (operation_id<>''),
    ordinal integer NOT NULL CHECK (ordinal > 0),
    state text NOT NULL CHECK (state IN ('prepared','dispatched','completed','failed','outcome_unknown','invalidated')),
    recipient jsonb NOT NULL CHECK (jsonb_typeof(recipient)='object'),
    purpose text NOT NULL CHECK (purpose IN ('knowledge','execution_evidence','raw_audit')),
    manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest)='object' AND octet_length(manifest::text)<=65536),
    -- Writer includes manifest, recipient and typed dependency metadata, then
    -- checks the 64 MiB owner total atomically under the diagnostic gate.
    metadata_bytes integer NOT NULL CHECK (metadata_bytes BETWEEN 0 AND 65536 AND metadata_bytes>=octet_length(manifest::text)+octet_length(recipient::text)),
    snapshot bytea,
    snapshot_state text NOT NULL CHECK (snapshot_state IN ('retained','expired','deleted','revoked','capacity_omitted')),
    snapshot_bytes integer NOT NULL DEFAULT 0 CHECK (snapshot_bytes BETWEEN 0 AND 262144),
    input_bytes integer NOT NULL CHECK (input_bytes BETWEEN 0 AND 262144),
    payload_hash bytea CHECK (payload_hash IS NULL OR octet_length(payload_hash)=32),
    observation_layer text NOT NULL CHECK (observation_layer IN ('adapter_arguments','serialized_request','manual_package')),
    external_receipt text NOT NULL DEFAULT 'unknown' CHECK (external_receipt IN ('unknown','acknowledged')),
    dispatch_reserved_at timestamptz,
    dispatched_at timestamptz,
    delivered_at timestamptz,
    completed_at timestamptz,
    invalidation_reason text NOT NULL DEFAULT '' CHECK (invalidation_reason IN ('','corrected','replaced','deleted','revoked','scope_changed')),
    error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    body_expires_at timestamptz NOT NULL,
    metadata_expires_at timestamptz NOT NULL,
    PRIMARY KEY (owner_id,id),
    UNIQUE (owner_id,operation_id,ordinal),
    CHECK ((snapshot_state='retained') = (snapshot IS NOT NULL)),
    CHECK ((snapshot IS NULL AND snapshot_bytes=0) OR (snapshot IS NOT NULL AND snapshot_bytes=octet_length(snapshot) AND snapshot_bytes=input_bytes)),
    CHECK (body_expires_at>=created_at AND metadata_expires_at>=body_expires_at)
);
CREATE INDEX context_attempts_expiry_idx ON context_attempts(metadata_expires_at);
CREATE INDEX context_attempts_body_expiry_idx ON context_attempts(body_expires_at) WHERE snapshot IS NOT NULL;
CREATE INDEX context_attempts_capacity_idx ON context_attempts(owner_id,created_at,id);

CREATE TABLE attempt_typed_dependencies (
    owner_id uuid NOT NULL,
    attempt_id uuid NOT NULL,
    dependency_id uuid NOT NULL,
    dependency_version integer NOT NULL CHECK (dependency_version>0),
    dependency_kind text NOT NULL CHECK (dependency_kind IN ('source','claim','summary')),
    purpose text NOT NULL CHECK (purpose IN ('knowledge','execution_evidence','raw_audit')),
    hard_scope jsonb NOT NULL CHECK (jsonb_typeof(hard_scope)='object'),
    policy_id uuid,
    policy_revision integer CHECK (policy_revision>0),
    scope_revision integer NOT NULL DEFAULT 0 CHECK (scope_revision>=0),
    PRIMARY KEY (owner_id,attempt_id,dependency_id,dependency_version,dependency_kind),
    FOREIGN KEY (owner_id,attempt_id) REFERENCES context_attempts ON DELETE CASCADE,
    CHECK ((policy_id IS NULL) = (policy_revision IS NULL))
);
CREATE INDEX attempt_dependencies_reverse_idx ON attempt_typed_dependencies(owner_id,dependency_id,dependency_version);

-- Durable lineage lives as long as its business artifact, not as long as the
-- diagnostic attempt. No FK to attempts or memory records: deletion must erase
-- artifact bodies before removing memory, and retain enough tombstone lineage
-- to fail closed on subsequent replay. Consumers manage parent lifetime.
CREATE TABLE context_artifact_dependencies (
    owner_id uuid NOT NULL,
    parent_kind text NOT NULL CHECK (parent_kind IN ('run','desk_turn','artifact','summary','manual_package','training_sample')),
    parent_id text NOT NULL CHECK (parent_id<>''),
    parent_version integer NOT NULL CHECK (parent_version>0),
    dependency_id uuid NOT NULL,
    dependency_version integer NOT NULL CHECK (dependency_version>0),
    dependency_kind text NOT NULL CHECK (dependency_kind IN ('source','claim','summary')),
    purpose text NOT NULL CHECK (purpose IN ('knowledge','execution_evidence','raw_audit')),
    hard_scope jsonb NOT NULL CHECK (jsonb_typeof(hard_scope)='object'),
    recipient jsonb NOT NULL CHECK (jsonb_typeof(recipient)='object'),
    policy_id uuid,
    policy_revision integer CHECK (policy_revision>0),
    scope_revision integer NOT NULL DEFAULT 0 CHECK (scope_revision>=0),
    PRIMARY KEY (owner_id,parent_kind,parent_id,parent_version,dependency_id,dependency_version,dependency_kind),
    CHECK ((policy_id IS NULL) = (policy_revision IS NULL))
);
CREATE INDEX context_artifact_dependencies_reverse_idx ON context_artifact_dependencies(owner_id,dependency_id,dependency_version);
