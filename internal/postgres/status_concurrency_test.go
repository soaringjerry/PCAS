package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"

	"sync/atomic"
)

// Pause the result transaction after lease fencing, on a separate worker pool.
// This widens the production interleaving without forging jobs or lock order.
type statusResultBarrier struct {
	reads            atomic.Int32
	entered, release chan struct{}
}

func (b *statusResultBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, q pgx.TraceQueryStartData) context.Context {
	if q.SQL == "SELECT available_at FROM memory_jobs WHERE id=$1" && b.reads.Add(1) == 2 {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
	return ctx
}
func (*statusResultBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Short lock contention is retried; deadlocks are never accepted.

// Keep the result write open longer than the snapshot deadline. It must
// coexist with FOR SHARE, while a command may wait for this short write.
