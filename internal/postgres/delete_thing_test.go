package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func tryCommand(s *Store, scope memory.Scope, c workspace.Command) error {
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		return err
	}
	c.RequestID, c.ExpectedRevision = string(memory.NewID()), st.Revision
	_, err = s.Execute(context.Background(), scope, c)
	return err
}

// A deleted to-do is gone with what hangs off it: the receipt memory kept of
// its changes, and other to-dos' dependence on it. A project that still holds
// things is not deleted.
func TestDeleteThingRemovesItAndWhatHangsOffIt(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Title: "搬家", Name: "搬家"})
	project := st.Projects[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "订搬家车", ProjectID: project})
	first := urgentTask(t, st, "订搬家车").ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "打包"})
	second := urgentTask(t, st, "打包").ID
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: second, Patch: asJSON(map[string][]string{"dependsOn": {first}})})
	receipts := func() int {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector='actions' AND s.external_id=$2 AND r.state='active'`, string(scope.OwnerID), first).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if receipts() != 1 {
		t.Fatal("fixture: the to-do has no receipt in memory")
	}
	if err := tryCommand(s, scope, workspace.Command{Type: "deleteThing", ID: project}); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("project with a to-do in it: %v", err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteThing", ID: first})
	if len(st.Tasks) != 1 || st.Tasks[0].ID != second || len(st.Tasks[0].DependsOn) != 0 {
		t.Fatalf("after delete: %+v", st.Tasks)
	}
	if receipts() != 0 {
		t.Fatal("the deleted to-do's receipt is still in memory")
	}
	if err := tryCommand(s, scope, workspace.Command{Type: "deleteThing", ID: first}); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("deleting twice: %v", err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteThing", ID: project})
	if len(st.Projects) != 0 {
		t.Fatal("empty project not deleted")
	}
	if err := tryCommand(s, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "agent"}, workspace.Command{Type: "deleteThing", ID: second}); err == nil {
		t.Fatal("a non-owner deleted a to-do")
	}
}

// A deleted item leaves nothing of its text behind: not in the snapshots undo
// keeps, and not blocked by the training sample an adopted result produced.
func TestDeleteThingAfterAdoptedResultLeavesNoCopies(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	autoAdoptModel(t, s, "- [ ] 独特步骤甲7731\n- [ ] 独特步骤乙7731", nil)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "独特标题7731"})
	id := st.Tasks[0].ID
	workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: id, Text: "独特说明7731"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: id, Status: "doing"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "auto-model", Kind: "breakdown", Prompt: "列步骤"})
	if err := s.runAgentOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var samples int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM training_samples t JOIN agent_runs r ON (r.owner_id,r.id)=(t.owner_id,t.run_id) WHERE r.owner_id=$1 AND r.thing_id=$2`, string(scope.OwnerID), id).Scan(&samples); err != nil || samples == 0 {
		t.Fatalf("fixture: the adopted result produced no sample (%d, %v)", samples, err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteThing", ID: id})
	if len(st.Tasks) != 0 || len(st.Runs) != 0 || len(st.Samples) != 0 {
		t.Fatalf("after delete: %d tasks, %d runs, %d samples", len(st.Tasks), len(st.Runs), len(st.Samples))
	}
	for table, query := range map[string]string{
		"action_log":       `SELECT count(*) FROM action_log WHERE owner_id=$1 AND changes::text LIKE '%7731%'`,
		"agent_runs":       `SELECT count(*) FROM agent_runs WHERE owner_id=$1 AND document::text LIKE '%7731%'`,
		"training_samples": `SELECT count(*) FROM training_samples WHERE owner_id=$1 AND document::text LIKE '%7731%'`,
		"work_items":       `SELECT count(*) FROM work_items WHERE owner_id=$1 AND document::text LIKE '%7731%'`,
		"work_documents":   `SELECT count(*) FROM work_documents WHERE owner_id=$1 AND document::text LIKE '%7731%'`,
	} {
		var n int
		if err := s.pool.QueryRow(ctx, query, string(scope.OwnerID)).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s still holds the deleted item's text: %d rows %v", table, n, err)
		}
	}
}
