ALTER TABLE workspace_notices ADD COLUMN source_id uuid;
ALTER TABLE workspace_notices ADD COLUMN source_version integer;
ALTER TABLE workspace_notices ADD FOREIGN KEY(owner_id,source_id,source_version)
    REFERENCES source_versions(owner_id,source_id,version) ON DELETE CASCADE;
CREATE TABLE record_reimport_blocks (
    owner_id uuid NOT NULL,
    id_hash bytea NOT NULL,
    PRIMARY KEY(owner_id,id_hash)
);
