-- Freeze status cards and their queue. Preserve both tables and every item.
CREATE OR REPLACE FUNCTION status_invalidate_keys(p_owner uuid, p_keys text[]) RETURNS void LANGUAGE plpgsql AS $$
BEGIN RETURN; END $$;
CREATE OR REPLACE FUNCTION status_invalidate(p_owner uuid,p_claim uuid,p_key text DEFAULT '') RETURNS void LANGUAGE plpgsql AS $$
BEGIN RETURN; END $$;
CREATE OR REPLACE FUNCTION status_card_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RETURN NULL; END $$;
UPDATE memory_jobs SET state='done',error_code='status_cards_removed',lease_token=NULL,lease_until=NULL,updated_at=now()
 WHERE stage LIKE 'memory.card:%' AND state IN('queued','leased','blocked','failed');

-- Legacy card pointers may outlive deleted memories/entities while frozen.
ALTER TABLE status_card_items DROP CONSTRAINT status_card_items_owner_id_claim_id_claim_version_fkey;
ALTER TABLE status_cards DROP CONSTRAINT status_cards_owner_id_entity_id_fkey;
DROP TRIGGER status_claims_changed ON claims;
DROP TRIGGER status_revision_changed ON claim_revisions;
DROP TRIGGER status_mentions_changed ON claim_mentions;
DROP TRIGGER status_record_changed ON memory_records;
DROP TRIGGER status_record_deleted ON memory_records;
DROP TRIGGER status_source_deleted ON memory_records;
DROP TRIGGER status_source_updated ON memory_records;
DROP TRIGGER status_evidence_mutated ON evidence;
DROP TRIGGER status_card_rebuilt ON status_cards;

-- The membership view is now the sole source for all membership consumers.
CREATE OR REPLACE FUNCTION status_claim_keys(p_owner uuid,p_claim uuid) RETURNS SETOF text LANGUAGE sql STABLE AS $$
 SELECT key FROM status_current_members WHERE owner_id=p_owner AND claim_id=p_claim
$$;

ALTER TABLE deadlines ADD COLUMN original_text text NOT NULL DEFAULT '';
ALTER TABLE deadlines DROP CONSTRAINT deadlines_kind_check;
ALTER TABLE deadlines ADD CONSTRAINT deadlines_kind_check CHECK(kind IN('deadline','appointment','recurring','unclear'));
CREATE TABLE assistant_requirements (
 owner_id uuid NOT NULL, claim_id uuid NOT NULL, claim_version integer NOT NULL,
 unrestricted boolean NOT NULL, scope text NOT NULL,
 PRIMARY KEY(owner_id,claim_id,claim_version),
 FOREIGN KEY(owner_id,claim_id,claim_version) REFERENCES claim_revisions ON DELETE CASCADE
);
-- Old card rows are only a migration source, never a runtime source.
INSERT INTO assistant_requirements(owner_id,claim_id,claim_version,unrestricted,scope)
 SELECT DISTINCT ON(owner_id,claim_id,claim_version) owner_id,claim_id,claim_version,btrim(applies_to)='',applies_to
 FROM status_card_items WHERE key='self:rule' ORDER BY owner_id,claim_id,claim_version,position;
ALTER TABLE handovers ADD COLUMN input_hash text NOT NULL DEFAULT '';
ALTER TABLE claims ADD COLUMN organize_after timestamptz NOT NULL DEFAULT '-infinity';

-- Keep invocation budgets valid even when completed queue entries are deleted.
ALTER TABLE background_usage ADD COLUMN stage text NOT NULL DEFAULT '';
UPDATE background_usage b SET stage=split_part(j.stage,':',1) FROM memory_jobs j WHERE j.id=b.job_id;
CREATE INDEX background_usage_stage_time_idx ON background_usage(stage,created_at);

-- Paid model results survive both write retries and worker recovery.
CREATE TABLE background_model_results (
 owner_id uuid NOT NULL, job_id uuid PRIMARY KEY, purpose text NOT NULL,
 prompt jsonb NOT NULL, output text NOT NULL, reservation_id uuid NOT NULL,
 provider_id text NOT NULL, model text NOT NULL, input_tokens integer NOT NULL,
 output_tokens integer NOT NULL, cost numeric NOT NULL, refs jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);

