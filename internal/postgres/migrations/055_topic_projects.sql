CREATE TABLE topic_project_checks (
 owner_id uuid NOT NULL REFERENCES workspace_owners ON DELETE CASCADE,
 topic_id uuid NOT NULL,input_hash text NOT NULL,page integer NOT NULL,
 output jsonb NOT NULL,PRIMARY KEY(owner_id,topic_id,input_hash,page)
);
CREATE TABLE topic_project_links (
 owner_id uuid NOT NULL REFERENCES workspace_owners ON DELETE CASCADE,
 topic_id uuid NOT NULL,input_hash text NOT NULL,evidence_hash text NOT NULL,project_id uuid NOT NULL,
 action_id uuid NOT NULL,created_project boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),undone_at timestamptz,
 PRIMARY KEY(owner_id,topic_id,input_hash),UNIQUE(owner_id,action_id)
);
-- Topic links are structural project membership, just like project groups.
CREATE OR REPLACE FUNCTION project_claim_projects(p_owner uuid,p_claim uuid) RETURNS TABLE(project_id uuid) LANGUAGE sql STABLE AS $$
 SELECT DISTINCT w.id FROM work_items w WHERE w.owner_id=p_owner AND w.kind='project' AND (
 EXISTS(SELECT 1 FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.scope->>'project_id'=w.id::text)
 OR EXISTS(SELECT 1 FROM entity_versions ev WHERE ev.owner_id=p_owner AND ev.entity_type IN('project','topic') AND ev.disambiguation->>'work_item_id'=w.id::text AND (
 EXISTS(SELECT 1 FROM claim_revisions c WHERE c.owner_id=p_owner AND c.claim_id=p_claim AND c.subject_id=ev.entity_id)
 OR EXISTS(SELECT 1 FROM claim_mentions m WHERE m.owner_id=p_owner AND m.claim_id=p_claim AND m.entity_id=ev.entity_id))))
$$;
ALTER TABLE model_usage DROP CONSTRAINT model_usage_purpose_check;
ALTER TABLE model_usage ADD CONSTRAINT model_usage_purpose_check CHECK(purpose IN
 ('secretary','deputy','answer','extraction','embedding','vision','organize','compare','entity_compare','entity_candidates','card','handover','selector','reader','selfcheck','alias_scan','alias_confirm','query_embedding','transcription','project_handover','effort','topic_project'));
