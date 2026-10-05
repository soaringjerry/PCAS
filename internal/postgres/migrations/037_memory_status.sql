ALTER TABLE claims
    ADD COLUMN retired text NOT NULL DEFAULT '' CHECK (retired IN ('','superseded','duplicate')),
    ADD COLUMN retired_by uuid,
    ADD COLUMN retired_at timestamptz,
    ADD COLUMN compared integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT claims_retired_target_check CHECK (retired = '' OR retired_by IS NOT NULL);

-- Retired targets deliberately have no FK: deleting a kept claim may leave
-- duplicates as history, without making them current again (X2-9).
CREATE INDEX claims_owner_retired_compared_idx ON claims (owner_id, retired, compared);

CREATE TABLE entity_merges (
    owner_id uuid NOT NULL,
    merged_id uuid NOT NULL,
    kept_id uuid NOT NULL,
    merged_at timestamptz NOT NULL DEFAULT now(),
    rule integer NOT NULL,
    undone_at timestamptz,
    PRIMARY KEY (owner_id, merged_id),
    FOREIGN KEY (owner_id, merged_id) REFERENCES entities ON DELETE CASCADE,
    FOREIGN KEY (owner_id, kept_id) REFERENCES entities ON DELETE CASCADE
);

CREATE TABLE status_cards (
    owner_id uuid NOT NULL,
    key text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('project','topic','area','person','self')),
    entity_id uuid,
    name text NOT NULL,
    rule integer NOT NULL,
    built_at timestamptz,
    stale boolean NOT NULL DEFAULT true,
    PRIMARY KEY (owner_id, key),
    FOREIGN KEY (owner_id, entity_id) REFERENCES entities ON DELETE CASCADE
);

CREATE TABLE status_card_items (
    owner_id uuid NOT NULL,
    key text NOT NULL,
    field text NOT NULL CHECK (field IN ('status','deadline','decided','blocker','next','preference','people')),
    position integer NOT NULL,
    claim_id uuid NOT NULL,
    claim_version integer NOT NULL,
    PRIMARY KEY (owner_id, key, field, position),
    FOREIGN KEY (owner_id, key) REFERENCES status_cards ON DELETE CASCADE,
    FOREIGN KEY (owner_id, claim_id, claim_version) REFERENCES claim_revisions ON DELETE CASCADE
);

CREATE TABLE deadlines (
    owner_id uuid NOT NULL,
    id uuid NOT NULL,
    claim_id uuid NOT NULL,
    claim_version integer NOT NULL,
    kind text NOT NULL CHECK (kind IN ('deadline','appointment','recurring')),
    at timestamptz,
    recurrence text NOT NULL DEFAULT '',
    title text NOT NULL,
    time_note text NOT NULL DEFAULT '',
    PRIMARY KEY (owner_id, id),
    FOREIGN KEY (owner_id, claim_id, claim_version) REFERENCES claim_revisions ON DELETE CASCADE
);

CREATE TABLE handovers (
    owner_id uuid PRIMARY KEY,
    body text NOT NULL,
    rule integer NOT NULL,
    built_at timestamptz,
    stale boolean NOT NULL DEFAULT true,
    depends jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(depends) = 'array')
);

ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check
    CHECK (purpose IN ('secretary','deputy','answer','extraction','embedding','vision','organize','compare','card','handover','reader','selfcheck'));
ALTER TABLE model_usage ADD COLUMN tier text NOT NULL DEFAULT ''
    CHECK (tier IN ('','light','medium','heavy'));

-- R3-6: an item's applicability is separate from its fixed card column.
ALTER TABLE status_card_items ADD COLUMN applies_to text NOT NULL DEFAULT '';

-- One membership definition for builders, progress and readers. Subjects count
-- as person membership even if extraction did not repeat them as mentions.
CREATE VIEW status_current_members AS
WITH current_claims AS (
 SELECT r.owner_id,r.id AS claim_id,r.version AS claim_version,c.category,c.subject_id
 FROM memory_records r JOIN claims cl ON (cl.owner_id,cl.id)=(r.owner_id,r.id)
 JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE r.state='active' AND rv.state='active' AND cl.retired=''
 AND claim_source_is_current(r.owner_id,r.id,r.version,now())
), entity_members AS (
 SELECT c.owner_id,c.claim_id,c.claim_version,cm.entity_id FROM current_claims c
 JOIN claim_mentions cm USING(owner_id,claim_id,claim_version)
 UNION SELECT owner_id,claim_id,claim_version,subject_id FROM current_claims WHERE subject_id IS NOT NULL
)
SELECT m.owner_id,'entity:'||m.entity_id::text AS key,ev.entity_type AS kind,m.entity_id,ev.name,m.claim_id,m.claim_version
 FROM entity_members m JOIN memory_records er ON(er.owner_id,er.id)=(m.owner_id,m.entity_id) AND er.state='active'
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version)
 WHERE ev.entity_type IN('project','topic','area','person')
UNION ALL
SELECT c.owner_id,'self:'||c.category,'self',NULL::uuid,
 CASE c.category WHEN 'identity' THEN '身份' WHEN 'taste' THEN '口味' WHEN 'rule' THEN '对助手的要求' ELSE '目标' END,
 c.claim_id,c.claim_version FROM current_claims c WHERE c.category IN('identity','taste','rule','goal');

CREATE INDEX status_items_claim_idx ON status_card_items(owner_id,claim_id);
CREATE INDEX deadlines_claim_idx ON deadlines(owner_id,claim_id,claim_version);

