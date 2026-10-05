package postgres_test

import (
	"github.com/soaringjerry/PCAS/internal/memory"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPhase25B2_MalformedOrMissingArraysDoNotWriteResults(t *testing.T) {
	for _, wire := range []string{"{invalid JSON", `{"duplicates":[]}`, `{"superseded":[]}`} {
		t.Run(wire, func(t *testing.T) {
			f := phase25B2NewFixture(t)
			_, refs := f.groupTexts(t, "虚构格式检验甲。", "虚构格式检验乙。")
			f.model(t, func(_ *http.Request, _ int, _ phase25B234ModelRequest) phase25B234ModelReply {
				return phase25B234ModelReply{content: wire}
			})
			f.scheduleCompare(t)
			f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.compare:%'`, f.scope.OwnerID)
			j, err := f.store.Claim(f.ctx, time.Minute)
			if err != nil || j == nil {
				t.Fatalf("claim=%+v err=%v", j, err)
			}
			if !strings.HasPrefix(j.Stage, "memory.compare:") {
				t.Fatalf("wrong stage=%s", j.Stage)
			}
			// Handler acknowledgement style is not part of the contract. The
			// observable requirement is no derived results or compared advancement.
			_ = f.store.ProcessCompare(f.ctx, *j)
			for _, r := range refs {
				f.assertRetirement(t, r, "", memory.Ref{})
			}
			var marked int
			if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims WHERE owner_id=$1 AND compared<>0`, f.scope.OwnerID).Scan(&marked); err != nil {
				t.Fatal(err)
			}
			if marked != 0 {
				t.Error("invalid output advanced compared markers")
			}
			f.assertRevisions(t, refs...)
		})
	}
}
func TestPhase25B2_OutsideBatchAndSelfEdgesDiscarded(t *testing.T) {
	f := phase25B2NewFixture(t)
	_, refs := f.groupTexts(t, "虚构越界甲。", "虚构越界乙。")
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		out.Duplicates = []phase25B2WireDuplicate{{Keep: 999, Members: []int{1, 2}}, {Keep: 1, Members: []int{1}}}
		out.Superseded = []phase25B2WireSuperseded{{Old: 1, New: 1}, {Old: 0, New: 2}, {Old: 2, New: 999}, {Old: -1, New: 1}}
		return out
	})
	f.runCompare(t)
	for _, r := range refs {
		f.assertRetirement(t, r, "", memory.Ref{})
	}
	f.assertRevisions(t, refs...)
}
