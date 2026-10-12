package postgres

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// ChainDepthLimit is the depth of a causal chain of queue jobs above which a
// queued job is not run.
//
// Basis: the job history of the live instance on 2026-10-12, 166,049 jobs for
// 27,474 sources. One handler queues follow-on work in its own transaction in
// a few places only. The deepest is the extraction of an imported
// conversation, where each segment queues the next one. Of 1,447 imported
// conversations the longest had 37 segments (depth 36); the 99th percentile
// was depth 10. The statements of the last segment add one step. The limit is
// about five times the deepest real chain, so that a real long conversation
// passes and a loop of jobs that queue each other stops. The statement and
// the index of migration 064 hold the same number.
const ChainDepthLimit = 200

// EventRetention is the time that a recorded event is kept after its chain
// has no job that waits or runs.
//
// Basis: an event is evidence for the investigation of a chain. The job
// history above came from twelve days and its imports; at that rate 90 days
// are about 1.2 million rows of about 150 bytes. The jobs themselves are
// kept, with their root and their depth.
const EventRetention = 90 * 24 * time.Hour

var lastEventRetention atomic.Int64

// applyEventLimits stops queued jobs of a chain that is deeper than the
// limit, and removes old events once an hour. A stopped job is failed with
// the code chain_limit and counted as a failure of its stage, so that the
// background health shows it. It is not removed: the owner's retry command
// queues it again after the cause is found, with its root and depth kept.
func (s *Store) applyEventLimits(ctx context.Context) error {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `WITH stopped AS (
 UPDATE memory_jobs SET state='failed',error_code='chain_limit',lease_token=NULL,lease_until=NULL,updated_at=now()
 WHERE state='queued' AND chain_depth>200 RETURNING owner_id,split_part(stage,':',1) AS stage)
 INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count)
 SELECT owner_id,stage,'failure','chain_limit',count(*) FROM stopped GROUP BY owner_id,stage`)
		return err
	})
	if err != nil {
		return err
	}
	now := time.Now()
	last := lastEventRetention.Load()
	if now.Unix()-last < 3600 || !lastEventRetention.CompareAndSwap(last, now.Unix()) {
		return nil
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM recorded_events e WHERE e.occurred_at<now()-make_interval(secs=>$1)
 AND NOT EXISTS(SELECT 1 FROM memory_jobs j WHERE j.root_job_id=e.root_id AND j.state IN ('queued','leased'))`, EventRetention.Seconds())
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		slog.InfoContext(ctx, "recorded events removed after retention", "count", n)
	}
	return nil
}
