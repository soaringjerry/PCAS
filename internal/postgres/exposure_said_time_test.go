package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// History filed today was said long ago: its exposure starts from then.
func TestExposureStartsFromWhenItWasSaid(t *testing.T) {
	s := b1EmptyStore(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	b1Model(t, s, `{}`)
	scope := owner()
	said := time.Now().Add(-90 * 24 * time.Hour).UTC().Truncate(time.Second)
	source, err := s.Ingest(ctx, scope, memory.IngestRequest{Connector: "manual", ExternalID: "old-note", ExternalVersion: "1", Title: "虚构旧笔记", Text: "云杉每周六早上去虚构湖边跑步。", MediaType: "text/plain", ExpressedAt: &said})
	if err != nil {
		t.Fatal(err)
	}
	var old, fresh memory.Ref
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		old, err = s.rememberTx(ctx, tx, scope, statement{Text: "云杉每周六早上去虚构湖边跑步。", Nature: "fact", Actor: "ai", Confirmation: "adopted", Acquisition: "direct", Source: source.Ref})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := b1Source(t, s, scope, "虚构新笔记", "霜叶今天把盆栽搬到了窗边。", "manual")
	fresh = b1Claim(t, s, scope, "霜叶今天把盆栽搬到了窗边。", "fact", "adopted", now)
	var oldAt, freshAt time.Time
	if err := s.pool.QueryRow(ctx, "SELECT last_effective_use_at FROM activity WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(old.ID)).Scan(&oldAt); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT last_effective_use_at FROM activity WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(fresh.ID)).Scan(&freshAt); err != nil {
		t.Fatal(err)
	}
	if !oldAt.Equal(said) {
		t.Fatalf("old memory last used at %v, want when it was said %v", oldAt, said)
	}
	if time.Since(freshAt) > time.Minute {
		t.Fatalf("a memory with no said time must start from now, got %v", freshAt)
	}
	m, err := s.GetMemory(ctx, scope, string(old.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Default half-life is thirty days: ninety days leaves about one eighth.
	if m.Exposure < 0.10 || m.Exposure > 0.15 {
		t.Fatalf("exposure after ninety days = %v, want about 0.125", m.Exposure)
	}
}
