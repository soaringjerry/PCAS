-- Repair already-installed triggers without changing migration 037's checksum.
-- All status writers lock cards (in key order) before card queue slots.
CREATE FUNCTION status_invalidate_keys(p_owner uuid,p_keys text[]) RETURNS void LANGUAGE plpgsql AS $$
DECLARE c record; anchor record;
BEGIN
 PERFORM 1 FROM status_cards WHERE owner_id=p_owner AND key=ANY(p_keys) ORDER BY key FOR UPDATE;
 SELECT r.id,r.version INTO anchor FROM memory_records r JOIN entity_versions ev
 ON(ev.owner_id,ev.entity_id,ev.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=p_owner AND r.state='active' AND ev.entity_type='area' ORDER BY r.created_at,r.id LIMIT 1;
 FOR c IN SELECT * FROM status_cards WHERE owner_id=p_owner AND key=ANY(p_keys) ORDER BY key LOOP
  UPDATE status_cards SET stale=true WHERE owner_id=p_owner AND key=c.key;
  IF anchor.id IS NOT NULL THEN
   INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at)
   VALUES(gen_random_uuid(),p_owner,anchor.id,anchor.version,'memory.card:'||c.rule||':'||c.key,
          CASE WHEN c.kind='self' THEN 7 ELSE 8 END,clock_timestamp()+interval '10 minutes')
   ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET
    available_at=excluded.available_at,priority=excluded.priority,updated_at=clock_timestamp(),
    state=CASE WHEN memory_jobs.state='leased' THEN 'leased' ELSE 'queued' END,
    attempts=CASE WHEN memory_jobs.state='leased' THEN memory_jobs.attempts ELSE 0 END;
  END IF;
 END LOOP;
 UPDATE handovers SET stale=true WHERE owner_id=p_owner;
END $$;
CREATE OR REPLACE FUNCTION status_invalidate(p_owner uuid,p_claim uuid,p_key text DEFAULT '') RETURNS void LANGUAGE plpgsql AS $$
DECLARE keys text[];
BEGIN
 IF NOT EXISTS(SELECT 1 FROM status_cards WHERE owner_id=p_owner) AND NOT EXISTS(SELECT 1 FROM handovers WHERE owner_id=p_owner) THEN RETURN; END IF;
 SELECT array_agg(sc.key ORDER BY sc.key) INTO keys FROM status_cards sc WHERE sc.owner_id=p_owner AND (
 sc.key=p_key OR EXISTS(SELECT 1 FROM status_current_members m WHERE m.owner_id=p_owner AND m.claim_id=p_claim AND m.key=sc.key)
 OR EXISTS(SELECT 1 FROM status_card_items i WHERE i.owner_id=p_owner AND i.claim_id=p_claim AND i.key=sc.key));
 PERFORM status_invalidate_keys(p_owner,keys);
END $$;
CREATE OR REPLACE FUNCTION status_claim_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v jsonb; p uuid; old_key text;
BEGIN
 -- Compared is batch bookkeeping. The comparer invalidates affected groups
 -- once after its bulk update, rather than enqueueing for every input row.
 IF TG_OP='UPDATE' AND TG_TABLE_NAME='claims' AND current_setting('pcas.defer_compared_invalidation',true)='on' AND
    (to_jsonb(NEW)-'compared')=(to_jsonb(OLD)-'compared') THEN RETURN NULL; END IF;
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

-- One general worker lane alternates ordinary cards and comparisons. Persist
-- the next lane across restarts; fresh input and self cards remain ahead of both.
CREATE TABLE background_dispatch (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),prefer_cards boolean NOT NULL DEFAULT true);
INSERT INTO background_dispatch(singleton) VALUES(true);
UPDATE memory_jobs SET priority=CASE WHEN stage LIKE 'memory.card:%:self:%' THEN 7 ELSE 8 END
 WHERE stage LIKE 'memory.card:%';
CREATE INDEX jobs_card_dispatch_ready_idx ON memory_jobs(priority,available_at,created_at,id)
 WHERE stage LIKE 'memory.card:%' AND state IN('queued','leased');
CREATE INDEX jobs_compare_dispatch_ready_idx ON memory_jobs(priority,available_at,created_at,id)
 WHERE (stage LIKE 'memory.compare:%' OR stage LIKE 'memory.entity_compare:%') AND state IN('queued','leased');
