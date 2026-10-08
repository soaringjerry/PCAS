CREATE TABLE document_versions (
 owner_id uuid NOT NULL,document_id uuid NOT NULL,version integer NOT NULL CHECK(version>0),
 document jsonb NOT NULL,written_at timestamptz NOT NULL,
 author text NOT NULL CHECK(author IN('user','deputy','secretary')),
 based_on integer,run_id uuid,
 PRIMARY KEY(owner_id,document_id,version)
);
CREATE INDEX document_versions_time_idx ON document_versions(owner_id,document_id,written_at DESC,version DESC);
INSERT INTO document_versions(owner_id,document_id,version,document,written_at,author,based_on,run_id)
 SELECT owner_id,id,1,document||jsonb_build_object('version',1),
 coalesce((document->>'updatedAt')::timestamptz,(document->>'createdAt')::timestamptz,clock_timestamp()),
 CASE WHEN document->>'by' IN('ai','deputy') THEN 'deputy' WHEN document->>'by'='secretary' THEN 'secretary' ELSE 'user' END,
 NULL,nullif(document->>'runId','')::uuid FROM work_documents;
UPDATE work_documents SET document=document||jsonb_build_object('version',1,'basedOn',null);
CREATE FUNCTION assign_document_version() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n integer;
BEGIN
 IF current_setting('pcas.restore_document',true)=NEW.id::text THEN RETURN NEW; END IF;
 SELECT coalesce(max(version),0)+1 INTO n FROM document_versions WHERE owner_id=NEW.owner_id AND document_id=NEW.id;
 NEW.document=NEW.document||jsonb_build_object('version',n);
 RETURN NEW;
END $$;
CREATE FUNCTION retain_document_version() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE by_who text;
BEGIN
 IF current_setting('pcas.restore_document',true)=NEW.id::text THEN RETURN NULL; END IF;
 by_who=CASE WHEN NEW.document->>'by' IN('ai','deputy') THEN 'deputy' WHEN NEW.document->>'by'='secretary' THEN 'secretary' ELSE 'user' END;
 INSERT INTO document_versions(owner_id,document_id,version,document,written_at,author,based_on,run_id)
 VALUES(NEW.owner_id,NEW.id,(NEW.document->>'version')::integer,NEW.document,clock_timestamp(),by_who,
 (NEW.document->>'basedOn')::integer,CASE WHEN by_who='deputy' THEN nullif(NEW.document->>'runId','')::uuid END);
 RETURN NULL;
END $$;
CREATE TRIGGER document_version_number BEFORE INSERT OR UPDATE ON work_documents FOR EACH ROW EXECUTE FUNCTION assign_document_version();
CREATE TRIGGER document_version_retention AFTER INSERT OR UPDATE ON work_documents FOR EACH ROW EXECUTE FUNCTION retain_document_version();
-- Versions have no FK to the current document: deleting a document or undoing
-- adoption only removes the current view, and its whole history comes back
-- when the original document is restored by the existing undo log.
CREATE TABLE revise_run_targets (
 owner_id uuid NOT NULL,run_id uuid NOT NULL,document_id uuid NOT NULL,target_version integer NOT NULL,
 PRIMARY KEY(owner_id,run_id),FOREIGN KEY(owner_id,run_id) REFERENCES agent_runs ON DELETE CASCADE
);
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['document_versions','revise_run_targets'] LOOP
  EXECUTE format('CREATE TRIGGER snapshot_insert AFTER INSERT ON %I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
  EXECUTE format('CREATE TRIGGER snapshot_update AFTER UPDATE ON %I REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
  EXECUTE format('CREATE TRIGGER snapshot_delete AFTER DELETE ON %I REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION bump_workspace_snapshot_version()',t);
 END LOOP;
END $$;
