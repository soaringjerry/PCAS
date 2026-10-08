-- A group of memories judged not to be a project (a trip that is over, a mere
-- topic) keeps that verdict with the evidence it was given on, so it is not
-- asked about again until what is said of it changes.
ALTER TABLE topic_project_checks ADD COLUMN evidence_hash text NOT NULL DEFAULT '';
