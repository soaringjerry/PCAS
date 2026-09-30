package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestEditedArtifactRetainsFieldProvenance(t *testing.T) {
	for _, kind := range []string{"task", "idea", "project"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			ctx := context.Background()
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "私密事实"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "私密事实"})
			mem := st.Memories[0]
			id := string(memory.NewID())
			workspaceCommand(t, s, scope, workspace.Command{Type: map[string]string{"task": "addTask", "idea": "addIdea", "project": "addProject"}[kind], ID: id, Title: "普通事项", Name: "普通项目"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", Kind: "summary", Prompt: "总结私密事实"})
			run := st.Runs[0]
			if !hasArtifactDependency(run.ContextVersions, mem.ID) {
				t.Fatal("fixture run did not include the private memory")
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "私密事实生成的说明。"})
			workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "progress", Text: "私密事实生成的说明。"})
			edit := workspace.Command{Type: "setNotes", ID: id, Text: "私密事实生成的说明！"}
			if kind == "project" {
				edit = workspace.Command{Type: "updateProject", ID: id, Patch: asJSON(map[string]any{"progress": "私密事实生成的说明！"})}
			}
			workspaceCommand(t, s, scope, edit)
			// Inspect the derived dependency separately from normal memory recall,
			// which could otherwise mask a missing artifact dependency in a run.
			err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				item, err := getItem(ctx, tx, scope, id)
				if err != nil {
					return err
				}
				_, refs, err := sanitizeItemTx(ctx, tx, scope, "manual", item)
				if err == nil && !hasArtifactDependency(refs, mem.ID) {
					t.Error("editing inside the field lost its dependency")
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: mem.ID, AgentIDs: []string{}})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", Kind: "ask", Prompt: "重新总结"})
			if strings.Contains(st.Runs[0].Brief, "私密事实") {
				t.Fatal("edited adopted field bypassed revocation")
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: mem.ID, IncludeSources: true})
			data, err := s.Export(ctx, scope, false, false)
			if err != nil || strings.Contains(string(data), "私密事实") {
				t.Fatalf("edited derived content survived deletion: %v", err)
			}
		})
	}
}

func TestPromotedIdeaRetainsArtifactProvenance(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "私密事实"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "私密事实"})
	mem := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "普通想法"})
	idea := st.Ideas[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: idea.ID, AgentID: "manual", Kind: "summary", Prompt: "总结私密事实"})
	run := st.Runs[0]
	if !hasArtifactDependency(run.ContextVersions, mem.ID) {
		t.Fatal("fixture run did not include the private memory")
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "私密事实生成的说明。"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "progress", Text: "私密事实生成的说明。"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: idea.ID, Text: "私密事实生成的说明！"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaPromote", ID: idea.ID})
	task := st.Tasks[0]
	var copied int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM adopted_artifacts WHERE owner_id=$1 AND thing_id=$2 AND run_id=$3 AND kind='notes'", string(scope.OwnerID), task.ID, run.ID).Scan(&copied); err != nil || copied != 1 {
		t.Fatalf("promotion did not copy field provenance: %d %v", copied, err)
	}
	// Simulate a task promoted before provenance was copied by this release.
	if _, err := s.pool.Exec(ctx, "DELETE FROM adopted_artifacts WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), task.ID); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: mem.ID, AgentIDs: []string{}})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task.ID, AgentID: "manual", Kind: "ask", Prompt: "总结待办"})
	if strings.Contains(st.Runs[0].Brief, "私密事实") {
		t.Fatal("promoted copy bypassed revocation")
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM adopted_artifacts WHERE owner_id=$1 AND thing_id=$2 AND run_id=$3 AND kind='notes'", string(scope.OwnerID), task.ID, run.ID).Scan(&copied); err != nil || copied != 1 {
		t.Fatalf("legacy promotion was not repaired before use: %d %v", copied, err)
	}
	// Deletion must repair old promotions independently of a later model run.
	if _, err := s.pool.Exec(ctx, "DELETE FROM adopted_artifacts WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), task.ID); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: mem.ID, IncludeSources: true})
	data, err := s.Export(ctx, scope, false, false)
	if err != nil || strings.Contains(string(data), "私密事实") {
		t.Fatalf("promoted derived content survived deletion: %v", err)
	}
}

func hasArtifactDependency(refs []memory.Ref, id string) bool {
	for _, ref := range refs {
		if string(ref.ID) == id {
			return true
		}
	}
	return false
}
