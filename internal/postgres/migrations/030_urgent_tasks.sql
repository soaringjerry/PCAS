-- To-dos carry an "urgent" mark in their document. Open to-dos without a time
-- whose title already says they cannot wait start out marked.
UPDATE work_items
SET document = document || '{"urgent": true}'::jsonb
WHERE kind = 'task'
  AND status NOT IN ('done', 'cancelled')
  AND due_at IS NULL
  AND scheduled_at IS NULL
  AND title ~ '尽快|不能拖|不能再拖|马上|赶紧|抓紧|越快越好';
