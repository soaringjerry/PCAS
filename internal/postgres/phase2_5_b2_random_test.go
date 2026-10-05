package postgres_test

import (
	"fmt"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestPhase25B2_RandomActualComparisonSequence(t *testing.T) {
	const seed int64 = 252502
	f := phase25B2NewFixtureTimeout(t, 3*time.Minute)
	rng := rand.New(rand.NewSource(seed))
	t.Logf("seed=%d", seed)
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构随机月报")), Type: "project"}
	keeper := f.claimWith(t, "虚构随机保留项。", "direct", "confirmed", nil)
	f.labels(t, keeper, "progress", true, 1, g)
	current := []memory.Ref{keeper}
	retired := []memory.Ref{}
	texts := map[memory.ID]string{keeper.ID: "虚构随机保留项。"}
	var mu sync.RWMutex
	allowed := map[string]bool{}
	target := ""
	reason := ""
	model := f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		mu.RLock()
		defer mu.RUnlock()
		out := phase25B2Empty()
		for _, m := range in.Memories {
			if !allowed[m.Text] {
				t.Errorf("retired or old version fed to comparison: %s", m.Text)
			}
		}
		if target != "" {
			old, keep := phase25B2N(in, target), phase25B2N(in, "虚构随机保留项。")
			if old == 0 || keep == 0 {
				t.Errorf("planned current items missing: target=%d keep=%d", old, keep)
			}
			if reason == "duplicate" {
				out.Duplicates = []phase25B2WireDuplicate{{Keep: keep, Members: []int{old, keep}}}
			} else {
				out.Superseded = []phase25B2WireSuperseded{{Old: old, New: keep}}
			}
		}
		return out
	})
	refresh := func() {
		mu.Lock()
		defer mu.Unlock()
		allowed = map[string]bool{}
		for _, r := range current {
			allowed[texts[r.ID]] = true
		}
	}
	for step := 0; step < 32; step++ {
		switch rng.Intn(4) {
		case 0:
			text := fmt.Sprintf("虚构随机新增 %d。", step)
			r := f.claim(t, text)
			f.labels(t, r, "progress", true, 1, g)
			current = append(current, r)
			texts[r.ID] = text
		case 1:
			if len(current) > 1 {
				i := 1 + rng.Intn(len(current)-1)
				text := fmt.Sprintf("虚构随机纠正 %d。", step)
				current[i] = f.correct(t, current[i], text)
				texts[current[i].ID] = text
				f.exec(t, `UPDATE claims SET organized=1 WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, current[i].ID)
			}
		case 2:
			if len(current) > 1 {
				i := 1 + rng.Intn(len(current)-1)
				if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{current[i]}}); err != nil {
					t.Fatal(err)
				}
				current = append(current[:i], current[i+1:]...)
			}
		case 3:
			// New organized input makes the actual group eligible. It is unprotected,
			// so manual corrections elsewhere in the oracle cannot invalidate a plan.
			text := fmt.Sprintf("虚构随机比较 %d。", step)
			r := f.claim(t, text)
			f.labels(t, r, "progress", true, 1, g)
			current = append(current, r)
			texts[r.ID] = text
			refresh()
			mu.Lock()
			target = text
			reason = []string{"duplicate", "superseded"}[rng.Intn(2)]
			expectedReason := reason
			mu.Unlock()
			if n := f.runCompare(t); n == 0 {
				t.Error("no actual comparison ran")
			}
			f.assertRetirement(t, r, expectedReason, keeper)
			retired = append(retired, r)
			current = current[:len(current)-1]
			mu.Lock()
			target = ""
			mu.Unlock()
		}
		refresh()
		p, err := f.store.ListMemories(f.ctx, f.scope, workspace.MemoryQuery{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		phase25B234AssertIDs(t, p.Items, current...)
		for _, m := range p.Items {
			if m.Retired != "" || m.RetiredBy != "" {
				t.Error("current view leaked retirement")
			}
			for _, r := range current {
				if m.ID == string(r.ID) && m.Version != r.Version {
					t.Errorf("wrong current version=%d want=%d", m.Version, r.Version)
				}
			}
		}
		p, err = f.store.ListMemories(f.ctx, f.scope, workspace.MemoryQuery{Retired: true, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		phase25B234AssertIDs(t, p.Items, retired...)
		f.assertRevisions(t, append(append([]memory.Ref{}, current...), retired...)...)
		if t.Failed() {
			t.Fatalf("seed=%d step=%d", seed, step)
		}
	}
	if len(model.calls()) < 2 {
		t.Error("random sequence did not exercise actual comparisons")
	}
}
