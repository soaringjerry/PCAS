package store

// Frozen before reading implementation, from docs/tasks/phase2_6/README.md
// and docs/research/memory-status-layer-audit.md at origin/phase2_6/main 98443bb.
// This is the acceptance oracle, not a description of current behavior.
//
// T1: exactly 5,000 current memories, 300 nonempty groups, 1,000 entities;
// exactly half Chinese and half English; >=10 groups above 300 members;
// largest group >1,000; English/colloquial Chinese dates and 2-3 letter names.
// Include ungrouped memories, groups with 1/2 members, multi-group memories,
// old/recent duplicates and corrections, scoped/unscoped requirements,
// expired/future/recurring/unclear deadlines, and handover source categories.
// Expected content is authored with the fixture, never derived from model output.
//
// T2/G1: every cap has a documented reason and batching or visible overflow.
// G2/A5/B5: failures preserve good data and pending work; reason/count/backoff.
// G3/A1: successful model output survives failed writes; no repeated model call.
// G4: only affected material is recomputed after one memory changes.
// G5/A7/C3: semantic decisions use the model, including uncertain results.
// G6/B8: reads produce no database mutations, including stale content reads.
// G7/A3: stages do not borrow budget; classification has first priority.
// A2: stage success/defer/failure and last reason visible; >30m defer warns.
// A4: 234 synthetic legacy markers migrate losslessly; done-job cleanup is safe.
// A6: all batches compared, including early/recent pair and memory x group.
// A7: rejection survives memory-count changes; rename alone permits recheck.
// A8: >1,000-link merge completes; rejection takes no card locks.
// A9: every channel records actual/estimated tokens and enforces daily cost.
// A10: unchanged scheduling does not rewrite rows.
// A11: extraction above 30 memories accepts every batch.
// A12: scale scheduling finishes; alias queue drains; quota test never skips.
// B1/C8: no card builds/writes/reads; old card rows remain unchanged.
// B2/T3: classification also extracts every planted deadline, no extra call;
// backfill covers old/ungrouped/small-group memories using classification budget.
// B3: expired/unclear dates retain provenance and appropriate labels.
// B4/C2: deduplicate all requirements via comparison; unrestricted always sent;
// scoped ranked separately by model; documented overflow count and priority.
// B5/C1: handover uses identity/goals/tastes repeated or recent, all requirements,
// deadlines; changes only; >=1h between rewrites; old text/time remains available.
// B6: new domains allowed, synonymous domains do not multiply.
// B7: all group consumers use the same membership rule.
// B9: small legacy cards explained; remaining shared cause fixed if necessary.
// C3: group/depth judgment piggybacks an existing call; no substring matching.
// C4: heavy reads bounded/batched; unfinished groups disclosed to user.
// C5: unrelated background change preserves response; changed action target retries.
// C6: all overflow observable; recurring arrangements not displaced by dated rows.
// C7: mention/adoption signals recorded and affect ranking (on/off comparison).
// U1-U3: one library entry, current/stale/empty text truthful, group search/filter,
// legacy deep links redirect, public wording uses "已被替代或合并".
// U4: unrelated background writes neither reload whole page nor reject user edits.
// T2 concurrency: scheduler + worker + user request overlap for EACH stage.
// T4: record request latency/calls, per-memory calls, hourly calls by stage.
// Section 5: all T checks must pass on the integration branch; intermittent
// failures require a diagnosed cause. Deployment/live verification/V/docs are
// coordinator-owned and cannot be certified by this isolated synthetic suite.

import "testing"

var phase26HourlyBudget = map[string]int{
	"classification": 40,
	"comparison":     40,
	"alias_confirm":  30,
	"alias_scan":     6,
	"handover":       2,
}

func TestPhase26FrozenBudgetOracle(t *testing.T) {
	total := 0
	for _, budget := range phase26HourlyBudget {
		total += budget
	}
	// The contract reserves 118 of the 120 hourly calls. The two unassigned
	// calls are not permission to lend capacity across the five stages.
	if total != 118 || total > 120 {
		t.Fatalf("invalid frozen stage budgets: %d", total)
	}
}
