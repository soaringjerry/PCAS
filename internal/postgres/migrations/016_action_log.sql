CREATE TABLE action_log (
 owner_id uuid NOT NULL REFERENCES workspace_owners,
 id uuid NOT NULL,
 source text NOT NULL CHECK (source IN ('command','desk','worker')),
 turn_id uuid,
 summary text NOT NULL,
 changes jsonb NOT NULL CHECK (jsonb_typeof(changes)='array'),
 created_at timestamptz NOT NULL DEFAULT now(),
 undone_at timestamptz,
 PRIMARY KEY(owner_id,id)
);
CREATE INDEX action_log_created_idx ON action_log(owner_id,created_at DESC);
ALTER TABLE desk_turns ADD COLUMN conversation_id uuid, ADD COLUMN thing_id uuid,
 ADD COLUMN request_id uuid, ADD COLUMN request_hash bytea, ADD COLUMN response jsonb;
CREATE UNIQUE INDEX desk_turn_request_idx ON desk_turns(owner_id,request_id) WHERE request_id IS NOT NULL;
CREATE INDEX desk_turn_conversation_idx ON desk_turns(owner_id,conversation_id,created_at);

-- A transaction-local buffer hooks every document write, including run writes
-- owned by the worker. An unlogged transaction does not allocate a buffer.
CREATE FUNCTION collect_action_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE buffer jsonb; entry jsonb; row_id text; position integer; old_doc jsonb; new_hash text;
BEGIN
 IF nullif(current_setting('pcas.action_changes',true),'') IS NULL THEN RETURN NULL; END IF;
 buffer := current_setting('pcas.action_changes')::jsonb;
 IF TG_OP='DELETE' THEN row_id:=OLD.id::text; ELSE row_id:=NEW.id::text; END IF;
 IF TG_OP<>'INSERT' THEN old_doc:=OLD.document; END IF;
 IF TG_OP<>'DELETE' THEN new_hash:=encode(sha256(convert_to(NEW.document::text,'UTF8')),'hex'); END IF;
 SELECT ordinality::integer-1 INTO position FROM jsonb_array_elements(buffer) WITH ORDINALITY
 WHERE value->>'table'=TG_TABLE_NAME AND value->>'id'=row_id;
 IF position IS NULL THEN
  entry:=jsonb_build_object('table',TG_TABLE_NAME,'id',row_id,'before',old_doc,'afterHash',new_hash);
  buffer:=buffer||jsonb_build_array(entry);
 ELSE
  buffer:=jsonb_set(buffer,ARRAY[position::text,'afterHash'],coalesce(to_jsonb(new_hash),'null'::jsonb));
 END IF;
 PERFORM set_config('pcas.action_changes',buffer::text,true);
 RETURN NULL;
END $$;
CREATE TRIGGER log_item_change AFTER INSERT OR UPDATE OR DELETE ON work_items FOR EACH ROW EXECUTE FUNCTION collect_action_change();
CREATE TRIGGER log_document_change AFTER INSERT OR UPDATE OR DELETE ON work_documents FOR EACH ROW EXECUTE FUNCTION collect_action_change();
CREATE TRIGGER log_run_change AFTER INSERT OR UPDATE OR DELETE ON agent_runs FOR EACH ROW EXECUTE FUNCTION collect_action_change();
