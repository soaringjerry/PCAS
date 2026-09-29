ALTER TABLE record_versions ADD COLUMN actor text NOT NULL DEFAULT 'user' CHECK (actor IN ('user','ai','import','system'));

CREATE TABLE workspace_owners (
    owner_id uuid PRIMARY KEY,
    revision bigint NOT NULL DEFAULT 0,
    settings jsonb NOT NULL CHECK (jsonb_typeof(settings)='object')
);
CREATE TABLE work_items (
    owner_id uuid NOT NULL REFERENCES workspace_owners,
    id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('task','idea','project')),
    title text NOT NULL,
    status text NOT NULL CHECK (status IN ('todo','doing','waiting','done','cancelled','active','shelved','awakened','promoted','dropped','paused')),
    project_id uuid,
    due_at timestamptz,
    scheduled_at timestamptz,
    version integer NOT NULL CHECK (version>0),
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY(owner_id,id),
    FOREIGN KEY(owner_id,project_id) REFERENCES work_items DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX work_items_status_idx ON work_items(owner_id,kind,status,due_at);
CREATE INDEX work_items_project_idx ON work_items(owner_id,project_id);
CREATE TABLE workspace_commands (
    owner_id uuid NOT NULL REFERENCES workspace_owners,
    request_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    revision bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_id,request_id)
);
CREATE TABLE workspace_agents (
    owner_id uuid NOT NULL REFERENCES workspace_owners,
    id text NOT NULL,
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object'),
    PRIMARY KEY(owner_id,id)
);
CREATE TABLE capture_candidates (
    owner_id uuid NOT NULL REFERENCES workspace_owners,
    id uuid NOT NULL,
    source_id uuid NOT NULL,
    source_version integer NOT NULL,
    state text NOT NULL CHECK (state IN ('pending','accepted','ignored','merged')),
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object'),
    PRIMARY KEY(owner_id,id),
    FOREIGN KEY(owner_id,source_id,source_version) REFERENCES source_versions ON DELETE CASCADE
);
CREATE TABLE work_documents (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    thing_id uuid NOT NULL,
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object'),
    PRIMARY KEY(owner_id,id),
    FOREIGN KEY(owner_id,thing_id) REFERENCES work_items ON DELETE CASCADE
);
CREATE TABLE agent_runs (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    thing_id uuid NOT NULL,
    agent_id text NOT NULL,
    status text NOT NULL CHECK (status IN ('queued','running','waiting','done','failed')),
    reserved_cost numeric(14,6) NOT NULL CHECK (reserved_cost>=0),
    created_at timestamptz NOT NULL,
    lease_until timestamptz,
    lease_token uuid,
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object'),
    PRIMARY KEY(owner_id,id),
    FOREIGN KEY(owner_id,thing_id) REFERENCES work_items,
    FOREIGN KEY(owner_id,agent_id) REFERENCES workspace_agents
);
CREATE INDEX runs_budget_idx ON agent_runs(owner_id,created_at);
CREATE INDEX runs_queue_idx ON agent_runs(created_at) WHERE status='queued';
CREATE TABLE run_dependencies (
    owner_id uuid NOT NULL,
    run_id uuid NOT NULL,
    memory_id uuid NOT NULL,
    memory_version integer NOT NULL,
    PRIMARY KEY(owner_id,run_id,memory_id),
    FOREIGN KEY(owner_id,run_id) REFERENCES agent_runs ON DELETE CASCADE,
    FOREIGN KEY(owner_id,memory_id,memory_version) REFERENCES record_versions
);
CREATE INDEX run_dependencies_memory_idx ON run_dependencies(owner_id,memory_id);
CREATE TABLE context_exclusions (
    owner_id uuid NOT NULL,
    thing_id uuid NOT NULL,
    memory_id uuid NOT NULL,
    PRIMARY KEY(owner_id,thing_id,memory_id),
    FOREIGN KEY(owner_id,thing_id) REFERENCES work_items ON DELETE CASCADE,
    FOREIGN KEY(owner_id,memory_id) REFERENCES memory_records ON DELETE CASCADE
);
CREATE TABLE training_samples (
    owner_id uuid NOT NULL REFERENCES workspace_owners,
    id uuid NOT NULL,
    run_id uuid,
    memory_id uuid,
    state text NOT NULL CHECK (state IN ('candidate','included','excluded')),
    stale boolean NOT NULL DEFAULT false,
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object'),
    PRIMARY KEY(owner_id,id),
    FOREIGN KEY(owner_id,run_id) REFERENCES agent_runs,
    FOREIGN KEY(owner_id,memory_id) REFERENCES memory_records
);
CREATE TABLE memory_export_manifests (
    owner_id uuid NOT NULL REFERENCES workspace_owners,
    id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    sample_versions jsonb NOT NULL CHECK (jsonb_typeof(sample_versions)='array'),
    PRIMARY KEY(owner_id,id)
);
CREATE TABLE claim_source_keys (
    owner_id uuid NOT NULL,
    source_id uuid NOT NULL,
    claim_key text NOT NULL,
    claim_id uuid NOT NULL,
    corrected boolean NOT NULL DEFAULT false,
    PRIMARY KEY(owner_id,source_id,claim_key),
    FOREIGN KEY(owner_id,source_id) REFERENCES sources ON DELETE CASCADE,
    FOREIGN KEY(owner_id,claim_id) REFERENCES claims ON DELETE CASCADE
);
CREATE TABLE claim_reimport_blocks (
    owner_id uuid NOT NULL,
    key_hash bytea NOT NULL CHECK (octet_length(key_hash)=32),
    blocked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_id,key_hash)
);
CREATE TABLE workspace_notices (
    owner_id uuid NOT NULL,
    thing_id uuid NOT NULL,
    trigger_id text NOT NULL,
    due_at timestamptz NOT NULL,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_id,thing_id,trigger_id,due_at),
    FOREIGN KEY(owner_id,thing_id) REFERENCES work_items ON DELETE CASCADE
);
