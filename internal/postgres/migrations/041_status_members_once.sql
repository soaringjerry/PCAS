-- Every changed memory worked out the whole members view once per card, and
-- the view's shared sub-query could not be narrowed to the one memory asked for.
CREATE OR REPLACE VIEW status_current_members AS
WITH current_claims AS NOT MATERIALIZED (
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

CREATE OR REPLACE FUNCTION status_invalidate(p_owner uuid,p_claim uuid,p_key text DEFAULT '') RETURNS void LANGUAGE plpgsql AS $$
DECLARE keys text[];
BEGIN
 IF NOT EXISTS(SELECT 1 FROM status_cards WHERE owner_id=p_owner) AND NOT EXISTS(SELECT 1 FROM handovers WHERE owner_id=p_owner) THEN RETURN; END IF;
 SELECT array_agg(sc.key ORDER BY sc.key) INTO keys FROM status_cards sc WHERE sc.owner_id=p_owner AND sc.key IN(
  SELECT p_key
  UNION ALL SELECT m.key FROM status_current_members m WHERE m.owner_id=p_owner AND m.claim_id=p_claim
  UNION ALL SELECT i.key FROM status_card_items i WHERE i.owner_id=p_owner AND i.claim_id=p_claim);
 PERFORM status_invalidate_keys(p_owner,keys);
END $$;
