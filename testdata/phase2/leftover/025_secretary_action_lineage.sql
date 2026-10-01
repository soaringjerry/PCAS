-- Server-only origin metadata survives expiry of undo/body snapshots. Existing
-- rows remain legacy; no historical recipient or field provenance is invented.
ALTER TABLE action_log ADD COLUMN context_task jsonb
 CHECK (context_task IS NULL OR jsonb_typeof(context_task)='object');
ALTER TABLE action_log ADD COLUMN context_stale boolean NOT NULL DEFAULT false;
