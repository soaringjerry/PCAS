package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestStatusDispatchSelfThenAlternatesPastComparePage(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	// A real record anchors all synthetic jobs; only dispatch is under test.
	src := b1Source(t, s, scope, "虚构队列资料", "虚构队列资料。", "manual")
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_jobs"); err != nil {
		t.Fatal(err)
	}
	insert := func(stage string, priority int, ordinal int) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at,created_at)
 VALUES($1,$2,$3,$4,$5,$6,now()-interval '1 minute',now()-interval '1 minute'+$7*interval '1 microsecond')`, memory.NewID(), scope.OwnerID, src.ID, src.Version, stage, priority, ordinal); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 510 {
		insert(fmt.Sprintf("memory.compare:1:%d", i), ComparePriority, i)
	}
	for i, category := range []string{"identity", "taste", "rule", "goal"} {
		insert("memory.card:1:self:"+category, SelfCardPriority, i)
	}
	for i := range 3 {
		insert(fmt.Sprintf("memory.card:1:entity:%d", i), CardPriority, i+1000)
	}
	insert("memory.index", 20, 0)
	index, err := s.ClaimIndex(ctx, time.Minute)
	if err != nil || index == nil || index.Stage != "memory.index" {
		t.Fatal(index, err)
	}
	for _, category := range []string{"identity", "taste", "rule", "goal"} {
		j, err := s.Claim(ctx, time.Minute)
		if err != nil || j == nil || j.Stage != "memory.card:1:self:"+category {
			t.Fatal(j, err, category)
		}
	}
	for i := range 3 {
		j, err := s.Claim(ctx, time.Minute)
		if err != nil || j == nil || j.Stage != fmt.Sprintf("memory.card:1:entity:%d", i) {
			t.Fatal(j, err)
		}
		j, err = s.Claim(ctx, time.Minute)
		if err != nil || j == nil || j.Stage != fmt.Sprintf("memory.compare:1:%d", i) {
			t.Fatal(j, err)
		}
	}
}
