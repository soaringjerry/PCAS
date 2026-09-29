ALTER TABLE source_versions DROP CONSTRAINT source_versions_representation_check;
ALTER TABLE source_versions ADD CONSTRAINT source_versions_representation_check CHECK(representation IN ('original','ocr','transcript','extracted'));
CREATE TABLE blob_cleanup_jobs (
    owner_id uuid NOT NULL,
    blob_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_id,blob_key)
);
