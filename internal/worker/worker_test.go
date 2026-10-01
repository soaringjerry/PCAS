package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type recordingQueue struct {
	job     *Job
	blocked string
	retried string
}

func (q *recordingQueue) Claim(context.Context, time.Duration) (*Job, error) {
	job := q.job
	q.job = nil
	return job, nil
}
func (q *recordingQueue) Block(_ context.Context, _ Job, code string) error {
	q.blocked = code
	return nil
}
func (q *recordingQueue) Retry(_ context.Context, _ Job, code string) error {
	q.retried = code
	return nil
}

func TestFailureCategoryDecidesRetryOrBlock(t *testing.T) {
	for _, tc := range []struct {
		name             string
		err              error
		blocked, retried string
	}{
		{name: "named, retryable", err: &JobError{Code: "model_output_invalid", Retry: true}, retried: "model_output_invalid"},
		{name: "named, waits for the user", err: &JobError{Code: "model_call_failed"}, blocked: "model_call_failed"},
		{name: "named and wrapped", err: errors.Join(errors.New("context"), &JobError{Code: "model_call_failed", Retry: true}), retried: "model_call_failed"},
		{name: "no provider", err: memory.ErrUnavailable, blocked: "provider_not_configured"},
		{name: "anything else", err: errors.New("database hiccup"), retried: "processing_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queue := &recordingQueue{job: &Job{ID: memory.NewID(), Stage: "source.extract", Attempts: 1}}
			w := New(queue, map[string]Handler{"source.extract": func(context.Context, Job) error { return tc.err }}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			worked, err := w.RunOnce(context.Background())
			if !worked || err != nil {
				t.Fatal(worked, err)
			}
			if queue.blocked != tc.blocked || queue.retried != tc.retried {
				t.Fatalf("blocked=%q retried=%q", queue.blocked, queue.retried)
			}
		})
	}
}
