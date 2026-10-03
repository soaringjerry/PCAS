package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Work handed to an agent reports back once through the notice channels with
// what it produced, and is told apart from a reminder that came due.
func TestFinishedRunReportsBack(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	autoAdoptModel(t, s, "研究结论：三层记忆已经落地。\n第二段细节。", nil)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]bool{"followUps": true})})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "研究 PCAS"})
	id := st.Tasks[0].ID
	workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "auto-model", Kind: "summary", Prompt: "写研究报告"})
	if err := s.runAgentOnce(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Notices) != 1 || !st.Notices[0].Result || st.Notices[0].ThingID != id || !strings.HasPrefix(st.Notices[0].Reason, "副手做完了：") || !strings.Contains(st.Notices[0].Reason, "三层记忆已经落地") {
		t.Fatalf("notices: %+v", st.Notices)
	}
	for _, entry := range st.Activity {
		if strings.HasPrefix(entry.Text, "提醒了你") {
			t.Fatalf("a result is listed as a reminder: %+v", entry)
		}
	}
	channel := &fakeNotifyChannel{name: "telegram"}
	for range 2 {
		if err := s.DispatchNotices(ctx, time.Now(), []notify.Channel{channel}); err != nil {
			t.Fatal(err)
		}
	}
	if len(channel.calls) != 1 {
		t.Fatalf("sent %d times", len(channel.calls))
	}
	m := channel.calls[0]
	if !m.Result || m.Title != "研究 PCAS" || !strings.HasPrefix(m.Body, "副手做完了：") || !strings.Contains(m.Body, "第二段细节") || !strings.HasSuffix(m.URL, "/t/"+id) {
		t.Fatalf("message: %+v", m)
	}
}
