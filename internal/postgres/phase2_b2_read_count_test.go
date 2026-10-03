package postgres

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type b2QueryTrace struct{ Count atomic.Int64 }

func (x *b2QueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	x.Count.Add(1)
	return ctx
}
func (x *b2QueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestPhase2B2_M1_ReadBatchQueryCountDoesNotGrowWithRows(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	g := b2Library(t, s, scope, 130)
	// Reopen this test's own schema with pgx's query tracer. The owner, database,
	// handlers, HTTP API and queries are unchanged; no fake storage is involved.
	config := s.pool.Config()
	trace := &b2QueryTrace{}
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	original := s.pool
	s.pool = pool
	t.Cleanup(func() { s.pool = original; pool.Close() })
	// Warm owner setup and lazy configuration before either counted read.
	b2List(t, s, scope, "?limit=1")
	trace.Count.Store(0)
	small := b2List(t, s, scope, "?limit=1")
	one := trace.Count.Load()
	trace.Count.Store(0)
	many := b2List(t, s, scope, "?limit=100")
	hundred := trace.Count.Load()
	b2Equal(t, len(small.Items), 1)
	b2Equal(t, len(many.Items), 100)
	if one <= 0 {
		t.Fatal("query tracer saw no database requests")
	}
	b2Equal(t, hundred, one)
	for _, m := range many.Items {
		b2Event(t, m, &g.From, &g.To, "range")
		b2Mentions(t, m)
	}
}
