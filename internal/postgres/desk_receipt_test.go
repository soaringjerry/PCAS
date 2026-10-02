package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSecretaryReceiptsOmitOnlyCurrentThingTitle(t *testing.T) {
	for _, tc := range []struct {
		name, ref string
		inThing   bool
		short     bool
	}{{"hall", "T1", false, false}, {"THIS", "THIS", true, true}, {"same item through another alias", "T1", true, true}, {"other item", "T2", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			loc, err := time.LoadLocation("Asia/Shanghai")
			if err != nil {
				t.Fatal(err)
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": loc.String()})})
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "当前安排"})
			currentID := st.Tasks[0].ID
			workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "另一件安排"})
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				secretaryModelReply(w, secretaryOutput{Actions: []secretaryAction{
					{Op: "add_steps", Ref: tc.ref, Steps: []string{"核对", "准备", "发送"}},
					{Op: "update", Ref: tc.ref, Set: map[string]json.RawMessage{"due": asJSON(testsupport.DateFromToday(t, "Asia/Shanghai", 4, 10, 0).Format("2006-01-02T15:04"))}},
					{Op: "update", Ref: tc.ref, Set: map[string]json.RawMessage{"status": asJSON("done")}},
				}})
			})
			req := turnRequest("加三步，改到周一十点，再完成")
			if tc.inThing {
				req.ThingID = &currentID
			}
			out := mustTurn(t, s, scope, req)
			title := "当前安排"
			if tc.ref == "T2" {
				title = "另一件安排"
			}
			date := localDeskDate(testsupport.DateFromToday(t, "Asia/Shanghai", 4, 10, 0).UTC().Format(time.RFC3339), loc)
			want := []string{"给「" + title + "」加了 3 步", "已改：" + title + " → " + date, "已完成：" + title}
			if tc.short {
				want = []string{"加了 3 步", "已改：→ " + date, "已完成"}
			}
			check := func(turn workspace.SecretaryTurn) {
				t.Helper()
				if len(turn.Receipts) != len(want) {
					t.Fatal(turn)
				}
				for i, receipt := range turn.Receipts {
					if receipt.Text != want[i] || receipt.ActionID == nil || !receipt.Undoable || receipt.Status != "done" || receipt.ThingID == nil || (tc.short && !strings.EqualFold(*receipt.ThingID, currentID)) {
						t.Fatal("incorrect receipt or action metadata", receipt, want[i])
					}
				}
			}
			check(out.Turn)
			check(mustTurn(t, s, scope, req).Turn)
			history, err := s.DeskTurns(context.Background(), scope, out.ConversationID)
			if err != nil || len(history.Turns) != 1 {
				t.Fatal(err, history)
			}
			check(history.Turns[0])
		})
	}
}
