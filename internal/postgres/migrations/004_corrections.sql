CREATE TABLE claim_key_redirects (
    owner_id uuid NOT NULL,
    key_hash bytea NOT NULL CHECK(octet_length(key_hash)=32),
    claim_id uuid NOT NULL,
    PRIMARY KEY(owner_id,key_hash),
    FOREIGN KEY(owner_id,claim_id) REFERENCES claims ON DELETE CASCADE
);

-- Adopted outputs retain dependencies even after they become action artifacts.
CREATE TABLE adopted_artifacts (
    owner_id uuid NOT NULL,
    run_id uuid NOT NULL,
    thing_id uuid NOT NULL,
    kind text NOT NULL CHECK(kind IN ('notes','body','progress','check','task')),
    artifact_id text NOT NULL,
    body text NOT NULL,
    PRIMARY KEY(owner_id,run_id,thing_id,kind,artifact_id),
    FOREIGN KEY(owner_id,run_id) REFERENCES agent_runs ON DELETE CASCADE,
    FOREIGN KEY(owner_id,thing_id) REFERENCES work_items ON DELETE CASCADE
);
