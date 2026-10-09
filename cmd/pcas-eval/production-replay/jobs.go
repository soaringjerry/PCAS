package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Only an explicitly selected queued job in the verified disposable copy is
// leased. Completed, failed, blocked, or still-active jobs are never overwritten.
// These copy-only fixture writes are not a production queue entry point.
func leaseNamed(ctx context.Context, pool *pgxpool.Pool, owner memory.ID, op operation, ref memory.Ref) (worker.Job, error) {
	var job worker.Job
	err := pool.QueryRow(ctx, `WITH candidate AS (
 SELECT j.id,coalesce(r.kind,'source') AS kind FROM memory_jobs j
 LEFT JOIN memory_records r ON(r.owner_id,r.id)=(j.owner_id,j.record_id)
 WHERE j.owner_id=$1 AND j.state='queued' AND j.available_at<=now() AND j.stage=$2
 AND (($3::uuid IS NOT NULL AND j.id=$3::uuid) OR ($3::uuid IS NULL AND j.record_id=$4::uuid AND j.record_version=$5))
 FOR UPDATE OF j
 ),leased AS (
 UPDATE memory_jobs j SET state='leased',attempts=j.attempts+1,lease_token=$6::uuid,
 lease_until=clock_timestamp()+interval '5 minutes',updated_at=now()
 FROM candidate c WHERE j.id=c.id RETURNING j.id,j.owner_id,j.record_id,j.record_version,j.stage,j.attempts,j.lease_token,c.kind)
 SELECT id::text,owner_id::text,record_id::text,record_version,stage,attempts,lease_token::text,kind FROM leased`, string(owner), op.Stage, nullableID(op.JobID), nullableID(ref.ID), ref.Version, string(op.LeaseToken)).Scan(&job.ID, &job.OwnerID, &job.Record.ID, &job.Record.Version, &job.Stage, &job.Attempts, &job.LeaseToken, &job.Record.Kind)
	return job, err
}

func nullableID(id memory.ID) any {
	if id == "" {
		return nil
	}
	return string(id)
}

func supportedStage(stage string) bool {
	switch strings.SplitN(stage, ":", 2)[0] {
	case "source.chunk", "source.extract", "source.tokenize", "source.embed", "memory.organize", "memory.compare", "memory.entity_candidates", "memory.entity_compare", "memory.handover", "memory.project_handover", "memory.topic_project", "memory.date_tidy", "memory.effort":
		return true
	}
	return false
}

// Restrict the existing worker to one already selected fixture job. Its normal
// retry/block/defer policy remains in Worker.RunOnce and Store, not this tool.
type selectedQueue struct {
	*postgres.Store
	job *worker.Job
}

func (q *selectedQueue) Claim(context.Context, time.Duration) (*worker.Job, error) {
	job := q.job
	q.job = nil
	return job, nil
}

func processNamed(ctx context.Context, s *postgres.Store, job worker.Job) error {
	handlers := map[string]worker.Handler{"source.chunk": s.ProcessChunks, "source.extract": s.ProcessExtraction, "source.tokenize": s.ProcessIndex, "source.embed": s.ProcessEmbedding, "memory.organize": s.ProcessOrganize, "memory.compare": s.ProcessCompare, "memory.entity_candidates": s.ProcessEntityCandidates, "memory.entity_compare": s.ProcessEntityCompare, "memory.handover": s.ProcessHandover, "memory.project_handover": s.ProcessProjectHandover, "memory.topic_project": s.ProcessTopicProject, "memory.date_tidy": s.ProcessDateTidy, "memory.effort": s.ProcessEffort}
	handler, ok := handlers[strings.SplitN(job.Stage, ":", 2)[0]]
	if !ok {
		return errors.New("replay_handler_not_supported")
	}
	var observed error
	wrapped := func(ctx context.Context, job worker.Job) error { observed = handler(ctx, job); return observed }
	queue := &selectedQueue{Store: s, job: &job}
	// The private operation result preserves the handler error. Avoid a second
	// unrestricted log containing copied job identities on the public terminal.
	w := worker.New(queue, map[string]worker.Handler{strings.SplitN(job.Stage, ":", 2)[0]: wrapped}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	_, err := w.RunOnce(ctx)
	return errors.Join(observed, err)
}

// The production worker chooses its next queued run. Refuse competing work
// rather than changing the copy's queue or exposing another model submission.
func processDeputy(ctx context.Context, s *postgres.Store, pool *pgxpool.Pool, owner, runID memory.ID) (workspace.Run, error) {
	var run workspace.Run
	var selected bool
	var pending int
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND id=$2 AND status='queued'),(SELECT count(*) FROM agent_runs WHERE status IN ('queued','running'))`, string(owner), string(runID)).Scan(&selected, &pending); err != nil {
		return run, err
	}
	if !selected || pending != 1 {
		return run, errors.New("replay_deputy_queue_not_exclusive")
	}
	if err := s.RunDeputyOnce(ctx); err != nil {
		return run, err
	}
	var body json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2`, string(owner), string(runID)).Scan(&body); err != nil {
		return run, err
	}
	if err := json.Unmarshal(body, &run); err != nil {
		return run, err
	}
	if run.Status != "done" {
		return run, errors.New("replay_deputy_not_completed")
	}
	return run, nil
}
