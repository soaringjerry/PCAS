package postgres_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Input adaptation is isolated here. Only the local fake model's requests are
// observed; no backend implementation or backend tests are consulted.
type phase25B2InputMemory struct {
	N           int    `json:"n"`
	Text        string `json:"text"`
	ExpressedAt string `json:"expressedAt"`
	Protected   bool   `json:"protected"`
}
type phase25B2Input struct {
	Memories []phase25B2InputMemory `json:"memories"`
}
type phase25B2WireDuplicate struct {
	Keep    int   `json:"keep"`
	Members []int `json:"members"`
}
type phase25B2WireSuperseded struct {
	Old int `json:"old"`
	New int `json:"new"`
}
type phase25B2WireOutput struct {
	Duplicates []phase25B2WireDuplicate  `json:"duplicates"`
	Superseded []phase25B2WireSuperseded `json:"superseded"`
}

func phase25B2Empty() phase25B2WireOutput {
	return phase25B2WireOutput{Duplicates: []phase25B2WireDuplicate{}, Superseded: []phase25B2WireSuperseded{}}
}
func phase25B2InputFrom(r phase25B234ModelRequest) (phase25B2Input, error) {
	text, err := phase25B3Prompt(r)
	if err != nil {
		return phase25B2Input{}, err
	}
	var in phase25B2Input
	err = json.Unmarshal([]byte(text), &in)
	if err == nil && len(in.Memories) == 0 {
		err = fmt.Errorf("missing numbered comparison memories: %s", text)
	}
	return in, err
}
func phase25B2N(in phase25B2Input, text string) int {
	for _, m := range in.Memories {
		if m.Text == text {
			return m.N
		}
	}
	return 0
}
func phase25B2JSON(out phase25B2WireOutput) phase25B234ModelReply {
	if out.Duplicates == nil {
		out.Duplicates = []phase25B2WireDuplicate{}
	}
	if out.Superseded == nil {
		out.Superseded = []phase25B2WireSuperseded{}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return phase25B234ModelReply{status: 500}
	}
	return phase25B234ModelReply{content: string(data)}
}
func (f *phase25B234Fixture) compareModel(t *testing.T, fn func(phase25B2Input) phase25B2WireOutput) *phase25B234Model {
	t.Helper()
	return f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		in, err := phase25B2InputFrom(r)
		if err != nil { // Unrelated entity candidates must never merge accidentally.
			t.Logf("non-comparison local input: %v", err)
			return phase25B234ModelReply{content: `{"same":false,"keep":null}`}
		}
		for i, m := range in.Memories {
			if m.N != i+1 {
				t.Errorf("comparison n=%d at position=%d", m.N, i)
			}
		}
		if fn == nil {
			return phase25B2JSON(phase25B2Empty())
		}
		return phase25B2JSON(fn(in))
	})
}
func (f *phase25B234Fixture) scheduleCompare(t *testing.T) int {
	t.Helper()
	// Age only fixture inputs. Organizing is a precondition supplied by labels;
	// unrelated ingestion/card jobs are excluded from this isolated queue test.
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'memory.compare:%' AND stage NOT LIKE 'memory.entity_compare:%'`, f.scope.OwnerID)
	n, err := f.store.ScheduleCompare(f.ctx, time.Now().Add(11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func (f *phase25B234Fixture) compareJob(t *testing.T) string {
	t.Helper()
	f.exec(t, `UPDATE memory_jobs SET available_at=least(available_at,now()) WHERE owner_id=$1 AND state='queued' AND error_code='' AND (stage LIKE 'memory.compare:%' OR stage LIKE 'memory.entity_compare:%')`, f.scope.OwnerID)
	j, err := f.store.Claim(f.ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		return ""
	}
	var priority int
	if err := f.db.QueryRow(f.ctx, `SELECT priority FROM memory_jobs WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, j.ID).Scan(&priority); err != nil {
		t.Fatal(err)
	}
	if priority != 8 {
		t.Errorf("compare priority=%d, want 8", priority)
	}
	if strings.HasPrefix(j.Stage, "memory.compare:") {
		err = f.store.ProcessCompare(f.ctx, *j)
	} else if strings.HasPrefix(j.Stage, "memory.entity_compare:") {
		err = f.store.ProcessEntityCompare(f.ctx, *j)
	} else {
		t.Fatalf("unexpected claimed stage=%s", j.Stage)
	}
	if err != nil {
		t.Fatalf("process %s: %v", j.Stage, err)
	}
	return j.Stage
}
func (f *phase25B234Fixture) runCompare(t *testing.T) int {
	t.Helper()
	f.scheduleCompare(t)
	n := 0
	for i := 0; i < 70; i++ {
		stage := f.compareJob(t)
		if stage == "" {
			return n
		}
		if strings.HasPrefix(stage, "memory.compare:") {
			n++
		}
		f.scheduleCompare(t)
	}
	t.Fatal("comparison queue failed to drain within allowance")
	return n
}
func (f *phase25B234Fixture) assertRetirement(t *testing.T, ref memory.Ref, reason string, by memory.Ref) {
	t.Helper()
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if m.Retired != reason || (reason != "" && m.RetiredBy != string(by.ID)) {
		t.Errorf("retirement=%q by=%q want=%q/%s", m.Retired, m.RetiredBy, reason, by.ID)
	}
}
func TestPhase25B2_X2_1_ActualDuplicateEvidenceAndHistory(t *testing.T) {
	f := phase25B2NewFixture(t)
	texts := []string{"虚构月报周五提交。", "虚构月报在周五提交。", "虚构月报周五十点前提交。"}
	_, refs := f.groupTexts(t, texts...)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		out.Duplicates = []phase25B2WireDuplicate{{Keep: phase25B2N(in, texts[2]), Members: []int{phase25B2N(in, texts[0]), phase25B2N(in, texts[1]), phase25B2N(in, texts[2])}}}
		return out
	})
	if n := f.runCompare(t); n != 1 {
		t.Errorf("group calls=%d", n)
	}
	for _, r := range refs[:2] {
		f.assertRetirement(t, r, "duplicate", refs[2])
	}
	f.assertRetirement(t, refs[2], "", memory.Ref{})
	m, err := f.store.GetMemory(f.ctx, f.scope, string(refs[2].ID))
	if err != nil {
		t.Fatal(err)
	}
	if m.MergedFrom != 2 || m.Trust != "repeated" {
		t.Errorf("mergedFrom/trust=%d/%s", m.MergedFrom, m.Trust)
	}
	var sources, compared int
	if err := f.db.QueryRow(f.ctx, `SELECT count(DISTINCT source_id) FROM evidence WHERE owner_id=$1 AND target_id=$2`, f.scope.OwnerID, refs[2].ID).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if sources != 3 {
		t.Errorf("survivor independent sources=%d want 3", sources)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND compared=$3`, f.scope.OwnerID, []string{string(refs[0].ID), string(refs[1].ID), string(refs[2].ID)}, postgres.CompareVersion).Scan(&compared); err != nil {
		t.Fatal(err)
	}
	if compared != 3 {
		t.Errorf("atomic compared markers=%d", compared)
	}
	var p workspace.MemoryPage
	f.get(t, "/v1/workspace/memories", &p)
	phase25B234AssertIDs(t, p.Items, refs[2])
	f.get(t, "/v1/workspace/memories?retired=1", &p)
	phase25B234AssertIDs(t, p.Items, refs[:2]...)
	f.assertRevisions(t, refs...)
}
func TestPhase25B2_X2_2_ActualSupersessionAndRecall(t *testing.T) {
	f := phase25B2NewFixture(t)
	texts := []string{"虚构白鹭月报期限是周三。", "虚构白鹭月报期限改到周五。"}
	_, refs := f.groupTexts(t, texts...)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, texts[0]), New: phase25B2N(in, texts[1])}}
		return out
	})
	f.runCompare(t)
	f.assertRetirement(t, refs[0], "superseded", refs[1])
	f.assertRetirement(t, refs[1], "", memory.Ref{})
	p, err := f.store.ListMemories(f.ctx, f.scope, workspace.MemoryQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	phase25B234AssertIDs(t, p.Items, refs[1])
	phase25B4AssertRecallIDs(t, f.recall(t, "白鹭月报"), refs[1])
	f.assertRevisions(t, refs...)
}

func TestPhase25B2_X2_5_ConfirmedAndManuallyCorrectedProtected(t *testing.T) {
	f := phase25B2NewFixture(t)
	g, base := f.groupTexts(t, "虚构替代新说法。")
	confirmed := f.claimWith(t, "虚构确认过的原话。", "direct", "confirmed", nil)
	f.labels(t, confirmed, "progress", true, 1, g)
	manual := f.claim(t, "虚构待纠正的原话。")
	f.labels(t, manual, "progress", true, 1, g)
	manual = f.correct(t, manual, "虚构用户亲手纠正的原话。")
	f.exec(t, `UPDATE claims SET organized=1 WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, manual.ID)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		for _, m := range in.Memories {
			if strings.Contains(m.Text, "原话") && !m.Protected {
				t.Errorf("protected source not marked: %+v", m)
			}
		}
		out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, "虚构确认过的原话。"), New: phase25B2N(in, "虚构替代新说法。")}, {Old: phase25B2N(in, "虚构用户亲手纠正的原话。"), New: phase25B2N(in, "虚构替代新说法。")}}
		return out
	})
	f.runCompare(t)
	f.assertRetirement(t, confirmed, "", memory.Ref{})
	f.assertRetirement(t, manual, "", memory.Ref{})
	f.assertRevisions(t, append(base, confirmed, manual)...)
}
func TestPhase25B2_X2_6_CyclesDiscardedAndChainsPointToFinal(t *testing.T) {
	for _, cycle := range []bool{true, false} {
		t.Run(fmt.Sprintf("cycle=%v", cycle), func(t *testing.T) {
			f := phase25B2NewFixture(t)
			texts := []string{"虚构链条甲。", "虚构链条乙。", "虚构链条丙。"}
			_, refs := f.groupTexts(t, texts...)
			f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
				out := phase25B2Empty()
				a, b, c := phase25B2N(in, texts[0]), phase25B2N(in, texts[1]), phase25B2N(in, texts[2])
				out.Superseded = []phase25B2WireSuperseded{{Old: a, New: b}}
				if cycle {
					out.Superseded = append(out.Superseded, phase25B2WireSuperseded{Old: b, New: a})
				} else {
					out.Superseded = append(out.Superseded, phase25B2WireSuperseded{Old: b, New: c})
				}
				return out
			})
			f.runCompare(t)
			if cycle {
				for _, r := range refs {
					f.assertRetirement(t, r, "", memory.Ref{})
				}
			} else {
				for _, r := range refs[:2] {
					f.assertRetirement(t, r, "superseded", refs[2])
				}
				f.assertRetirement(t, refs[2], "", memory.Ref{})
			}
			f.assertRevisions(t, refs...)
		})
	}
}
func TestPhase25B2_X2_7_ConcurrentChangeDiscardsOnlyRelatedResult(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		t.Run(fmt.Sprintf("delete=%v", deletion), func(t *testing.T) {
			f := phase25B2NewFixture(t)
			texts := []string{"虚构竞态旧甲。", "虚构竞态新甲。", "虚构竞态旧乙。", "虚构竞态新乙。"}
			_, refs := f.groupTexts(t, texts...)
			entered, release := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
				close(entered)
				<-release
				out := phase25B2Empty()
				out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, texts[0]), New: phase25B2N(in, texts[1])}, {Old: phase25B2N(in, texts[2]), New: phase25B2N(in, texts[3])}}
				return out
			})
			f.scheduleCompare(t)
			f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.compare:%'`, f.scope.OwnerID)
			j, err := f.store.Claim(f.ctx, time.Minute)
			if err != nil || j == nil {
				t.Fatalf("claim=%+v err=%v", j, err)
			}
			if !strings.HasPrefix(j.Stage, "memory.compare:") {
				t.Fatalf("unexpected stage=%s", j.Stage)
			}
			done := make(chan error, 1)
			go func() { done <- f.store.ProcessCompare(f.ctx, *j) }()
			select {
			case <-entered:
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			if deletion {
				if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{refs[0]}}); err != nil {
					t.Fatal(err)
				}
			} else {
				refs[0] = f.correct(t, refs[0], "虚构竞态纠正后的甲。")
			}
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			if !deletion {
				f.assertRetirement(t, refs[0], "", memory.Ref{})
			}
			f.assertRetirement(t, refs[2], "superseded", refs[3])
			f.assertRevisions(t, refs[1:]...)
			if !deletion {
				f.assertRevisions(t, refs[0])
			}
		})
	}
}
func TestPhase25B2_X2_13_VersionUpgradeCurrentOnly(t *testing.T) {
	f := phase25B2NewFixture(t)
	texts := []string{"虚构规则升级旧说法。", "虚构规则升级新说法。"}
	_, refs := f.groupTexts(t, texts...)
	model := f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		if phase25B2N(in, texts[0]) != 0 {
			out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, texts[0]), New: phase25B2N(in, texts[1])}}
		}
		return out
	})
	f.runCompare(t)
	first := len(model.calls())
	old := postgres.CompareVersion
	// CompareVersion is a public constant. Seed the previous generation marker
	// to reproduce the persisted state seen by a binary whose rule increased.
	f.exec(t, `UPDATE claims SET compared=$2 WHERE owner_id=$1 AND retired=''`, f.scope.OwnerID, old-1)
	f.scheduleCompare(t)
	f.assertRetirement(t, refs[0], "superseded", refs[1])
	f.runCompare(t)
	if len(model.calls()) <= first {
		t.Error("upgrade did not recompare")
	}
	for _, r := range model.calls()[first:] {
		in, err := phase25B2InputFrom(r)
		if err != nil {
			continue
		}
		for _, m := range in.Memories {
			if m.Text == texts[0] {
				t.Error("retired source fed after version upgrade")
			}
		}
	}
	var version int
	if err := f.db.QueryRow(f.ctx, `SELECT compared FROM claims WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, refs[1].ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != old {
		t.Errorf("compared=%d", version)
	}
	f.assertRevisions(t, refs...)
}
func TestPhase25B2_NoModelSchedulesNothing(t *testing.T) {
	f := phase25B2NewFixture(t)
	_, refs := f.groupTexts(t, "虚构不配模型甲。", "虚构不配模型乙。")
	if n := f.scheduleCompare(t); n != 0 {
		t.Errorf("without model queued=%d", n)
	}
	f.assertRevisions(t, refs...)
}
