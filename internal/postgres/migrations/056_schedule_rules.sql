-- Occurrences are expanded in read queries and are never stored.
ALTER TABLE deadlines ADD COLUMN schedule_rule jsonb;
ALTER TABLE deadlines ADD COLUMN date_only boolean NOT NULL DEFAULT false;
-- Legacy writers encoded an unspecified clock as local midnight with a note.
UPDATE deadlines d SET date_only=true FROM workspace_owners o
 WHERE o.owner_id=d.owner_id AND d.at IS NOT NULL AND d.time_note<>''
 AND (d.at AT TIME ZONE coalesce(o.settings->>'timezone','UTC'))::time='00:00';
-- Only owners of legacy recurring rows need normalization, through the existing
-- organize budget. No unrelated memory is relabelled.
UPDATE claims cl SET organized=0,organize_after='-infinity'
 WHERE EXISTS(SELECT 1 FROM deadlines d WHERE(d.owner_id,d.claim_id)=(cl.owner_id,cl.id) AND d.kind='recurring');

CREATE OR REPLACE FUNCTION library_handover_hash(p_owner uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT md5(jsonb_build_object(
 'memories',coalesce((SELECT jsonb_agg(jsonb_build_array(m.claim_id,m.claim_version,c.category,c.value,c.scope,rv.expressed_at) ORDER BY m.claim_id)
 FROM library_handover_members(p_owner) m JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(p_owner,m.claim_id,m.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(p_owner,m.claim_id,m.claim_version)),'[]'::jsonb),
 'requirements',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.claim_id) FROM assistant_requirements a
 JOIN library_handover_members(p_owner) m ON(m.claim_id,m.claim_version)=(a.claim_id,a.claim_version) WHERE a.owner_id=p_owner),'[]'::jsonb),
 'deadlines',coalesce((SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM deadlines d
 JOIN library_handover_members(p_owner) m ON(m.claim_id,m.claim_version)=(d.claim_id,d.claim_version) WHERE d.owner_id=p_owner),'[]'::jsonb))::text)
$$;
