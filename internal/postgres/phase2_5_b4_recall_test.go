package postgres_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func (f *phase25B234Fixture) index(t *testing.T) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		j, err := f.store.ClaimIndex(f.ctx, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			return
		}
		if err := f.store.ProcessIndex(f.ctx, *j); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("fictional indexing queue did not drain")
}

func (f *phase25B234Fixture) recall(t *testing.T, keyword string) memory.RecallResult {
	t.Helper()
	f.index(t)
	result, err := f.store.Recall(f.ctx, f.scope, memory.RecallRequest{Query: keyword, Mode: memory.Continue, Budget: memory.Budget{Candidates: 40, Edges: 40, Tokens: 8000, Hops: 2}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func phase25B4AssertRecallIDs(t *testing.T, r memory.RecallResult, want ...memory.Ref) {
	t.Helper()
	seen := map[memory.ID]int{}
	for _, ref := range r.Memories {
		if ref.Kind == memory.ClaimKind {
			seen[ref.ID]++
			found := false
			for _, w := range want {
				if ref == w {
					found = true
				}
			}
			if !found {
				t.Errorf("recall exposed unexpected/stale memory %+v", ref)
			}
		}
	}
	for _, w := range want {
		if seen[w.ID] != 1 {
			t.Errorf("recall memory %+v occurs %d times, want once", w, seen[w.ID])
		}
	}
}

// This tests the public fallback retriever, not the as-yet-unspecified model
// protocol. Actual prompt sections and tier calls will use the B4 adapter.
func TestPhase25B4_X4_4_FallbackRecallWithoutStatusLayer(t *testing.T) {
	f := phase25B234NewFixture(t)
	ref := f.claimWith(t, "AcceptanceRecall 虚构陆青准备白鹭月报。", "direct", "confirmed", nil)
	r := f.recall(t, "AcceptanceRecall")
	phase25B4AssertRecallIDs(t, r, ref)
	f.assertRevisions(t, ref)
}

func TestPhase25B4_X4_11_FallbackRecallExcludesRetired(t *testing.T) {
	f := phase25B234NewFixture(t)
	var refs []memory.Ref
	for i := 0; i < 3; i++ {
		refs = append(refs, f.claimWith(t, fmt.Sprintf("AcceptanceRecall 虚构白鹭月报信息 %d。", i), "direct", "confirmed", nil))
	}
	phase25B4AssertRecallIDs(t, f.recall(t, "AcceptanceRecall"), refs...)
	if t.Failed() {
		t.Fatal("positive recall baseline failed")
	}
	f.retire(t, refs[1], refs[0], "duplicate")
	f.retire(t, refs[2], refs[0], "superseded")
	phase25B4AssertRecallIDs(t, f.recall(t, "AcceptanceRecall"), refs[0])
	f.assertRevisions(t, refs...)
}

func TestPhase25B4_ModelUsagePurposesAndTier(t *testing.T) {
	f := phase25B234NewFixture(t)
	if _, err := f.store.Snapshot(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []string{"compare", "card", "handover", "reader", "selfcheck"} {
		for _, tier := range []string{"", "light", "medium", "heavy"} {
			f.exec(t, `INSERT INTO model_usage(owner_id,id,purpose,agent_id,model,input_tokens,output_tokens,cost,tier) VALUES($1,$2,$3,'fictitious-agent','fictitious-model',3,2,0,$4)`, f.scope.OwnerID, memory.NewID(), purpose, tier)
		}
	}
	var n int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM model_usage WHERE owner_id=$1`, f.scope.OwnerID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 20 {
		t.Errorf("usage rows=%d, want 20", n)
	}
	id := memory.NewID()
	f.exec(t, `INSERT INTO model_usage(owner_id,id,purpose,agent_id,model,input_tokens,output_tokens,cost) VALUES($1,$2,'reader','fictitious-agent','fictitious-model',3,2,0)`, f.scope.OwnerID, id)
	var tier string
	if err := f.db.QueryRow(f.ctx, `SELECT tier FROM model_usage WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, id).Scan(&tier); err != nil {
		t.Fatal(err)
	}
	if tier != "" {
		t.Errorf("default tier=%q", tier)
	}
}

func TestPhase25B4_RandomFallbackRecallSequence(t *testing.T) {
	const seed int64 = 252504
	f := phase25B234NewFixture(t)
	rng := rand.New(rand.NewSource(seed))
	t.Logf("seed=%d", seed)
	kept := f.claimWith(t, "AcceptanceRecall 虚构固定保留的事实。", "direct", "confirmed", nil)
	current := []memory.Ref{kept}
	var history []memory.Ref
	phase25B4AssertRecallIDs(t, f.recall(t, "AcceptanceRecall"), kept)
	if t.Failed() {
		t.Fatal("positive recall baseline failed")
	}
	for step := 0; step < 32; step++ {
		switch rng.Intn(3) {
		case 0:
			current = append(current, f.claimWith(t, fmt.Sprintf("AcceptanceRecall 虚构随机上下文 %d。", step), "direct", "confirmed", nil))
		case 1:
			if len(current) > 1 {
				i := 1 + rng.Intn(len(current)-1)
				f.retire(t, current[i], kept, []string{"duplicate", "superseded"}[rng.Intn(2)])
				history = append(history, current[i])
				current = append(current[:i], current[i+1:]...)
			}
		case 2:
			if len(current) > 1 {
				i := 1 + rng.Intn(len(current)-1)
				if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{current[i]}}); err != nil {
					t.Fatal(err)
				}
				current = append(current[:i], current[i+1:]...)
			}
		}
		phase25B4AssertRecallIDs(t, f.recall(t, "AcceptanceRecall"), current...)
		f.assertRevisions(t, append(append([]memory.Ref{}, current...), history...)...)
		if t.Failed() {
			t.Fatalf("seed=%d step=%d", seed, step)
		}
	}
}
