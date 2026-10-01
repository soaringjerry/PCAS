-- Admission metadata only: raw requests and replies remain in their existing
-- source/desk_turns stores so deletion propagation has no second body to scrub.
CREATE TABLE desk_turn_order (
 owner_id uuid NOT NULL REFERENCES workspace_owners(owner_id),
 request_id uuid NOT NULL,
 conversation_id uuid NOT NULL,
 request_hash bytea NOT NULL,
 admission_order bigint GENERATED ALWAYS AS IDENTITY,
 creator_id uuid NOT NULL,
 accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','done','canceled','failed','expired')),
 PRIMARY KEY(owner_id,request_id)
);
CREATE INDEX desk_turn_order_pending_idx ON desk_turn_order(owner_id,conversation_id,admission_order) WHERE status='pending';
