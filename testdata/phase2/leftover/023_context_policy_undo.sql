-- Undoing a first grant restores absence for independently authorized claims;
-- keep the monotonic revision without pretending that absence was an owner deny.
ALTER TABLE source_authorizations ADD COLUMN explicit_deny boolean NOT NULL DEFAULT false;
UPDATE source_authorizations SET explicit_deny=revoked;
ALTER TABLE source_authorizations ADD CONSTRAINT source_policy_deny_is_revoked CHECK (NOT explicit_deny OR revoked);

-- Exact trusted destination/view for later history checks. This is server-owned
-- provenance, never accepted as a replacement task context from client JSON.
ALTER TABLE desk_turns ADD COLUMN context_task jsonb CHECK (context_task IS NULL OR jsonb_typeof(context_task)='object');
