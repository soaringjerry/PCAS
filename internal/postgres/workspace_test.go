package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/ai/siwc"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestDirectProviderPreservesExistingSubscriptionPermissions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	legacy, err := ai.Load("", &ai.Codex{})
	if err != nil {
		t.Fatal(err)
	}
	s.SetModels(legacy)
	state, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Execute(ctx, scope, workspace.Command{Type: "capture", Text: "现有记忆", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Execute(ctx, scope, workspace.Command{Type: "acceptCandidate", ID: state.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "现有记忆", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
	if err != nil {
		t.Fatal(err)
	}
	id := state.Memories[0].ID
	state, err = s.Execute(ctx, scope, workspace.Command{Type: "setMemoryVisibility", ID: id, AgentIDs: []string{"chatgpt"}, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
	if err != nil {
		t.Fatal(err)
	}
	m, err := siwc.New(t.TempDir(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	current, err := ai.Load("", &ai.Codex{}, m)
	if err != nil {
		t.Fatal(err)
	}
	s.SetModels(current)
	state, err = s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !oneOf("chatgpt-direct", state.Memories[0].VisibleTo...) {
		t.Fatal("existing subscription permission lost")
	}
	if oneOf("manual", state.Memories[0].VisibleTo...) {
		t.Fatal("permission broadened to unrelated channel")
	}
	for _, a := range state.Agents {
		if a.ID == "chatgpt-direct" && (a.Available || a.Default) {
			t.Fatal("unverified connection became available/default")
		}
	}
	state, err = s.Execute(ctx, scope, workspace.Command{Type: "setMemoryVisibility", ID: id, AgentIDs: []string{"chatgpt"}, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if oneOf("chatgpt-direct", state.Memories[0].VisibleTo...) {
		t.Fatal("snapshot undid explicit grant revocation")
	}
}

func TestWorkspaceMemoryLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	models, err := ai.Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetModels(models)
	state, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 0 || len(state.Memories) != 0 || len(state.Agents) != 1 {
		t.Fatal("workspace must start without demo data")
	}
	command := func(c workspace.Command) {
		t.Helper()
		c.RequestID = string(memory.NewID())
		c.ExpectedRevision = state.Revision
		var err error
		state, err = s.Execute(ctx, scope, c)
		if err != nil {
			t.Fatalf("%s: %v", c.Type, err)
		}
	}
	projectID := string(memory.NewID())
	command(workspace.Command{Type: "addProject", ID: projectID, Name: "PCAS"})
	command(workspace.Command{Type: "addTask", Title: "接入记忆系统", ProjectID: projectID})
	taskID := state.Tasks[0].ID
	command(workspace.Command{Type: "updateTask", ID: taskID, Patch: asJSON(map[string]any{"due": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)})})
	command(workspace.Command{Type: "capture", Text: "我一直想去河边那家旧书店"})
	candidate := state.Candidates[0]
	command(workspace.Command{Type: "acceptCandidate", ID: candidate.ID, Kind: "memory", MemoryKind: "intention", Text: candidate.Text, ProjectID: projectID})
	mem := state.Memories[0]
	result, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "旧书店", Mode: memory.Remember})
	if err != nil || !strings.Contains(result.Summary, "旧书店") {
		t.Fatalf("immediate recall: %+v %v", result, err)
	}
	command(workspace.Command{Type: "requestRun", ThingID: taskID, AgentID: "manual", Kind: "plan", Prompt: "帮我规划"})
	run := state.Runs[0]
	if !strings.Contains(run.Brief, candidate.Text) {
		t.Fatal("missing allowed memory")
	}
	command(workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "安排一次走访"})
	command(workspace.Command{Type: "editMemory", ID: mem.ID, Text: "书店在山边，不是河边", Reason: "地点记错了"})
	if state.Memories[0].Version != 2 || !state.Runs[0].StaleContext {
		t.Fatal("correction failed to propagate")
	}
	_, err = s.Execute(ctx, scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "doc", Text: "安排走访", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
	if !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale adoption allowed: %v", err)
	}
	command(workspace.Command{Type: "setMemoryVisibility", ID: mem.ID, AgentIDs: []string{}})
	command(workspace.Command{Type: "requestRun", ThingID: taskID, AgentID: "manual", Kind: "ask", Prompt: "现在有什么背景"})
	if strings.Contains(state.Runs[0].Brief, "书店") {
		t.Fatal("revoked memory leaked")
	}
	command(workspace.Command{Type: "deleteMemory", ID: mem.ID})
	if len(state.Memories) != 0 {
		t.Fatal("memory not deleted")
	}
	if _, err := s.Export(ctx, scope, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(ctx, owner()); err != nil {
		t.Fatal(err)
	}
}
func TestWorkspaceReplayAndConflict(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	state, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	c := workspace.Command{Type: "addTask", Title: "只创建一次", RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
	a, err := s.Execute(ctx, scope, c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Execute(ctx, scope, c)
	if err != nil || len(b.Tasks) != 1 || a.Revision != b.Revision {
		t.Fatalf("replay: %v", err)
	}
	c.Title = "被篡改"
	if _, err := s.Execute(ctx, scope, c); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("idempotency body mismatch allowed")
	}
	c.RequestID = string(memory.NewID())
	if _, err := s.Execute(ctx, scope, c); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("lost update allowed")
	}
	if _, err := s.Execute(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "agent"}, c); !errors.Is(err, memory.ErrForbidden) {
		t.Fatal("agent wrote owner state")
	}
}
func TestHistoryPaginationAndDeleteSource(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	in := input()
	in.Text = "河边书店"
	first := mustIngest(t, s, scope, in)
	in.ExternalVersion = "v2"
	in.Text = "山边书店"
	mustIngest(t, s, scope, in)
	request := memory.RecallRequest{Query: "书店", Mode: memory.History, Budget: memory.Budget{Candidates: 1}}
	one, err := s.Recall(ctx, scope, request)
	if err != nil || one.Coverage.NextCursor == "" {
		t.Fatalf("missing history cursor: %+v %v", one, err)
	}
	request.Cursor = one.Coverage.NextCursor
	two, err := s.Recall(ctx, scope, request)
	if err != nil || len(two.Memories) != 1 || two.Memories[0] == one.Memories[0] {
		t.Fatalf("pagination: %+v %v", two, err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: first.ID, Version: 2, Kind: memory.SourceKind}}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.NewService(s).Ingest(ctx, scope, in); !errors.Is(err, memory.ErrBlocked) {
		t.Fatal("deleted source resurrected", err)
	}
	data, err := s.Export(ctx, scope, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "书店") {
		t.Fatal("deleted text retained in export")
	}
}
