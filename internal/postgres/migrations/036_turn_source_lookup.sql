-- Source-currentness and correction/deletion look up keys by claim identity.
-- The existing primary key starts with source_id and cannot bound that lookup.
CREATE INDEX claim_source_keys_claim_idx ON claim_source_keys(owner_id,claim_id);

-- Workspace cards read the newest 200 visible memories. Preserve the established
-- descending update time / ascending identity order while stopping early.
CREATE INDEX memory_records_active_updated_idx
    ON memory_records(owner_id,updated_at DESC,id) WHERE state='active';
