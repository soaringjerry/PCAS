CREATE TABLE workspace_reviews (
    owner_id uuid NOT NULL REFERENCES workspace_owners(owner_id) ON DELETE CASCADE,
    due_at timestamptz NOT NULL,
    pending_count integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_id,due_at)
);
