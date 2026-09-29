-- Evaluate what was applicable, using only revisions already known at the
-- requested system time. Corrections supersede their change group without
-- mutating the original revision's time bounds.
CREATE FUNCTION applicable_claim_versions(p_owner uuid,p_valid timestamptz,p_known timestamptz)
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
 ORDER BY claim_id,version DESC
$$;
