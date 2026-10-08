-- A group of memories judged not to be a project (a trip that is over, a mere
-- topic) keeps that verdict with the evidence it was given on, so it is not
-- asked about again until what is said of it changes.
ALTER TABLE topic_project_checks ADD COLUMN evidence_hash text NOT NULL DEFAULT '';

-- Groups queued under the old rule (any topic with a goal and a date, in no
-- order) are taken off the queue; the scheduler queues again by the new rule,
-- the most recently spoken of first.
DELETE FROM memory_jobs WHERE stage LIKE 'memory.topic_project:%' AND state='queued';

-- Projects the background made from mere topics under the old rule (a rail
-- trip of last June, 公共交通) that nobody has touched since are taken back,
-- the same way undoing them one by one would: the group is no longer tied to
-- the project, and the empty project is removed. A project with anything in
-- it, or changed by the user, stays.
CREATE TEMP TABLE topic_projects_withdrawn AS
 SELECT l.owner_id,l.project_id FROM topic_project_links l
 JOIN memory_records r ON r.owner_id=l.owner_id AND r.id=l.topic_id
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(r.owner_id,r.id,r.version)
 JOIN work_items w ON w.owner_id=l.owner_id AND w.id=l.project_id
 WHERE l.created_project AND l.undone_at IS NULL AND ev.entity_type='topic' AND w.kind='project' AND w.version=1
 AND NOT EXISTS(SELECT 1 FROM work_items c WHERE c.owner_id=w.owner_id AND c.project_id=w.id)
 AND NOT EXISTS(SELECT 1 FROM work_documents d WHERE d.owner_id=w.owner_id AND d.thing_id=w.id)
 AND NOT EXISTS(SELECT 1 FROM agent_runs a WHERE a.owner_id=w.owner_id AND a.thing_id=w.id);
UPDATE entity_versions ev SET disambiguation=ev.disambiguation-'work_item_id' FROM topic_projects_withdrawn x
 WHERE ev.owner_id=x.owner_id AND ev.disambiguation->>'work_item_id'=x.project_id::text;
UPDATE action_log a SET undone_at=now() FROM topic_project_links l JOIN topic_projects_withdrawn x ON(l.owner_id,l.project_id)=(x.owner_id,x.project_id)
 WHERE(a.owner_id,a.id)=(l.owner_id,l.action_id) AND a.undone_at IS NULL;
UPDATE topic_project_links l SET undone_at=now() FROM topic_projects_withdrawn x WHERE(l.owner_id,l.project_id)=(x.owner_id,x.project_id) AND l.undone_at IS NULL;
DELETE FROM work_items w USING topic_projects_withdrawn x WHERE(w.owner_id,w.id)=(x.owner_id,x.project_id);
DROP TABLE topic_projects_withdrawn;
