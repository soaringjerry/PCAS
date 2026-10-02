package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type deferRecordingQueue struct {
	recordingQueue
	code      string
	until     time.Time
	noAttempt bool
}

func (q *deferRecordingQueue) Defer(_ context.Context, _ Job, code string, until time.Time, noAttempt bool) error {
	q.code, q.until, q.noAttempt = code, until, noAttempt
	return nil
}
func TestBudgetDeferralUsesQueueWithoutFailure(t *testing.T) {
	until := time.Now().Add(time.Hour)
	q := &deferRecordingQueue{recordingQueue: recordingQueue{job: &Job{Stage: "source.extract", Attempts: 5}}}
	w := New(q, map[string]Handler{"source.extract": func(context.Context, Job) error {
		return &JobError{Code: "budget_deferred", Until: until, NoAttempt: true}
	}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worked, err := w.RunOnce(context.Background())
	if err != nil || !worked || q.code != "budget_deferred" || !q.until.Equal(until) || !q.noAttempt || q.blocked != "" || q.retried != "" {
		t.Fatalf("worked=%v err=%v queue=%+v", worked, err, q)
	}
}
