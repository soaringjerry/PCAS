-- No request or reply bodies here. Closed groups fence delayed retries forever.
CREATE TABLE desk_smoke_groups (
 owner_id uuid NOT NULL REFERENCES workspace_owners(owner_id),
 id uuid NOT NULL,
 closed boolean NOT NULL DEFAULT false,
 PRIMARY KEY(owner_id,id)
);
CREATE TABLE desk_smoke_requests (
 owner_id uuid NOT NULL,
 request_id uuid NOT NULL,
 smoke_id uuid NOT NULL,
 PRIMARY KEY(owner_id,request_id),
 FOREIGN KEY(owner_id,smoke_id) REFERENCES desk_smoke_groups(owner_id,id)
);
CREATE INDEX desk_smoke_requests_group_idx ON desk_smoke_requests(owner_id,smoke_id);

-- Temporary rollback data. Unlike ordinary undo, check cleanup has no age
-- limit. All documents below are removed on successful cleanup.
CREATE TABLE desk_smoke_actions (
 owner_id uuid NOT NULL,
 action_id uuid NOT NULL,
 smoke_id uuid NOT NULL,
 PRIMARY KEY(owner_id,action_id),
 FOREIGN KEY(owner_id,smoke_id) REFERENCES desk_smoke_groups(owner_id,id)
);
CREATE TABLE desk_smoke_changes (
 owner_id uuid NOT NULL,
 smoke_id uuid NOT NULL,
 table_name text NOT NULL CHECK(table_name IN ('work_items','work_documents','agent_runs','training_samples')),
 record_id uuid NOT NULL,
 before_document jsonb NOT NULL,
 after_hash text,
 action_order bigint NOT NULL,
 PRIMARY KEY(owner_id,smoke_id,table_name,record_id),
 FOREIGN KEY(owner_id,smoke_id) REFERENCES desk_smoke_groups(owner_id,id)
);
