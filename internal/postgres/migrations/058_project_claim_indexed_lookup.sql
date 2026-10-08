-- Migration 055 added topic links but restored the per-project scans that 053
-- had removed. Keep both group types and all historical memberships while
-- starting from the changed claim's indexed scope, subject and mentions.
CREATE OR REPLACE FUNCTION project_claim_projects(p_owner uuid,p_claim uuid) RETURNS TABLE(project_id uuid) LANGUAGE sql STABLE AS $$
 SELECT DISTINCT w.id FROM work_items w
 WHERE w.owner_id=p_owner AND w.kind='project' AND w.id::text IN (
  SELECT c.scope->>'project_id' FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.scope->>'project_id' IS NOT NULL
  UNION
  SELECT ev.disambiguation->>'work_item_id' FROM entity_versions ev
  WHERE ev.owner_id=p_owner AND ev.entity_type IN('project','topic') AND ev.disambiguation->>'work_item_id' IS NOT NULL AND ev.entity_id IN (
   SELECT c.subject_id FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.subject_id IS NOT NULL
   UNION SELECT m.entity_id FROM claim_mentions m WHERE m.owner_id=p_owner AND m.claim_id=p_claim))
$$;
