package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestDependencyVerificationQueriesDoNotGrowWithMemoryCount(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	source := b1Source(t, s, scope, "Fictitious reference", "Fictitious dependency source.", "manual")
	refs := []memory.Ref{source}
	for i := 0; i < 30; i++ {
		refs = append(refs, b1Claim(t, s, scope, fmt.Sprintf("Fictitious dependency %d.", i), "fact", "adopted", source))
	}
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
	for name, verify := range map[string]func(context.Context, pgx.Tx, memory.Scope, workspace.Run) error{
		"versions": verifyRunTx,
		"access":   verifyRunAccessTx,
	} {
		t.Run(name, func(t *testing.T) {
			count := func(dependencies []memory.Ref) int64 {
				trace.Count.Store(0)
				if err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
					return verify(t.Context(), tx, scope, workspace.Run{AgentID: "model", ContextVersions: dependencies})
				}); err != nil {
					t.Fatal(err)
				}
				return trace.Count.Load()
			}
			few := count(refs[:2])
			many := count(refs)
			if few <= 0 || many != few {
				t.Fatalf("dependency checks: few=%d queries, many=%d queries", few, many)
			}
		})
	}
}

func TestDependencyVerificationRejectsMissingZeroVersion(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, nil)
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious dependency check"})
	for _, kind := range []memory.Kind{memory.SourceKind, memory.ClaimKind} {
		t.Run(string(kind), func(t *testing.T) {
			err := pgx.BeginFunc(t.Context(), s.pool, func(tx pgx.Tx) error {
				return verifyRunTx(t.Context(), tx, scope, workspace.Run{AgentID: "model", ContextVersions: []memory.Ref{{ID: memory.NewID(), Version: 0, Kind: kind}}})
			})
			if !errors.Is(err, memory.ErrConflict) {
				t.Fatalf("missing dependency must fail: %v", err)
			}
		})
	}
}
