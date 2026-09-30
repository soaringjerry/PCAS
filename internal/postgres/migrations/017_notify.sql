ALTER TABLE workspace_notices
    ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN dismissed_at timestamptz,
    ADD COLUMN delivered jsonb NOT NULL DEFAULT '{}';
CREATE UNIQUE INDEX workspace_notices_id ON workspace_notices(id);
CREATE INDEX workspace_notices_pending ON workspace_notices(created_at) WHERE dismissed_at IS NULL;
CREATE TABLE push_subscriptions (
    owner_id uuid NOT NULL REFERENCES workspace_owners(owner_id) ON DELETE CASCADE,
    endpoint text NOT NULL,
    p256dh text NOT NULL,
    auth text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_id, endpoint)
);
