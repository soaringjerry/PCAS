-- Fingerprint content only: undo preserves increasing versions and writes audit
-- history/source receipts. Both collection and undo call this same function.
CREATE FUNCTION action_document_hash(document jsonb) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT encode(sha256(convert_to(
  (document - ARRAY['recordVersion','updatedAt','history','evolution','sources'])::text,
  'UTF8')), 'hex');
$$;

-- Existing afterHash values cannot be converted without an after snapshot.
-- The undo reader accepts the legacy full-document hash as well.
CREATE OR REPLACE FUNCTION collect_action_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE buffer jsonb; entry jsonb; row_id text; position integer; old_doc jsonb; new_hash text;
BEGIN
 -- Deletion propagation also covers writes inside shared artifact helpers and
 -- cascading document deletes, without collecting another undoable action.
 IF TG_TABLE_NAME IN ('work_items','work_documents') AND TG_OP IN ('UPDATE','DELETE') THEN
  IF current_setting('pcas.expire_actions',true)=OLD.owner_id::text THEN
   UPDATE action_log SET changes='[]'::jsonb,expired_at=now()
   WHERE owner_id=OLD.owner_id AND expired_at IS NULL AND EXISTS(
    SELECT 1 FROM jsonb_array_elements(changes) c
    WHERE c->>'table'=TG_TABLE_NAME AND c->>'id'=OLD.id::text);
  END IF;
 END IF;
 IF nullif(current_setting('pcas.action_changes',true),'') IS NULL THEN RETURN NULL; END IF;
 buffer := current_setting('pcas.action_changes')::jsonb;
 IF TG_OP='DELETE' THEN row_id:=OLD.id::text; ELSE row_id:=NEW.id::text; END IF;
 IF TG_OP<>'INSERT' THEN old_doc:=OLD.document; END IF;
 IF TG_OP<>'DELETE' THEN new_hash:=action_document_hash(NEW.document); END IF;
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
