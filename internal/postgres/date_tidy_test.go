package postgres_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type dateTidyFixture struct {
	*phase25B234Fixture
	refs []memory.Ref
}

// Four dates the home page should not carry as they are, and one that is fine:
// an undated thing still to do, an undated thing only considered, a habit with
// no hour, an appointment long over, and an appointment next week.
func newDateTidyFixture(t *testing.T) *dateTidyFixture {
	f := &dateTidyFixture{phase25B234Fixture: phase25B234NewFixture(t)}
	g, refs := f.groupTexts(t, "虚构：我得去把雨棚的螺丝补齐。", "虚构：我考虑过试一周不看手机。", "虚构：我打算每天晨跑。", "虚构：上个月和木匠约了看料。", "虚构：下周和木匠再约一次。")
	f.card(t, g, refs, false)
	f.refs = refs
	row := func(i int, kind, title, extra string, args ...any) {
		all := append([]any{f.scope.OwnerID, memory.NewID(), refs[i].ID, refs[i].Version, kind, title, "虚构原话 " + title}, args...)
		f.exec(t, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,title,original_text`+extra, all...)
	}
	row(0, "unclear", "补齐雨棚螺丝", `) VALUES($1,$2,$3,$4,$5,$6,$7)`)
	row(1, "unclear", "一周不看手机", `) VALUES($1,$2,$3,$4,$5,$6,$7)`)
	row(2, "recurring", "每天晨跑", `,recurrence,schedule_rule) VALUES($1,$2,$3,$4,$5,$6,$7,'每天','{"clock":"","frequency":"daily","interval":1}')`)
	row(3, "appointment", "和木匠看料", `,at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, time.Now().AddDate(0, 0, -30))
	row(4, "appointment", "和木匠再约", `,at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, time.Now().AddDate(0, 0, 7))
	return f
}

func (f *dateTidyFixture) schedule(t *testing.T) map[string]string {
	t.Helper()
	if _, err := f.store.ScheduleStatus(f.ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Query(f.ctx, `SELECT record_id::text,stage FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.date_tidy:%' AND state='queued'`, f.scope.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, stage string
		if err = rows.Scan(&id, &stage); err != nil {
			t.Fatal(err)
		}
		out[id] = stage
	}
	return out
}

func (f *dateTidyFixture) run(t *testing.T, ref memory.Ref, stage string) error {
	t.Helper()
	j := worker.Job{OwnerID: f.scope.OwnerID, Record: memory.Ref{ID: ref.ID, Version: ref.Version, Kind: memory.ClaimKind}, Stage: stage, LeaseToken: memory.NewID(), Attempts: 1}
	var id string
	if err := f.db.QueryRow(f.ctx, `UPDATE memory_jobs SET state='leased',attempts=1,lease_token=$4,lease_until=now()+interval '5 minutes' WHERE owner_id=$1 AND record_id=$2 AND stage=$3 RETURNING id::text`, f.scope.OwnerID, ref.ID, stage, j.LeaseToken).Scan(&id); err != nil {
		t.Fatal(err)
	}
	j.ID = memory.ID(id)
	return f.store.ProcessDateTidy(f.ctx, j)
}

