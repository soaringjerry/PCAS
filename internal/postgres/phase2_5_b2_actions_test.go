package postgres_test

import (
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"testing"
)

// Command spelling is not frozen in the contract. This adapter is the only
// location to bind the public action boundary; successful actions must be
// recorded and undone through Store.Undo, never by restoring database columns.
func (f *phase25B234Fixture) compareAction(t *testing.T, types []string, id string) workspace.State {
	t.Helper()
	state, err := f.store.Snapshot(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range types {
		next, e := f.store.Execute(f.ctx, f.scope, workspace.Command{Type: typ, ID: id, MemoryID: id, TargetID: id, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
		if e == nil {
			t.Logf("public action binding=%s", typ)
			return next
		}
		t.Logf("action %s: %v", typ, e)
	}
	t.Fatal("no public action binding succeeded")
	return workspace.State{}
}
func TestPhase25B2_X2_8_RestoreExemptionAndActionUndo(t *testing.T) {
	f := phase25B2NewFixture(t)
	texts := []string{"虚构恢复旧期限。", "虚构恢复新期限。"}
	g, refs := f.groupTexts(t, texts...)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, texts[0]), New: phase25B2N(in, texts[1])}}
		return out
	})
	f.runCompare(t)
	f.assertRetirement(t, refs[0], "superseded", refs[1])
	var before int64
	if err := f.db.QueryRow(f.ctx, `SELECT coalesce(max(action_order),0) FROM action_log WHERE owner_id=$1`, f.scope.OwnerID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	f.compareAction(t, []string{"restoreMemory"}, string(refs[0].ID))
	f.assertRetirement(t, refs[0], "", memory.Ref{})
	var action string
	if err := f.db.QueryRow(f.ctx, `SELECT id FROM action_log WHERE owner_id=$1 AND action_order>$2 AND undone_at IS NULL ORDER BY action_order DESC LIMIT 1`, f.scope.OwnerID, before).Scan(&action); err != nil {
		t.Fatalf("restore action-log entry: %v", err)
	}
	// A newly organized memory makes the group eligible again, without
	// touching the restored memory or its exemption metadata.
	extra := f.claim(t, "虚构恢复后补充：白鹭月报使用青色图表。")
	f.labels(t, extra, "progress", true, 1, g)
	f.runCompare(t)
	f.assertRetirement(t, refs[0], "", memory.Ref{})
	if _, err := f.store.Undo(f.ctx, f.scope, action); err != nil {
		t.Fatal(err)
	}
	f.assertRetirement(t, refs[0], "superseded", refs[1])
	f.assertRevisions(t, refs...)
}

func TestPhase25B2_X2_8_RestoreUndoWithoutInterveningComparison(t *testing.T) {
	f := phase25B2NewFixture(t)
	old := f.claim(t, "虚构撤销恢复旧便签。")
	next := f.claim(t, "虚构撤销恢复新便签。")
	f.retire(t, old, next, "superseded")
	f.compareAction(t, []string{"restoreMemory"}, string(old.ID))
	f.assertRetirement(t, old, "", memory.Ref{})
	var action string
	if err := f.db.QueryRow(f.ctx, `SELECT id FROM action_log WHERE owner_id=$1 ORDER BY action_order DESC LIMIT 1`, f.scope.OwnerID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Undo(f.ctx, f.scope, action); err != nil {
		t.Fatal(err)
	}
	f.assertRetirement(t, old, "superseded", next)
	f.assertRevisions(t, old, next)
}
