CREATE TABLE record_search (
    owner_id uuid NOT NULL,
    record_id uuid NOT NULL,
    record_version integer NOT NULL,
    search_text text NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple',search_text)) STORED,
    PRIMARY KEY(owner_id,record_id,record_version),
    FOREIGN KEY(owner_id,record_id,record_version) REFERENCES record_versions ON DELETE CASCADE
);
CREATE INDEX record_search_fts_idx ON record_search USING gin(search_vector);

CREATE VIEW memory_text AS
 SELECT s.owner_id,s.source_id AS id,s.version,s.title || E'\n' || s.body AS body FROM source_versions s
 UNION ALL SELECT c.owner_id,c.claim_id,c.version,CASE WHEN jsonb_typeof(c.value)='string' THEN c.value #>> '{}' ELSE c.value::text END FROM claim_revisions c
 UNION ALL SELECT e.owner_id,e.entity_id,e.version,e.name FROM entity_versions e
 UNION ALL SELECT e.owner_id,e.id,e.version,e.title FROM episodes e;

CREATE TABLE background_usage (
    owner_id uuid NOT NULL,
    job_id uuid PRIMARY KEY REFERENCES memory_jobs ON DELETE CASCADE,
    reserved_cost numeric(14,6) NOT NULL CHECK(reserved_cost>=0),
    created_at timestamptz NOT NULL DEFAULT now()
);