-- Existing durable queue slots hold the debounce deadline, so no new progress
-- or change-time columns are needed. A running lease is never replaced.
CREATE FUNCTION status_invalidate(p_owner uuid,p_claim uuid,p_key text DEFAULT '') RETURNS void LANGUAGE plpgsql AS $$
DECLARE c record; anchor record;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM status_cards WHERE owner_id=p_owner) AND NOT EXISTS(SELECT 1 FROM handovers WHERE owner_id=p_owner) THEN RETURN; END IF;
 SELECT r.id,r.version INTO anchor FROM memory_records r JOIN entity_versions ev
 ON(ev.owner_id,ev.entity_id,ev.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=p_owner AND r.state='active' AND ev.entity_type='area' ORDER BY r.created_at,r.id LIMIT 1;
 FOR c IN UPDATE status_cards sc SET stale=true WHERE sc.owner_id=p_owner AND (
 sc.key=p_key OR EXISTS(SELECT 1 FROM status_current_members m WHERE m.owner_id=p_owner AND m.claim_id=p_claim AND m.key=sc.key)
 OR EXISTS(SELECT 1 FROM status_card_items i WHERE i.owner_id=p_owner AND i.claim_id=p_claim AND i.key=sc.key)) RETURNING sc.* LOOP
  IF anchor.id IS NOT NULL THEN
   INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at)
   VALUES(gen_random_uuid(),p_owner,anchor.id,anchor.version,'memory.card:'||c.rule||':'||c.key,13,clock_timestamp()+interval '10 minutes')
   ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET
    available_at=excluded.available_at,updated_at=clock_timestamp(),
    state=CASE WHEN memory_jobs.state='leased' THEN 'leased' ELSE 'queued' END,
    attempts=CASE WHEN memory_jobs.state='leased' THEN memory_jobs.attempts ELSE 0 END;
  END IF;
 END LOOP;
 UPDATE handovers SET stale=true WHERE owner_id=p_owner;
END $$;

CREATE FUNCTION status_claim_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v jsonb; p uuid; old_key text;
BEGIN
 v=CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
 p=coalesce((v->>'claim_id')::uuid,(v->>'id')::uuid);
 IF TG_TABLE_NAME='memory_records' AND v->>'kind'<>'claim' THEN RETURN NULL; END IF;
 IF TG_TABLE_NAME='claim_revisions' THEN
  IF TG_OP<>'INSERT' THEN
   old_key='self:'||OLD.category; PERFORM status_invalidate(OLD.owner_id,OLD.claim_id,old_key);
   IF OLD.subject_id IS NOT NULL THEN PERFORM status_invalidate(OLD.owner_id,OLD.claim_id,'entity:'||OLD.subject_id::text); END IF;
  END IF;
 ELSIF TG_TABLE_NAME='claim_mentions' THEN
  IF TG_OP<>'INSERT' THEN PERFORM status_invalidate(OLD.owner_id,OLD.claim_id,'entity:'||OLD.entity_id::text); END IF;
 END IF;
 PERFORM status_invalidate((v->>'owner_id')::uuid,p,CASE WHEN TG_TABLE_NAME='claim_mentions' THEN 'entity:'||(v->>'entity_id') ELSE '' END);
 IF TG_WHEN='BEFORE' THEN RETURN OLD; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER status_claims_changed AFTER INSERT OR UPDATE OR DELETE ON claims FOR EACH ROW EXECUTE FUNCTION status_claim_changed();
CREATE TRIGGER status_revision_changed AFTER INSERT OR UPDATE OR DELETE ON claim_revisions FOR EACH ROW EXECUTE FUNCTION status_claim_changed();
CREATE TRIGGER status_mentions_changed AFTER INSERT OR UPDATE OR DELETE ON claim_mentions FOR EACH ROW EXECUTE FUNCTION status_claim_changed();
CREATE TRIGGER status_record_changed AFTER UPDATE ON memory_records FOR EACH ROW WHEN (OLD.kind='claim' AND (OLD.version IS DISTINCT FROM NEW.version OR OLD.state IS DISTINCT FROM NEW.state)) EXECUTE FUNCTION status_claim_changed();
CREATE TRIGGER status_record_deleted BEFORE DELETE ON memory_records FOR EACH ROW WHEN (OLD.kind='claim') EXECUTE FUNCTION status_claim_changed();
CREATE FUNCTION status_card_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN UPDATE handovers SET stale=true WHERE owner_id=OLD.owner_id; RETURN NULL; END $$;
CREATE TRIGGER status_card_rebuilt AFTER UPDATE OF built_at OR DELETE ON status_cards FOR EACH ROW EXECUTE FUNCTION status_card_changed();

-- Source withdrawal/replacement can make derived claims cease to be current.
-- Invalidate before the source/membership disappears, including unselected ones.
CREATE FUNCTION status_source_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target record;
BEGIN
 FOR target IN SELECT DISTINCT target_id FROM evidence WHERE owner_id=OLD.owner_id AND source_id=OLD.id LOOP
  PERFORM status_invalidate(OLD.owner_id,target.target_id);
 END LOOP;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER status_source_deleted BEFORE DELETE ON memory_records FOR EACH ROW WHEN (OLD.kind='source') EXECUTE FUNCTION status_source_changed();
CREATE TRIGGER status_source_updated BEFORE UPDATE OF version,state ON memory_records FOR EACH ROW WHEN (OLD.kind='source' AND (OLD.version IS DISTINCT FROM NEW.version OR OLD.state IS DISTINCT FROM NEW.state)) EXECUTE FUNCTION status_source_changed();
CREATE FUNCTION status_evidence_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN PERFORM status_invalidate(OLD.owner_id,OLD.target_id); END IF;
 IF TG_OP<>'DELETE' THEN PERFORM status_invalidate(NEW.owner_id,NEW.target_id); END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER status_evidence_mutated BEFORE INSERT OR UPDATE OR DELETE ON evidence FOR EACH ROW EXECUTE FUNCTION status_evidence_changed();
