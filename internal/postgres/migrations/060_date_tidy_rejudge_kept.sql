-- What the tidy keeps changed: of rows with the same title that say one thing
-- only the latest stays, and a row that does not say which thing it means is
-- no longer left for the user to guess. Rows judged "keep" under the old
-- wording are judged once more; what was put away, made into a to-do or an
-- idea, or put back by the user stays as it is.
DELETE FROM memory_jobs j USING date_tidy_checks k
 WHERE k.decision='keep' AND j.owner_id=k.owner_id AND j.record_id=k.claim_id AND j.record_version=k.claim_version
 AND j.stage='memory.date_tidy:'||k.class AND j.state='done';
DELETE FROM date_tidy_checks WHERE decision='keep';
