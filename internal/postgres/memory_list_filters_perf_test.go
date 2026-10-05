package postgres

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Explicit opt-in, the same fictional 10,000-memory / 30,000-source fixture as
// P2. The default list must not pay for the new optional filters.
func TestMemoryListFiltersPerformance(t *testing.T) {
	if os.Getenv("PCAS_LIST_FILTER_PERF") == "" {
		t.Skip("set PCAS_LIST_FILTER_PERF for synthetic list timings")
	}
	s, scope := p2Fixture(t, 10000)
	for _, q := range []workspace.MemoryQuery{{Limit: 50}, {Trust: "stated", Limit: 50}, {Trust: "repeated", Limit: 50}, {Trust: "tentative", Limit: 50}, {Trust: "reported", Limit: 50}, {Trust: "inferred", Limit: 50}} {
		durations := []time.Duration{}
		for i := 0; i < 4; i++ {
			start := time.Now()
			page, err := s.ListMemories(context.Background(), scope, q)
			elapsed := time.Since(start)
			wantTotal, wantItems := 10000, 50
			if q.Trust != "" && q.Trust != "stated" {
				wantTotal, wantItems = 0, 0
			}
			if err != nil || page.Total != wantTotal || len(page.Items) != wantItems {
				t.Fatal(page.Total, len(page.Items), err)
			}
			if i > 0 {
				durations = append(durations, elapsed)
			}
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		t.Logf("list 10000 memories trust=%q median=%s samples=%v", q.Trust, durations[1], durations)
	}
	var keeper string
	if err := s.pool.QueryRow(context.Background(), "SELECT id::text FROM memory_records WHERE owner_id=$1 AND kind='claim' ORDER BY updated_at DESC,id LIMIT 1", string(scope.OwnerID)).Scan(&keeper); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), `UPDATE claims SET retired='duplicate',retired_by=$2 WHERE owner_id=$1 AND id IN(
 SELECT id FROM memory_records WHERE owner_id=$1 AND kind='claim' AND id<>$2 ORDER BY updated_at DESC,id LIMIT 20)`, string(scope.OwnerID), keeper); err != nil {
		t.Fatal(err)
	}
	for _, q := range []workspace.MemoryQuery{{Retired: true, Limit: 50}, {Retired: true, RetiredBy: keeper, Limit: 50}, {Retired: true, RetiredBy: keeper, Trust: "stated", Limit: 50}} {
		durations := []time.Duration{}
		for i := 0; i < 4; i++ {
			start := time.Now()
			page, err := s.ListMemories(context.Background(), scope, q)
			elapsed := time.Since(start)
			if err != nil || page.Total != 20 || len(page.Items) != 20 {
				t.Fatal(page.Total, len(page.Items), err)
			}
			if i > 0 {
				durations = append(durations, elapsed)
			}
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		t.Logf("retired list 10000 memories retiredBy=%v trust=%q median=%s samples=%v", q.RetiredBy != "", q.Trust, durations[1], durations)
	}

}