-- Recent identity/goal/taste means expressed within 30 days; repeated means at
-- least two independent sources. Requirements and deadlines have no age cap.
CREATE FUNCTION library_handover_members(p_owner uuid) RETURNS TABLE(claim_id uuid,claim_version integer) LANGUAGE sql STABLE AS $$
 SELECT DISTINCT m.claim_id,m.claim_version FROM status_current_members m
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(m.owner_id,m.claim_id,m.claim_version)
 WHERE m.owner_id=p_owner AND (m.key='self:rule' OR (m.key IN('self:identity','self:goal','self:taste') AND (
 rv.expressed_at>=now()-interval '30 days' OR (SELECT count(DISTINCT coalesce(nullif(sc.conversation_key,''),e.source_id::text))
 FROM evidence e LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version)
 WHERE e.owner_id=m.owner_id AND e.target_id=m.claim_id AND e.target_version=m.claim_version AND e.stance='supports')>=2)))
 UNION
 SELECT d.claim_id,d.claim_version FROM deadlines d JOIN memory_records r ON(r.owner_id,r.id,r.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 WHERE d.owner_id=p_owner AND r.state='active' AND cl.retired='' AND claim_source_is_current(r.owner_id,r.id,r.version,now())
$$;
CREATE FUNCTION library_handover_hash(p_owner uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT md5(jsonb_build_object(
 'memories',coalesce((SELECT jsonb_agg(jsonb_build_array(m.claim_id,m.claim_version,c.category,c.value,rv.expressed_at) ORDER BY m.claim_id)
 FROM library_handover_members(p_owner) m JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(p_owner,m.claim_id,m.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(p_owner,m.claim_id,m.claim_version)),'[]'::jsonb),
 'requirements',coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.claim_id) FROM assistant_requirements a
 JOIN library_handover_members(p_owner) m ON(m.claim_id,m.claim_version)=(a.claim_id,a.claim_version) WHERE a.owner_id=p_owner),'[]'::jsonb),
 'deadlines',coalesce((SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM deadlines d
 JOIN library_handover_members(p_owner) m ON(m.claim_id,m.claim_version)=(d.claim_id,d.claim_version) WHERE d.owner_id=p_owner),'[]'::jsonb))::text)
$$;

-- Stable 100-member blocks ensure an insertion only changes its own block.
-- Every pair of blocks meets in one <=200-memory comparison invocation.
CREATE TABLE memory_comparison_members (
 owner_id uuid NOT NULL, group_key text NOT NULL, claim_id uuid NOT NULL,
 position bigint NOT NULL,
 PRIMARY KEY(owner_id,group_key,claim_id), UNIQUE(owner_id,group_key,position),
 FOREIGN KEY(owner_id,claim_id) REFERENCES claims ON DELETE CASCADE
);
CREATE TABLE memory_comparison_batches (
 owner_id uuid NOT NULL, group_key text NOT NULL, block_a bigint NOT NULL, block_b bigint NOT NULL,
 fingerprint text NOT NULL, rule integer NOT NULL,
 attempts integer NOT NULL DEFAULT 0, retry_after timestamptz NOT NULL DEFAULT '-infinity',
 completed_at timestamptz,
 PRIMARY KEY(owner_id,group_key,block_a,block_b)
);
CREATE TABLE memory_group_progress (
 owner_id uuid NOT NULL, group_key text NOT NULL, claim_id uuid NOT NULL,
 claim_version integer NOT NULL, rule integer NOT NULL, compared_at timestamptz NOT NULL,
 PRIMARY KEY(owner_id,group_key,claim_id),
 FOREIGN KEY(owner_id,claim_id) REFERENCES claims ON DELETE CASCADE
);

CREATE TABLE background_stage_events (
 id bigserial PRIMARY KEY, owner_id uuid, job_id uuid,
 stage text NOT NULL, outcome text NOT NULL CHECK(outcome IN('success','deferred','failure','overflow')),
 reason text NOT NULL DEFAULT '', count integer NOT NULL DEFAULT 1 CHECK(count>=0),
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX background_stage_events_recent_idx ON background_stage_events(owner_id,stage,at);
CREATE TABLE background_job_deferrals (
 job_id uuid PRIMARY KEY, started_at timestamptz NOT NULL, warned boolean NOT NULL DEFAULT false
);
CREATE FUNCTION record_background_job_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE outcome text;
BEGIN
 IF NEW.state='done' AND OLD.state='leased' THEN outcome='success';
 ELSIF OLD.state='leased' AND NEW.state='queued' AND NEW.error_code<>'' THEN outcome='deferred';
 ELSIF OLD.state='leased' AND NEW.state IN('blocked','failed') THEN outcome='failure';
 ELSE RETURN NULL; END IF;
 INSERT INTO background_stage_events(owner_id,job_id,stage,outcome,reason)
 VALUES(NEW.owner_id,NEW.id,split_part(NEW.stage,':',1),outcome,NEW.error_code);
 IF outcome='deferred' THEN
 INSERT INTO background_job_deferrals(job_id,started_at) VALUES(NEW.id,clock_timestamp()) ON CONFLICT(job_id) DO NOTHING;
 ELSE DELETE FROM background_job_deferrals WHERE job_id=NEW.id; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER background_job_event AFTER UPDATE OF state ON memory_jobs FOR EACH ROW EXECUTE FUNCTION record_background_job_event();
