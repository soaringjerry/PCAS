-- Ownership blocks are maintained on every edit, not rediscovered by searching
-- for an old generated string. Legacy fields with ambiguous ownership remain
-- quarantined for owner review rather than being silently destroyed.
CREATE TABLE artifact_fields (
 owner_id uuid NOT NULL,
 thing_id uuid NOT NULL,
 field text NOT NULL,
 blocks jsonb NOT NULL,
 PRIMARY KEY(owner_id,thing_id,field),
 FOREIGN KEY(owner_id,thing_id) REFERENCES work_items ON DELETE CASCADE
);
CREATE TABLE retained_artifact_writing (
 owner_id uuid NOT NULL,
 thing_id uuid NOT NULL,
 field text NOT NULL,
 body text NOT NULL,
 reason text NOT NULL,
 PRIMARY KEY(owner_id,thing_id,field),
 FOREIGN KEY(owner_id,thing_id) REFERENCES work_items ON DELETE CASCADE
);
