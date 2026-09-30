-- Server-owned replies, scoped to owner and assistant. Client text is never
-- accepted as the provenance of an earlier answer.
CREATE TABLE desk_turns (
 owner_id uuid NOT NULL REFERENCES workspace_owners(owner_id),
 id uuid NOT NULL,
 agent_id text NOT NULL,
 question text NOT NULL,
 answer text NOT NULL,
 dependencies jsonb NOT NULL DEFAULT '[]',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(owner_id,id)
);
