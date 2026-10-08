-- A 5,000-memory library exceeded the existing two-second scheduling write
-- window when the hash expanded library_handover_members separately for each
-- of its three projections. Evaluate the same complete membership once; keep
-- the JSON shape, ordering and scope-sensitive hash from migration 056.
CREATE OR REPLACE FUNCTION library_handover_hash(p_owner uuid) RETURNS text LANGUAGE sql STABLE AS $$
 WITH members AS MATERIALIZED(SELECT * FROM library_handover_members(p_owner))
 SELECT md5(jsonb_build_object(
 'memories',coalesce((SELECT jsonb_agg(jsonb_build_array(m.claim_id,m.claim_version,c.category,c.value,c.scope,rv.expressed_at) ORDER BY m.claim_id)
 FROM members m JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(p_owner,m.claim_id,m.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(p_owner,m.claim_id,m.claim_version)),'[]'::jsonb),
 'requirements',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.claim_id) FROM assistant_requirements a
 JOIN members m ON(m.claim_id,m.claim_version)=(a.claim_id,a.claim_version) WHERE a.owner_id=p_owner),'[]'::jsonb),
 'deadlines',coalesce((SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM deadlines d
 JOIN members m ON(m.claim_id,m.claim_version)=(d.claim_id,d.claim_version) WHERE d.owner_id=p_owner),'[]'::jsonb))::text)
$$;
