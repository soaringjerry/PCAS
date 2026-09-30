-- An automatically extracted claim cannot outlive the source version that
-- supports it in the current view. Keep old claims and evidence for historical
-- inspection; explicit user corrections/confirmations remain independent.
CREATE FUNCTION claim_source_is_current(p_owner uuid,p_claim uuid,p_version integer,p_known timestamptz)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT NOT EXISTS (
   SELECT 1 FROM record_versions rv
   WHERE rv.owner_id=p_owner AND rv.record_id=p_claim AND rv.version=p_version
     AND rv.actor='ai'
     AND EXISTS(SELECT 1 FROM claim_source_keys k WHERE k.owner_id=p_owner AND k.claim_id=p_claim)
 ) OR EXISTS (
   SELECT 1 FROM evidence e
   JOIN memory_records s ON (s.owner_id,s.id)=(e.owner_id,e.source_id)
   WHERE e.owner_id=p_owner AND e.target_id=p_claim AND e.target_version=p_version
     AND e.stance='supports' AND s.state='active'
     AND e.source_version=(
       SELECT max(v.version) FROM record_versions v
       WHERE v.owner_id=e.owner_id AND v.record_id=e.source_id
         AND v.recorded_at<=p_known AND v.state='active'
     )
 )
$$;

CREATE OR REPLACE FUNCTION applicable_claim_versions(p_owner uuid,p_valid timestamptz,p_known timestamptz)
RETURNS TABLE(claim_id uuid,version integer)
LANGUAGE sql STABLE AS $$
 WITH known AS (
   SELECT c.claim_id,c.version,rv.valid_from,rv.valid_to,
     sum(CASE WHEN c.change_type IN ('initial','change') THEN 1 ELSE 0 END)
       OVER(PARTITION BY c.claim_id ORDER BY c.version) AS change_group
   FROM claim_revisions c JOIN record_versions rv
     ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
   JOIN memory_records r ON (r.owner_id,r.id)=(c.owner_id,c.claim_id)
   WHERE c.owner_id=p_owner AND rv.recorded_at<=p_known AND rv.state='active' AND r.state='active'
 ), corrected AS (
   SELECT DISTINCT ON(claim_id,change_group) * FROM known ORDER BY claim_id,change_group,version DESC
 )
 SELECT DISTINCT ON(claim_id) claim_id,version FROM corrected
 WHERE (valid_from IS NULL OR valid_from<=p_valid) AND (valid_to IS NULL OR valid_to>p_valid)
   AND claim_source_is_current(p_owner,claim_id,version,p_known)
 ORDER BY claim_id,version DESC
$$;
