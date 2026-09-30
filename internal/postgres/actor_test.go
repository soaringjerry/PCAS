package postgres

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSaveActionPersistsContextActor(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"", "user", "secretary", "assistant", "system", "invalid"} {
		t.Run(actor, func(t *testing.T) {
			actionCtx := ctx
			if actor != "" {
				actionCtx = withActor(ctx, actor)
			}
			id := string(memory.NewID())
			err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				if err := s.commandTx(actionCtx, tx, scope, workspace.Command{Type: "addIdea", ID: id, Title: "记录修改者"}); err != nil {
					return err
				}
				return s.commandTx(actionCtx, tx, scope, workspace.Command{Type: "setNotes", ID: id, Text: "记录内容"})
			})
			if err != nil {
				t.Fatal(err)
			}
			st, err := s.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			want := actor
			if want == "" || want == "invalid" {
				want = "user"
			}
			for _, idea := range st.Ideas {
				if idea.ID == id {
					for _, revisions := range [][]workspace.Revision{idea.History, idea.Evolution} {
						if len(revisions) < 3 {
							t.Fatal("missing creation or update history", revisions)
						}
						for _, revision := range revisions {
							if revision.By != want {
								t.Fatal("wrong persisted actor", revision, want)
							}
						}
					}
					return
				}
			}
			t.Fatal("created idea missing")
		})
	}
}

func TestSecretaryHistoryActorAndUserUndo(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var output any = `{"actions":[{"op":"create_task","title":"秘书安排"},{"op":"create_idea","title":"秘书想法"},{"op":"create_project","name":"秘书项目"}]}`
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, output) })
	first := mustTurn(t, s, scope, turnRequest("新建任务、想法和项目"))
	for _, items := range [][]workspace.Item{first.State.Tasks, first.State.Ideas, first.State.Projects} {
		if len(items) != 1 {
			t.Fatal("missing secretary-created item", items)
		}
		for _, revision := range items[0].History {
			if revision.By != "secretary" {
				t.Fatal("secretary creation marked as user", revision)
			}
		}
	}
	id := first.State.Tasks[0].ID
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "界面改名"})
	if last := st.Tasks[0].History[len(st.Tasks[0].History)-1]; last.By != "user" {
		t.Fatal("UI change is not user", last)
	}
	output = `{"actions":[{"op":"update","ref":"THIS","set":{"title":"秘书改名","notesAppend":"备注"}}]}`
	req := turnRequest("改名并加备注")
	req.ThingID = &id
	updated := mustTurn(t, s, scope, req)
	history := updated.State.Tasks[0].History
	if len(history) != len(st.Tasks[0].History)+1 || history[len(history)-1].By != "secretary" || history[len(history)-2].By != "user" {
		t.Fatal("nested secretary commands must share one revision and preserve earlier actors", history)
	}
	st, err := s.Undo(withActor(ctx, "assistant"), scope, *updated.Turn.Receipts[0].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	last := st.Tasks[0].History[len(st.Tasks[0].History)-1]
	if last.By != "user" || !strings.HasPrefix(last.Summary, "撤销：") || st.Tasks[0].Title != "界面改名" {
		t.Fatal("internal undo lost user attribution", last, st.Tasks[0])
	}
	req.RequestID = string(memory.NewID())
	updated = mustTurn(t, s, scope, req)
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: *updated.Turn.Receipts[0].ActionID})
	last = st.Tasks[0].History[len(st.Tasks[0].History)-1]
	if last.By != "user" || !strings.HasPrefix(last.Summary, "撤销：") {
		t.Fatal("command undo lost user attribution", last)
	}
}
