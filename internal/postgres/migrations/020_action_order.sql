-- now() is shared by every action in a transaction. Persist insertion order for
-- new actions while leaving historical rows NULL: their true tied order cannot
-- be reconstructed from random UUIDs or physical row locations.
CREATE SEQUENCE action_log_order_seq AS bigint;
ALTER TABLE action_log ADD COLUMN action_order bigint;
ALTER TABLE action_log ALTER COLUMN action_order SET DEFAULT nextval('action_log_order_seq');
ALTER SEQUENCE action_log_order_seq OWNED BY action_log.action_order;
CREATE UNIQUE INDEX action_log_order_idx ON action_log(action_order) WHERE action_order IS NOT NULL;
CREATE INDEX action_log_pending_order_idx ON action_log(owner_id,action_order) WHERE undone_at IS NULL;
