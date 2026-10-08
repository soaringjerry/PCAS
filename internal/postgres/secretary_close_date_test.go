package postgres_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The secretary closes a date by one sentence: the row leaves the list it is
// shown and the home schedule, the memory itself is untouched, and the receipt
// takes it back.
func TestSecretaryClosesADateByOneSentenceAndUndo(t *testing.T) {
	for _, as := range []string{"dropped", "done", "task"} {
		t.Run(as, func(t *testing.T) {
			f := phase25B234NewFixture(t)
			g, refs := f.groupTexts(t, "虚构每周二晚上有课。", "虚构月报先写结论。")
			f.card(t, g, refs, false)
			f.exec(t, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,recurrence,title,time_note) VALUES($1,$2,$3,$4,'recurring','每周二晚上','虚构晚课','')`, f.scope.OwnerID, memory.NewID(), refs[0].ID, refs[0].Version)
			listed := func() bool {
				var n int
				if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM deadlines d JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(d.owner_id,d.claim_id,d.claim_version)
 WHERE d.owner_id=$1 AND d.claim_id=$2 AND NOT(coalesce(c.scope->>'deadline_completed','false')='true' AND c.scope->>'deadline_completed_version'=c.version::text)`, f.scope.OwnerID, refs[0].ID).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n == 1
			}
			alias := regexp.MustCompile(`\[(M\d+) / [^\]]*\] 虚构每周二晚上有课`)
			calls := 0
			f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
				calls++
				prompt := phase25B4Prompt(r)
				// The section is written only while there is something in it.
				section := ""
				if i := strings.Index(prompt, "期限和固定安排"); i >= 0 {
					section = prompt[i:]
					if j := strings.Index(section, "补充记忆"); j >= 0 {
						section = section[:j]
					}
				}
				if calls > 1 {
					// Closed, it is no longer in the list the secretary is shown.
					if strings.Contains(section, "虚构晚课") {
						t.Error("closed date still listed for the secretary")
					}
					return phase25B4ReplyJSON(phase25B4Reply{text: "好。"}, false)
				}
				m := alias.FindStringSubmatch(section)
				if m == nil {
					t.Error("date's memory has no alias in the prompt")
					return phase25B4ReplyJSON(phase25B4Reply{text: "没找到。"}, false)
				}
				action, _ := json.Marshal(map[string]string{"op": "close_date", "ref": m[1], "as": as})
				return phase25B4ReplyJSON(phase25B4Reply{text: "好，办了。", actions: []json.RawMessage{action}}, false)
			})
			out := f.secretaryTurn(t, "晚课那个不用再提了", "")
			if len(out.Turn.Receipts) != 1 {
				t.Fatalf("receipts=%+v", out.Turn.Receipts)
			}
			receipt := out.Turn.Receipts[0]
			if receipt.Op != "close_date" || receipt.Status != "done" || !receipt.Undoable || receipt.ActionID == nil || !strings.Contains(receipt.Text, "虚构晚课") {
				t.Fatalf("receipt=%+v", receipt)
			}
			if listed() {
				t.Fatal("date still listed after close")
			}
			var closedAs string
			if err := f.db.QueryRow(f.ctx, `SELECT scope->>'deadline_closed_as' FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, refs[0].ID, refs[0].Version).Scan(&closedAs); err != nil || closedAs != as {
				t.Fatalf("closed as %q, want %q (%v)", closedAs, as, err)
			}
			// The memory itself is not rewritten.
			f.assertRevisions(t, refs...)
			f.secretaryTurn(t, "还有什么安排", "")
			state, err := f.store.Snapshot(f.ctx, f.scope)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.Execute(f.ctx, f.scope, workspace.Command{RequestID: string(memory.NewID()), ExpectedRevision: state.Revision, Type: "undoAction", ID: *receipt.ActionID}); err != nil {
				t.Fatal(err)
			}
			if !listed() {
				t.Fatal("undo did not bring the date back")
			}
			var left *string
			if err := f.db.QueryRow(f.ctx, `SELECT scope->>'deadline_closed_as' FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, refs[0].ID, refs[0].Version).Scan(&left); err != nil || left != nil {
				t.Fatalf("undo left the mark %v (%v)", left, err)
			}
		})
	}
}

// A ref that is not a memory the prompt showed, or a memory with no date, is
// skipped with a reason and changes nothing.
func TestSecretaryCloseDateRefusesUnknownRefAndDatelessMemory(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.groupTexts(t, "虚构月报先写结论。")
	f.card(t, g, refs, false)
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		return phase25B4ReplyJSON(phase25B4Reply{text: "办了。", actions: []json.RawMessage{
			json.RawMessage(`{"op":"close_date","ref":"M99","as":"dropped"}`),
			json.RawMessage(`{"op":"close_date","ref":"` + string(refs[0].ID) + `","as":"dropped"}`),
			json.RawMessage(`{"op":"close_date","ref":"M1","as":"forgotten"}`),
		}}, false)
	})
	out := f.secretaryTurn(t, "把那个去掉", "")
	if len(out.Turn.Receipts) != 3 {
		t.Fatalf("receipts=%+v", out.Turn.Receipts)
	}
	for _, r := range out.Turn.Receipts {
		if r.Status != "skipped" || r.Reason == "" || r.Undoable {
			t.Fatalf("receipt=%+v", r)
		}
	}
	f.assertRevisions(t, refs...)
}
