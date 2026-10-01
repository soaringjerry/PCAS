// Package worker coordinates durable memory jobs. Model and storage adapters are
// supplied by the composition root; business modules never start private queues.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

var ErrLeaseLost = errors.New("job lease lost")

// JobError lets a handler record a fixed failure category instead of the
// generic ones below. Code is stored and logged, so it must never contain
// source text, provider responses or credentials. Retry asks for the queue's
// bounded backoff; otherwise the job waits for an explicit retry.
type JobError struct {
	Code  string
	Retry bool
}

func (e *JobError) Error() string { return e.Code }

type Job struct {
	ID         memory.ID
	OwnerID    memory.ID
	Record     memory.Ref
	Stage      string
	Attempts   int
	LeaseToken memory.ID
}

type Queue interface {
	Claim(context.Context, time.Duration) (*Job, error)
	Block(context.Context, Job, string) error
	Retry(context.Context, Job, string) error
}

// A handler commits its output and job acknowledgement in the same fenced
// transaction. It must be idempotent and honor context cancellation.
type Handler func(context.Context, Job) error

type Worker struct {
	queue    Queue
	handlers map[string]Handler
	logger   *slog.Logger
}

func New(queue Queue, handlers map[string]Handler, logger *slog.Logger) *Worker {
	return &Worker{queue: queue, handlers: handlers, logger: logger}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	job, err := w.queue.Claim(ctx, 5*time.Minute)
	if err != nil || job == nil {
		return false, err
	}
	handler, ok := w.handlers[strings.SplitN(job.Stage, ":", 2)[0]]
	if !ok {
		return true, ignoreLostLease(w.queue.Block(ctx, *job, "handler_not_configured"))
	}
	workCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	err = handler(workCtx, *job)
	cancel()
	if err == nil || errors.Is(err, ErrLeaseLost) {
		return true, nil
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	} // lease recovery handles shutdown
	code, retry := "processing_failed", true
	var failure *JobError
	if errors.As(err, &failure) {
		code, retry = failure.Code, failure.Retry
	} else if errors.Is(err, memory.ErrUnavailable) {
		code, retry = "provider_not_configured", false
	}
	w.logger.Warn("memory job failed", "job_id", job.ID, "stage", job.Stage, "attempt", job.Attempts, "error_type", code)
	if retry {
		return true, ignoreLostLease(w.queue.Retry(ctx, *job, code))
	}
	return true, ignoreLostLease(w.queue.Block(ctx, *job, code))
}

func ignoreLostLease(err error) error {
	if errors.Is(err, ErrLeaseLost) {
		return nil
	}
	return err
}

func (w *Worker) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		worked, err := w.RunOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
