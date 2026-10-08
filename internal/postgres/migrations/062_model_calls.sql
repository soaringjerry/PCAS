-- Existing model_usage stays authoritative for tokens and cost.
-- No private prompt text is stored in this table.

CREATE TABLE model_calls (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    execution_id uuid NOT NULL,
    root_execution_id uuid NOT NULL,
    causation_id uuid,
    function_name text NOT NULL CHECK (function_name <> ''),
    stage text NOT NULL CHECK (stage <> ''),
    provider_id text NOT NULL CHECK (provider_id <> ''),
    model text NOT NULL CHECK (model <> ''),
    prompt_name text,
    instruction_hash text,
    schema_name text,
    schema_hash text,
    context_builder_version text,
    input_manifest jsonb NOT NULL,
    required_capabilities jsonb NOT NULL DEFAULT '[]',
    actual_mode jsonb,
    outcome text NOT NULL CHECK (outcome IN ('prepared','started','returned','failed','unknown')),
    error_code text NOT NULL DEFAULT '',
    reservation_id uuid,
    usage_id uuid,
    result_receipt jsonb,
    accounting_state text NOT NULL CHECK
        (accounting_state IN ('not_reserved','reserved','pending','settled','failed')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    finished_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (owner_id, id),
    CHECK ((prompt_name IS NULL) = (instruction_hash IS NULL)),
    CHECK ((schema_name IS NULL) = (schema_hash IS NULL)),
    CHECK (instruction_hash IS NULL OR instruction_hash ~ '^[0-9a-f]{64}$'),
    CHECK (schema_hash IS NULL OR schema_hash ~ '^[0-9a-f]{64}$'),
    CHECK (jsonb_typeof(input_manifest) = 'object'),
    CHECK (jsonb_typeof(required_capabilities) = 'array'),
    CHECK (actual_mode IS NULL OR jsonb_typeof(actual_mode) = 'object'),
    CHECK (result_receipt IS NULL OR jsonb_typeof(result_receipt) = 'object'),
    CHECK (outcome <> 'started' OR started_at IS NOT NULL),
    CHECK (outcome NOT IN ('returned','failed') OR finished_at IS NOT NULL),
    CHECK (outcome <> 'returned' OR started_at IS NOT NULL),
    CHECK (outcome NOT IN ('failed','unknown') OR error_code <> ''),
    CHECK (finished_at IS NULL OR finished_at >= created_at),
    CHECK (started_at IS NULL OR started_at >= created_at),
    CHECK (started_at IS NULL OR finished_at IS NULL OR finished_at >= started_at)
);

CREATE INDEX model_calls_owner_time ON model_calls (owner_id, created_at, id);
CREATE INDEX model_calls_owner_execution ON model_calls (owner_id, execution_id, id);
CREATE INDEX model_calls_owner_root ON model_calls (owner_id, root_execution_id, id);
CREATE UNIQUE INDEX model_calls_owner_usage ON model_calls (owner_id, usage_id) WHERE usage_id IS NOT NULL;
CREATE UNIQUE INDEX model_calls_owner_reservation ON model_calls (owner_id, reservation_id) WHERE reservation_id IS NOT NULL;

CREATE UNIQUE INDEX model_calls_unresolved_execution ON model_calls (owner_id, execution_id, stage)
    WHERE outcome IN ('prepared','started','unknown') OR accounting_state IN ('reserved','pending','failed');

-- IDs in input_manifest are references, not copies of sources or memory text.
-- No foreign key to business records: deleting a source must not erase billing evidence.
-- A returned invocation can still have pending or failed accounting.
-- An interrupted started invocation can become unknown; do not automatically repeat it.
-- Historical usage rows do not receive invented prompts, modes, or causal IDs.
