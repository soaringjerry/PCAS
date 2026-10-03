package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type independentIndexQueue struct {
	general chan Job
	index   chan Job
}

func (q *independentIndexQueue) Claim(context.Context, time.Duration) (*Job, error) {
	select {
	case j := <-q.general:
		return &j, nil
	default:
		return nil, nil
	}
}
func (q *independentIndexQueue) ClaimIndex(context.Context, time.Duration) (*Job, error) {
	select {
	case j := <-q.index:
		return &j, nil
	default:
		return nil, nil
	}
}
func (q *independentIndexQueue) Block(context.Context, Job, string) error { return nil }
func (q *independentIndexQueue) Retry(context.Context, Job, string) error { return nil }

func TestIndexLaneProgressesDuringSlowExtraction(t *testing.T) {
	q := &independentIndexQueue{general: make(chan Job, 1), index: make(chan Job, 2)}
	q.general <- Job{Stage: "source.extract"}
	started := make(chan struct{})
	indexed := make(chan string, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w := New(q, map[string]Handler{
		"source.extract":  func(ctx context.Context, _ Job) error { close(started); <-ctx.Done(); return ctx.Err() },
		"source.tokenize": func(_ context.Context, j Job) error { indexed <- j.Stage; return nil },
		"source.embed":    func(_ context.Context, j Job) error { indexed <- j.Stage; return nil },
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("extraction did not start")
	}
	q.index <- Job{Stage: "source.tokenize"}
	q.index <- Job{Stage: "source.embed"}
	for i := 0; i < 2; i++ {
		select {
		case <-indexed:
		case <-ctx.Done():
			t.Fatal("slow extraction starved indexing")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
