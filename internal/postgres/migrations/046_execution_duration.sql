-- Unknown historical durations remain NULL; exclude them from latency statistics.
ALTER TABLE model_usage ADD COLUMN duration_ms bigint CHECK(duration_ms >= 0);
-- Paid replay keeps the original measurement, never the replay's database time.
ALTER TABLE background_model_results ADD COLUMN duration_ms bigint CHECK(duration_ms >= 0);

CREATE TABLE execution_timings (
 owner_id uuid NOT NULL REFERENCES workspace_owners(owner_id) ON DELETE CASCADE,
 id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('secretary','deputy')),
 tier text NOT NULL CHECK(tier IN ('light','medium','heavy','')),
 at timestamptz NOT NULL DEFAULT now(),
 prepare_ms bigint NOT NULL CHECK(prepare_ms >= 0),
 answer_ms bigint NOT NULL CHECK(answer_ms >= 0),
 selfcheck_ms bigint NOT NULL CHECK(selfcheck_ms >= 0),
 writeback_ms bigint NOT NULL CHECK(writeback_ms >= 0),
 model_ms bigint NOT NULL CHECK(model_ms >= 0),
 total_ms bigint NOT NULL CHECK(total_ms >= 0),
 other_ms bigint NOT NULL CHECK(other_ms >= 0),
 PRIMARY KEY(owner_id,kind,id)
);
CREATE INDEX execution_timings_owner_at ON execution_timings(owner_id,at);