func (f *dateTidyFixture) open(t *testing.T, ref memory.Ref) bool {
	t.Helper()
	var n int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM deadlines d JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(d.owner_id,d.claim_id,d.claim_version)
 WHERE d.owner_id=$1 AND d.claim_id=$2 AND NOT(coalesce(c.scope->>'deadline_completed','false')='true' AND c.scope->>'deadline_completed_version'=c.version::text)`, f.scope.OwnerID, ref.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func dateTidyReply(decision, title string) phase25B234ModelReply {
	out, _ := json.Marshal(map[string]string{"decision": decision, "title": title, "reason": "虚构依据"})
	return phase25B234ModelReply{content: string(out)}
}

func TestDateTidySortsDatesWithUndoAndAsksOncePerMemoryVersion(t *testing.T) {
	f := newDateTidyFixture(t)
	verdict := map[string][2]string{"补齐雨棚螺丝": {"task", "把雨棚的螺丝补齐"}, "一周不看手机": {"idea", "试一周不看手机"}, "每天晨跑": {"keep", ""}, "和木匠看料": {"drop", ""}}
	model := f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		prompt := phase25B4Prompt(r)
		for title, v := range verdict {
			if strings.Contains(prompt, `"title":"`+title+`"`) {
				return dateTidyReply(v[0], v[1])
			}
		}
		t.Errorf("unexpected date in prompt: %.200s", prompt)
		return phase25B234ModelReply{status: 500}
	})
	jobs := f.schedule(t)
	want := map[string]string{string(f.refs[0].ID): "memory.date_tidy:unclear", string(f.refs[1].ID): "memory.date_tidy:unclear", string(f.refs[2].ID): "memory.date_tidy:habit", string(f.refs[3].ID): "memory.date_tidy:past"}
	if len(jobs) != len(want) {
		t.Fatalf("queued=%v; the appointment next week is not to be judged", jobs)
	}
	for id, stage := range want {
		if jobs[id] != stage {
			t.Fatalf("queued=%v want=%v", jobs, want)
		}
	}
	for i := 0; i < 4; i++ {
		if err := f.run(t, f.refs[i], want[string(f.refs[i].ID)]); err != nil {
			t.Fatal(i, err)
		}
	}
	if len(model.calls()) != 4 {
		t.Fatalf("calls=%d", len(model.calls()))
	}
	if f.open(t, f.refs[0]) || f.open(t, f.refs[1]) || !f.open(t, f.refs[2]) || f.open(t, f.refs[3]) || !f.open(t, f.refs[4]) {
		t.Fatal("wrong dates closed")
	}
	state, err := f.store.Snapshot(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 1 || state.Tasks[0].Title != "把雨棚的螺丝补齐" || len(state.Ideas) != 1 || state.Ideas[0].Title != "试一周不看手机" {
		t.Fatalf("tasks=%+v ideas=%+v", state.Tasks, state.Ideas)
	}
	task := state.Tasks[0]
	if task.Creation == nil || task.Creation.By != "background_tidy" || task.Creation.ActionID == "" || len(task.Creation.MemoryIDs) != 1 || task.Creation.MemoryIDs[0] != string(f.refs[0].ID) || !strings.Contains(task.Notes, "虚构原话") {
		t.Fatalf("task lost what it was made from: %+v notes=%q", task.Creation, task.Notes)
	}
	// One line on the home page for what was put away, each entry undoable; made things have their own receipts.
	var tidied *workspace.Activity
	for i, a := range state.Activity {
		if len(a.Items) > 0 {
			tidied = &state.Activity[i]
		}
	}
	if tidied == nil || len(tidied.Items) != 1 || tidied.Items[0].Text != "和木匠看料" || tidied.Items[0].ActionID == "" {
		t.Fatalf("activity=%+v", state.Activity)
	}
	// The memories themselves are not rewritten.
	f.assertRevisions(t, f.refs...)
	// A second pass asks nothing again: every verdict, keep included, is on record.
	if again := f.schedule(t); len(again) != 0 {
		t.Fatalf("re-queued=%v", again)
	}
	if len(model.calls()) != 4 {
		t.Fatalf("calls after second pass=%d", len(model.calls()))
	}
	// Undo of the to-do takes it away and the date is open again; undo of the drop brings that date back.
	undo := func(action string) {
		t.Helper()
		st, err := f.store.Snapshot(f.ctx, f.scope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.store.Execute(f.ctx, f.scope, workspace.Command{RequestID: string(memory.NewID()), ExpectedRevision: st.Revision, Type: "undoAction", ID: action}); err != nil {
			t.Fatal(err)
		}
	}
	undo(task.Creation.ActionID)
	undo(tidied.Items[0].ActionID)
	state, err = f.store.Snapshot(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 0 || !f.open(t, f.refs[0]) || !f.open(t, f.refs[3]) {
		t.Fatalf("undo left tasks=%d open0=%v open3=%v", len(state.Tasks), f.open(t, f.refs[0]), f.open(t, f.refs[3]))
	}
	for _, a := range state.Activity {
		if len(a.Items) > 0 {
			t.Fatalf("undone drop still reported: %+v", a)
		}
	}
	// What the user took back is not done to them again.
	if again := f.schedule(t); len(again) != 0 {
		t.Fatalf("re-queued after undo=%v", again)
	}
}

// An unreadable verdict changes nothing and is asked again; a date closed by
// the user while the model was thinking is left as the user put it.
func TestDateTidyInvalidOutputAndChangedInputChangeNothing(t *testing.T) {
	f := newDateTidyFixture(t)
	reply := "这不是 JSON"
	f.model(t, func(_ *http.Request, _ int, _ phase25B234ModelRequest) phase25B234ModelReply {
		if reply == "close-first" {
			f.exec(t, `UPDATE claim_revisions SET scope=scope||jsonb_build_object('deadline_completed',true,'deadline_completed_version',version) WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, f.refs[3].ID)
			return dateTidyReply("task", "不该建出来")
		}
		return phase25B234ModelReply{content: reply}
	})
	jobs := f.schedule(t)
	err := f.run(t, f.refs[0], jobs[string(f.refs[0].ID)])
	var jobErr *worker.JobError
	if !errors.As(err, &jobErr) || jobErr.Code != "date_tidy_output_invalid" {
		t.Fatalf("err=%v", err)
	}
	if !f.open(t, f.refs[0]) {
		t.Fatal("invalid output closed the date")
	}
	reply = "close-first"
	if err = f.run(t, f.refs[3], jobs[string(f.refs[3].ID)]); err != nil {
		t.Fatal(err)
	}
	state, err := f.store.Snapshot(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	var checks int
	if err = f.db.QueryRow(f.ctx, `SELECT count(*) FROM date_tidy_checks WHERE owner_id=$1`, f.scope.OwnerID).Scan(&checks); err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 0 || checks != 0 {
		t.Fatalf("tasks=%d checks=%d", len(state.Tasks), checks)
	}
}
