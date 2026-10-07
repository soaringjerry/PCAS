ALTER TABLE source_versions ADD COLUMN scope jsonb NOT NULL DEFAULT '{}';
CREATE TABLE project_files (
 owner_id uuid NOT NULL,source_id uuid NOT NULL,project_id uuid NOT NULL,
 content_hash text NOT NULL,size bigint NOT NULL CHECK(size>0),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner_id,source_id),UNIQUE(owner_id,project_id,content_hash),
 FOREIGN KEY(owner_id,source_id) REFERENCES memory_records ON DELETE CASCADE,
 FOREIGN KEY(owner_id,project_id) REFERENCES work_items ON DELETE CASCADE
);
CREATE TABLE attachment_model_results (
 owner_id uuid NOT NULL,source_id uuid NOT NULL,source_version integer NOT NULL,
 component text NOT NULL,body jsonb NOT NULL,
 PRIMARY KEY(owner_id,source_id,source_version,component),
 FOREIGN KEY(owner_id,source_id,source_version) REFERENCES record_versions(owner_id,record_id,version) ON DELETE CASCADE
);
CREATE FUNCTION clear_attachment_model_results() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state<>'active' THEN DELETE FROM attachment_model_results WHERE owner_id=NEW.owner_id AND source_id=NEW.id; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER clear_attachment_models AFTER UPDATE OF state ON memory_records FOR EACH ROW EXECUTE FUNCTION clear_attachment_model_results();
CREATE TRIGGER snapshot_insert AFTER INSERT ON project_files REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version();
CREATE TRIGGER snapshot_delete AFTER DELETE ON project_files REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version();
-- Source/item purges use expire_actions, unlike reversible deleteDoc. Purge
-- archived writing as well so deleted source-derived copies cannot survive.
CREATE FUNCTION purge_document_archive() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('pcas.expire_actions',true)=OLD.owner_id::text THEN
  DELETE FROM document_versions WHERE owner_id=OLD.owner_id AND document_id=OLD.id;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER document_archive_purge AFTER DELETE ON work_documents FOR EACH ROW EXECUTE FUNCTION purge_document_archive();
