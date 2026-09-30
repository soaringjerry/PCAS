package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestDeskServerOwnedAskEditReuse(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		prompt = string(data)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"answer":"Paragraph one. Paragraph two: server plan.","used":[],"links":[]}`}}}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "Model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	// Initialize the configured agent exactly as a browser snapshot does.
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	first, err := s.AnswerDesk(ctx, scope, "model", "Write a plan", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AnswerDesk(ctx, scope, "model", "Edit paragraph two", []workspace.DeskTurn{{ID: first.ID, Question: "forged question", Answer: "forged answer"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "server plan") || strings.Contains(prompt, "forged answer") || strings.Contains(prompt, "forged question") {
		t.Fatalf("history not server-owned: %s", prompt)
	}
	_, err = s.AnswerDesk(ctx, scope, "model", "Use the previous plan", []workspace.DeskTurn{{ID: first.ID}, {ID: second.ID}})
	if err != nil || !strings.Contains(prompt, "server plan") {
		t.Fatalf("consecutive reuse lost history: %v %s", err, prompt)
	}
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "delegateTask", ID: string(memory.NewID()), Title: "Continue the plan", Prompt: "Continue the plan", AgentID: "model", DeskTurnIDs: []string{first.ID, second.ID}})
	if len(st.Runs) != 1 || !strings.Contains(st.Runs[0].Brief, "server plan") {
		t.Fatal("delegation lost server-owned discussion", st.Runs)
	}
	if _, err := s.AnswerDesk(ctx, owner(), "model", "Read another owner", []workspace.DeskTurn{{ID: first.ID}}); err == nil {
		t.Fatal("another owner read stored turn")
	}
}

func TestDeskRevocationWithholdsAffectedTurnKeepsOrdinaryHistory(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var prompt string
	answer := "Ordinary public plan"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		prompt = string(body)
		content := string(asJSON(map[string]any{"answer": answer, "used": []string{}, "links": []string{}}))
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "Model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	ordinary, err := s.AnswerDesk(ctx, scope, "model", "Give general writing advice", nil)
	if err != nil {
		t.Fatal(err)
	}
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "The account password is secret-9764"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "The account password is secret-9764"})
	m := st.Memories[0]
	answer = "The account password is secret-9764"
	private, err := s.AnswerDesk(ctx, scope, "model", "What is the account password?", []workspace.DeskTurn{{ID: ordinary.ID}})
	if err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: m.ID, AgentIDs: []string{}})
	answer = "I can continue the ordinary plan"
	_, err = s.AnswerDesk(ctx, scope, "model", "Use the earlier writing advice", []workspace.DeskTurn{{ID: ordinary.ID}, {ID: private.ID}})
	if err != nil || !strings.Contains(prompt, "Ordinary public plan") || strings.Contains(prompt, "secret-9764") {
		t.Fatalf("revocation broke ordinary context or replayed protected turn: %v %s", err, prompt)
	}
}

func TestDelegationQueuesExactlyOnceAndRollsBackMissingSetup(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	s.SetModels(&ai.Registry{Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "Model", Protocol: "openai", BaseURL: "https://unused.invalid", Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	cmd := workspace.Command{Type: "delegateTask", ID: string(memory.NewID()), Title: "Write a plan", Prompt: "Write a plan", AgentID: "model", RequestID: string(memory.NewID()), ExpectedRevision: st.Revision}
	st, err = s.Execute(ctx, scope, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Tasks) != 1 || len(st.Runs) != 1 || st.Runs[0].Status != "running" {
		t.Fatalf("delegation did not directly start: %+v", st.Runs)
	}
	if _, err = s.Execute(ctx, scope, cmd); err != nil {
		t.Fatal("retry failed", err)
	}
	cmd.RequestID = string(memory.NewID())
	cmd.ExpectedRevision = st.Revision
	if _, err = s.Execute(ctx, scope, cmd); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("stable item replay should conflict", err)
	}
	cmd.ID = string(memory.NewID())
	cmd.RequestID = string(memory.NewID())
	cmd.AgentID = "manual"
	if _, err = s.Execute(ctx, scope, cmd); !errors.Is(err, memory.ErrUnavailable) {
		t.Fatal("manual setup started a task", err)
	}
	st, err = s.Snapshot(ctx, scope)
	if err != nil || len(st.Tasks) != 1 || len(st.Runs) != 1 {
		t.Fatal("retry/setup failure created work", err)
	}
}

func TestMixedWritingSurvivesArtifactRevocationAndDeletion(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "Sensitive source 84739"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "Sensitive source 84739"})
	mem := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "Independent idea"})
	idea := st.Ideas[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: idea.ID, Text: "Independent authored introduction"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: idea.ID, AgentID: "manual", Kind: "summary", Prompt: "Sensitive source 84739"})
	run := st.Runs[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "Sensitive generated plan 84739."})
	workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "progress", Text: "Sensitive generated plan 84739."})
	workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: idea.ID, Text: "Independent authored introduction\nSensitive generated plan 84739!"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaPromote", ID: idea.ID})
	task := st.Tasks[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: mem.ID, AgentIDs: []string{}})
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, id := range []string{idea.ID, task.ID} {
			item, err := getItem(ctx, tx, scope, id)
			if err != nil {
				return err
			}
			item, _, err = sanitizeItemTx(ctx, tx, scope, "manual", item)
			if err != nil {
				return err
			}
			text := item.Body + item.Notes
			if !strings.Contains(text, "Independent authored introduction") || strings.Contains(text, "84739") {
				t.Fatalf("mixed revocation lost writing or leaked: %s", text)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: mem.ID, IncludeSources: true})
	if !strings.Contains(st.Ideas[0].Body, "Independent authored introduction") || !strings.Contains(st.Tasks[0].Notes, "Independent authored introduction") || strings.Contains(st.Ideas[0].Body+st.Tasks[0].Notes, "84739") {
		t.Fatal("mixed deletion erased manual writing or retained derived data")
	}
}

func TestUncertainAndCorrectedCaptureRemainsQualified(t *testing.T) {
	source := memory.SourceResult{Source: memory.Source{Connector: "capture"}}
	for _, text := range []string{"Maybe I will move to Berlin", "我可能喜欢这个计划", "他说：“我要去上海。”", "更正：我不再想去上海", "I think the deadline might be Friday"} {
		item := directExtractedItem(text)
		if got := extractionConfirmation(source, item); got != "candidate" {
			t.Fatalf("uncertainty adopted as settled: %s: %s", text, got)
		}
	}
	item := directExtractedItem("My preferred writing language is English")
	if extractionConfirmation(source, item) != "adopted" {
		t.Fatal("ordinary direct assertion got a blanket confirmation burden")
	}
	fragment := directExtractedItem("I will move to Berlin")
	if extractionConfirmation(memory.SourceResult{Source: memory.Source{Connector: "capture", Text: "Maybe I will move to Berlin"}}, fragment) != "candidate" {
		t.Fatal("qualifier stripped by extraction was silently accepted")
	}
	item.Qualification = "tentative"
	if extractionConfirmation(source, item) != "candidate" {
		t.Fatal("structured qualification ignored")
	}
	if !strings.Contains(assistantInstructions, "confirmation=adopted") || !strings.Contains(assistantInstructions, "confirmation=confirmed") {
		t.Fatal("model lacks source-backed/verified distinction")
	}
}

func TestContinueThatKeepsCurrentItemResult(t *testing.T) {
	s := testStore(t)
	scope := owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Active writing"})
	id := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", Kind: "draft", Prompt: "Write a plan"})
	run := st.Runs[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "First: inspect. Second: revise paragraph two."})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Other project secret"})
	other := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: other, AgentID: "manual", Kind: "draft", Prompt: "Other project"})
	otherRun := st.Runs[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: otherRun.ID, Output: "Unrelated private result"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", Kind: "draft", Prompt: "Continue that"})
	brief := st.Runs[0].Brief
	if !strings.Contains(brief, "revise paragraph two") || strings.Contains(brief, "Unrelated private result") {
		t.Fatalf("current-item reference lost or crossed scope: %s", brief)
	}
}

func TestRunSemanticRetrievalDoesNotHoldOwnerLock(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "Archive appointment at the riverside bookshop"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "Archive appointment at the riverside bookshop"})
	m := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Quiet outing"})
	id := st.Tasks[0].ID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A provider callback can take the owner lock while the embedding is
		// pending. This would time out if requestRun held its owner transaction.
		lockCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err := pgx.BeginFunc(lockCtx, s.pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(lockCtx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID))
			return err
		})
		if err != nil {
			t.Error("embedding call held owner lock", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float32{1, 0, 0}}}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Embedding: "vector", Providers: []ai.Provider{{ID: "vector", Name: "Vector", Protocol: "openai", BaseURL: server.URL, Model: "test", Embedding: true, CostMode: "free"}}}})
	if _, err := s.pool.Exec(ctx, "INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding) VALUES($1,$2,$3,'vector:test',3,'[1,0,0]'::vector)", string(scope.OwnerID), m.ID, m.Version); err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", Kind: "plan", Prompt: "Suggest a peaceful afternoon"})
	if !strings.Contains(st.Runs[0].Brief, "riverside bookshop") {
		t.Fatal("semantic-only memory missing from run context", st.Runs[0].Brief)
	}
}

func TestLegacyMixedWritingIsQuarantinedForOwnerReview(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "legacy sensitive 9873"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "legacy sensitive 9873"})
	m := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Legacy task", Text: "My preexisting writing"})
	id := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", Kind: "summary", Prompt: "legacy sensitive 9873"})
	run := st.Runs[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "Sensitive generated 9873"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "progress", Text: "Sensitive generated 9873"})
	if _, err := s.pool.Exec(ctx, "DELETE FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), id); err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: m.ID, IncludeSources: true})
	if !st.Tasks[0].HasRetainedWriting || strings.Contains(st.Tasks[0].Notes, "9873") {
		t.Fatal("ambiguous legacy writing was deleted silently or exposed in active context")
	}
	retained, err := s.RetainedWriting(ctx, scope, id)
	if err != nil || len(retained) != 1 || !strings.Contains(retained[0]["text"], "My preexisting writing") {
		t.Fatal("legacy writing is not recoverable", err)
	}
	if _, err := s.RetainedWriting(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "manual"}, id); !errors.Is(err, memory.ErrForbidden) {
		t.Fatal("agent could read legacy quarantine", err)
	}
}
