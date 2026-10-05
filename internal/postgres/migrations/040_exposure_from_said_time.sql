-- Memories filed from imported history were stamped as last used at filing
-- time, so every one of them showed full exposure. Where nothing has actually
-- used a memory since, its last use is when it was said.
UPDATE activity a SET last_effective_use_at = rv.expressed_at
 FROM memory_records r
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE (a.owner_id,a.record_id)=(r.owner_id,r.id) AND r.kind='claim'
   AND rv.expressed_at IS NOT NULL AND rv.expressed_at < a.last_effective_use_at
   AND NOT EXISTS (SELECT 1 FROM use_events u WHERE u.owner_id=a.owner_id AND u.record_id=a.record_id);
