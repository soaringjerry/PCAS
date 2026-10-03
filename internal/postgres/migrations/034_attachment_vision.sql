ALTER TABLE source_versions DROP CONSTRAINT source_versions_representation_check;
ALTER TABLE source_versions ADD CONSTRAINT source_versions_representation_check
 CHECK (representation IN ('original','ocr','transcript','extracted','vision'));
ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check
 CHECK (purpose IN ('secretary','deputy','answer','extraction','embedding','vision'));

-- One saved attachment belongs to one secretary request. Retry keeps that binding.
CREATE TABLE desk_attachments (
 owner_id uuid NOT NULL REFERENCES workspace_owners,
 source_id uuid NOT NULL,
 source_version integer NOT NULL,
 request_id uuid NOT NULL,
 PRIMARY KEY (owner_id,source_id,source_version),
 FOREIGN KEY (owner_id,source_id,source_version) REFERENCES source_versions(owner_id,source_id,version) ON DELETE CASCADE
);
