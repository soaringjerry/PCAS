CREATE TABLE memory_commits (
    owner_id uuid NOT NULL,
    request_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    refs jsonb NOT NULL CHECK(jsonb_typeof(refs)='array'),
    PRIMARY KEY(owner_id,request_id)
);
