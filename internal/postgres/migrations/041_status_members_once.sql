-- Every changed memory worked out the whole members view once per card.
-- One memory's cards are now looked up directly, by the view's own rules.
CREATE FUNCTION status_claim_keys(p_owner uuid,p_claim uuid) RETURNS SETOF text LANGUAGE sql STABLE AS $$
 WITH c AS MATERIALIZED (
  SELECT r.owner_id,r.id AS claim_id,r.version AS claim_version,c.category,c.subject_id
  FROM memory_records r JOIN claims cl ON (cl.owner_id,cl.id)=(r.owner_id,r.id)
  JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
  JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
  WHERE r.owner_id=p_owner AND r.id=p_claim AND r.state='active' AND rv.state='active' AND cl.retired=''
  AND claim_source_is_current(r.owner_id,r.id,r.version,now())
 ), e AS (
  SELECT c.owner_id,cm.entity_id FROM c JOIN claim_mentions cm USING(owner_id,claim_id,claim_version)
  UNION SELECT owner_id,subject_id FROM c WHERE subject_id IS NOT NULL
 )
 SELECT 'entity:'||e.entity_id::text FROM e JOIN memory_records er ON(er.owner_id,er.id)=(e.owner_id,e.entity_id) AND er.state='active'
  JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version)
  WHERE ev.entity_type IN('project','topic','area','person')
 UNION ALL SELECT 'self:'||c.category FROM c WHERE c.category IN('identity','taste','rule','goal')
$$;

CREATE OR REPLACE FUNCTION status_invalidate(p_owner uuid,p_claim uuid,p_key text DEFAULT '') RETURNS void LANGUAGE plpgsql AS $$
DECLARE keys text[];
BEGIN
 IF NOT EXISTS(SELECT 1 FROM status_cards WHERE owner_id=p_owner) AND NOT EXISTS(SELECT 1 FROM handovers WHERE owner_id=p_owner) THEN RETURN; END IF;
 SELECT array_agg(sc.key ORDER BY sc.key) INTO keys FROM status_cards sc WHERE sc.owner_id=p_owner AND sc.key IN(
  SELECT p_key
  UNION ALL SELECT status_claim_keys(p_owner,p_claim)
  UNION ALL SELECT i.key FROM status_card_items i WHERE i.owner_id=p_owner AND i.claim_id=p_claim);
 PERFORM status_invalidate_keys(p_owner,keys);
END $$;
