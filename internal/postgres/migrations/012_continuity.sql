CREATE TABLE source_contexts (
 owner_id uuid NOT NULL,
 source_id uuid NOT NULL,
 source_version integer NOT NULL,
 conversation_key text NOT NULL DEFAULT '',
 parent_key text NOT NULL DEFAULT '',
 role text NOT NULL DEFAULT '',
 branch text NOT NULL DEFAULT '',
 gaps jsonb NOT NULL DEFAULT '[]',
 PRIMARY KEY(owner_id,source_id,source_version),
 FOREIGN KEY(owner_id,source_id,source_version) REFERENCES source_versions ON DELETE CASCADE
);
CREATE INDEX source_contexts_conversation_idx ON source_contexts(owner_id,conversation_key);
CREATE TABLE episode_keys (
 owner_id uuid NOT NULL,
 external_key text NOT NULL,
 episode_id uuid NOT NULL,
 PRIMARY KEY(owner_id,external_key),
 FOREIGN KEY(owner_id,episode_id) REFERENCES memory_records ON DELETE CASCADE
);
CREATE TABLE connector_configs (
 owner_id uuid NOT NULL REFERENCES workspace_owners,
 id uuid PRIMARY KEY,
 name text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('webhook','poll','folder','archive')),
 url text NOT NULL DEFAULT '',
 token_env text NOT NULL DEFAULT '',
 token_hash bytea,
 interval_seconds integer NOT NULL CHECK(interval_seconds BETWEEN 15 AND 86400),
 enabled boolean NOT NULL,
 version integer NOT NULL DEFAULT 1,
 cursor text NOT NULL DEFAULT '',
 etag text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'idle',
 error_code text NOT NULL DEFAULT '',
 gaps jsonb NOT NULL DEFAULT '[]',
 imported integer NOT NULL DEFAULT 0,
 last_sync timestamptz,
 next_sync timestamptz NOT NULL DEFAULT now(),
 lease_token uuid,
 lease_until timestamptz,
 UNIQUE(owner_id,id)
);
CREATE INDEX connector_due_idx ON connector_configs(next_sync) WHERE enabled;
CREATE TABLE connector_files (
 owner_id uuid NOT NULL,
 connector_id uuid NOT NULL,
 path_hash bytea NOT NULL,
 fingerprint text NOT NULL,
 PRIMARY KEY(owner_id,connector_id,path_hash),
 FOREIGN KEY(owner_id,connector_id) REFERENCES connector_configs(owner_id,id) ON DELETE CASCADE
);
CREATE TABLE summary_keys (
 owner_id uuid NOT NULL,
 root_id uuid NOT NULL,
 root_version integer NOT NULL,
 principal_id text NOT NULL,
 budget integer NOT NULL,
 summary_id uuid NOT NULL,
 membership_hash bytea NOT NULL,
 coverage jsonb NOT NULL,
 PRIMARY KEY(owner_id,root_id,root_version,principal_id,budget),
 FOREIGN KEY(owner_id,root_id,root_version) REFERENCES record_versions ON DELETE CASCADE,
 FOREIGN KEY(owner_id,summary_id) REFERENCES memory_records ON DELETE CASCADE
);
ALTER TABLE activity ADD COLUMN reinforcement_limit real NOT NULL DEFAULT 8 CHECK(reinforcement_limit BETWEEN 1 AND 100);
ALTER TABLE source_versions ADD COLUMN attachment_redacted boolean NOT NULL DEFAULT false;
CREATE TABLE archive_entries (
 owner_id uuid NOT NULL,
 archive_id uuid NOT NULL,
 archive_version integer NOT NULL,
 source_id uuid NOT NULL,
 source_version integer NOT NULL,
 PRIMARY KEY(owner_id,archive_id,archive_version,source_id,source_version),
 FOREIGN KEY(owner_id,archive_id,archive_version) REFERENCES source_versions ON DELETE CASCADE,
 FOREIGN KEY(owner_id,source_id,source_version) REFERENCES source_versions ON DELETE CASCADE
);
CREATE INDEX archive_entries_source_idx ON archive_entries(owner_id,source_id);
