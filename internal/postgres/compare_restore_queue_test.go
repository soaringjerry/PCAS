package postgres_test

import (
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Regression for F-B2-6: restoring is a comparison decision, so its exemption
// must survive removal of unrelated extraction/organizing jobs. Use public
// actions and the independent acceptance fixture's comparison queue boundary.
func TestCompareRestoreSurvivesUnrelatedQueueCleanup(t *testing.T) {
	f := phase25B2NewFixture(t)
	oldText, newText := "虚构白鹭截止日期周三。", "虚构白鹭截止日期改为周五。"
	g, refs := f.groupTexts(t, oldText, newText)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, oldText), New: phase25B2N(in, newText)}}
		return out
	})
	f.runCompare(t)
	f.assertRetirement(t, refs[0], "superseded", refs[1])
	f.compareAction(t, []string{"restoreMemory"}, string(refs[0].ID))
	var action string
	if err := f.db.QueryRow(f.ctx, `SELECT id FROM action_log WHERE owner_id=$1 ORDER BY action_order DESC LIMIT 1`, f.scope.OwnerID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	extra := f.claim(t, "虚构白鹭图表使用青色。")
	f.labels(t, extra, "progress", true, 1, g)
	f.runCompare(t)
	f.assertRetirement(t, refs[0], "", memory.Ref{})
	if _, err := f.store.Undo(f.ctx, f.scope, action); err != nil {
		t.Fatal(err)
	}
	f.assertRetirement(t, refs[0], "superseded", refs[1])
	f.assertRevisions(t, refs...)
}
