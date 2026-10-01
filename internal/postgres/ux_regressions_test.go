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
	phase2ManualDestination(t, s)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "Sensitive source 84739"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "Sensitive source 84739"})
	mem := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "Independent idea"})
	idea := st.Ideas[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: idea.ID, Text: "Independent authored introduction"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: idea.ID, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "summary", Prompt: "Sensitive source 84739"})
	run := st.Runs[0]
	phase2RTGetPackage(t, s, scope, run)
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "Sensitive generated plan 84739."})
	undoAutoAdoption(t, s, scope, run.ID)
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
	for _, text := range []string{"Maybe I will move to Berlin", "Probably I will move to Berlin", "我可能喜欢这个计划", "他说：“我要去上海。”", "更正：我不再想去上海", "I think the deadline might be Friday"} {
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
	for _, sourceText := range []string{
		"My preferred writing language is English. Maybe I will learn German.",
		"Maybe I will learn German. My preferred writing language is English.",
		"我可能会学习德语。My preferred writing language is English。也许以后再改。",
	} {
		if extractionConfirmation(memory.SourceResult{Source: memory.Source{Connector: "capture", Text: sourceText}}, item) != "adopted" {
			t.Fatalf("unrelated tentative sentence added a confirmation burden: %s", sourceText)
		}
	}
	completeSentence := directExtractedItem("My preferred writing language is English.")
	if extractionConfirmation(memory.SourceResult{Source: memory.Source{Connector: "capture", Text: completeSentence.Text + " Maybe I will learn German."}}, completeSentence) != "adopted" {
		t.Fatal("complete quoted sentence inherited the next sentence's uncertainty")
	}
	if extractionConfirmation(source, directExtractedItem("My surname is Ifield")) != "adopted" {
		t.Fatal("qualifier substring demoted an ordinary assertion")
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
	phase2ManualDestination(t, s)
	scope := owner()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Active writing"})
	id := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "draft", Prompt: "Write a plan"})
	run := st.Runs[0]
	phase2RTGetPackage(t, s, scope, run)
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "First: inspect. Second: revise paragraph two."})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Other project secret"})
	other := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: other, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "draft", Prompt: "Other project"})
	otherRun := st.Runs[0]
	phase2RTGetPackage(t, s, scope, otherRun)
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: otherRun.ID, Output: "Unrelated private result"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "draft", Prompt: "Continue that"})
	brief := phase2RTGetPackage(t, s, scope, st.Runs[0]).Text
	if !strings.Contains(brief, "revise paragraph two") || strings.Contains(brief, "Unrelated private result") {
		t.Fatalf("current-item reference lost or crossed scope: %s", brief)
	}
}

func TestRunSemanticRetrievalDoesNotHoldOwnerLock(t *testing.T) {
	s := testStore(t)
	phase2ManualDestination(t, s)
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
	manualDestination, ok := s.models.Get(phase2ManualProvider)
	if !ok {
		t.Fatal("configured manual destination lost")
	}
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Embedding: "vector", Providers: []ai.Provider{{ID: "vector", Name: "Vector", Protocol: "openai", BaseURL: server.URL, Model: "test", Embedding: true, CostMode: "free"}, manualDestination}}})
	if _, err := s.pool.Exec(ctx, "INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding) VALUES($1,$2,$3,'vector:test',3,'[1,0,0]'::vector)", string(scope.OwnerID), m.ID, m.Version); err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "plan", Prompt: "Suggest a peaceful afternoon"})
	pkg := phase2RTGetPackage(t, s, scope, st.Runs[0])
	if !strings.Contains(pkg.Text, "riverside bookshop") {
		t.Fatal("semantic-only memory missing from run context", pkg.Text)
	}
}

func TestLegacyMixedWritingIsQuarantinedForOwnerReview(t *testing.T) {
	s := testStore(t)
	phase2ManualDestination(t, s)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "legacy sensitive 9873"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "legacy sensitive 9873"})
	m := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Legacy task", Text: "My preexisting writing"})
	id := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "summary", Prompt: "legacy sensitive 9873"})
	run := st.Runs[0]
	phase2RTGetPackage(t, s, scope, run)
	workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "Sensitive generated 9873"})
	undoAutoAdoption(t, s, scope, run.ID)
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

func TestContinuationHonorsCurrentItemScopeBeforeSemanticRetrieval(t *testing.T) {
	for _, change := range []string{"exclude", "move-project", "revoke"} {
		t.Run(change, func(t *testing.T) {
			s := testStore(t)
			phase2ManualDestination(t, s)
			scope := owner()
			project := string(memory.NewID())
			workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", ID: project, Name: "Original project"})
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Writing", ProjectID: project})
			item := st.Tasks[0]
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: item.ID, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "draft", Prompt: "Write an ordinary plan"})
			phase2RTGetPackage(t, s, scope, st.Runs[0])
			workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: st.Runs[0].ID, Output: "Ordinary permitted plan"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "Private launch code 482910"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "Private launch code 482910", ProjectID: project})
			m := st.Memories[0]
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: item.ID, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "draft", Prompt: "Use private launch code 482910"})
			if !oneOf(m.ID, st.Runs[0].ContextMemoryIDs...) {
				t.Fatal("fixture did not include private memory")
			}
			privateRun := st.Runs[0].ID
			phase2RTGetPackage(t, s, scope, st.Runs[0])
			workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: privateRun, Output: "Derived private result 482910"})
			undoAutoAdoption(t, s, scope, privateRun)
			workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: privateRun, As: "progress", Text: "Derived private result 482910"})
			switch change {
			case "exclude":
				workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: item.ID, MemoryID: m.ID})
			case "move-project":
				other := string(memory.NewID())
				workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", ID: other, Name: "Other project"})
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: item.ID, Patch: asJSON(map[string]any{"projectId": other})})
			case "revoke":
				workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: m.ID, AgentIDs: []string{}})
			}
			var embeddingInput string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				embeddingInput = string(body)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float32{1, 0, 0}}}})
			}))
			defer server.Close()
			manualDestination, ok := s.models.Get(phase2ManualProvider)
			if !ok {
				t.Fatal("configured manual destination lost")
			}
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Embedding: "vector", Providers: []ai.Provider{{ID: "vector", Name: "Vector", Protocol: "openai", BaseURL: server.URL, Model: "test", Embedding: true, CostMode: "free"}, manualDestination}}})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: item.ID, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "draft", Prompt: "Continue that"})
			if embeddingInput == "" || strings.Contains(embeddingInput, "482910") || !strings.Contains(embeddingInput, "Ordinary permitted plan") {
				t.Fatalf("semantic query lost permitted history or leaked denied result: %s", embeddingInput)
			}
			pkg := phase2RTGetPackage(t, s, scope, st.Runs[0])
			if strings.Contains(pkg.Text, "482910") || !strings.Contains(pkg.Text, "Ordinary permitted plan") || oneOf(m.ID, st.Runs[0].ContextMemoryIDs...) {
				t.Fatalf("continuation bypassed current scope or lost ordinary context: %+v", st.Runs[0])
			}
		})
	}
}

func TestDeskHistoryCannotBypassDestinationItemScope(t *testing.T) {
	for _, change := range []string{"exclude", "other-project", "delegate-other-project"} {
		t.Run(change, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			ctx := context.Background()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"answer":"Private derived answer 519823","used":[],"links":[]}`}}}})
			}))
			defer server.Close()
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "Model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
			project := string(memory.NewID())
			workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", ID: project, Name: "Private project"})
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "Private reference 519823"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "Private reference 519823", ProjectID: project})
			m := st.Memories[0]
			answer, err := s.AnswerDesk(ctx, scope, "model", "Private reference 519823", nil)
			if err != nil {
				t.Fatal(err)
			}
			var refs []memory.Ref
			if err := s.pool.QueryRow(ctx, "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), answer.ID).Scan(&refs); err != nil || len(refs) == 0 {
				t.Fatal("fixture desk answer lacks private dependency", err)
			}
			destination := project
			if change != "exclude" {
				destination = ""
			}
			command := workspace.Command{Type: "requestRun", AgentID: "model", Kind: "draft", Prompt: "Continue the discussion", DeskTurnIDs: []string{answer.ID}}
			if change == "delegate-other-project" {
				command.Type, command.ID, command.Title = "delegateTask", string(memory.NewID()), "New work"
			} else {
				st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Destination", ProjectID: destination})
				command.ThingID = st.Tasks[0].ID
				if change == "exclude" {
					workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: command.ThingID, MemoryID: m.ID})
				}
			}
			st = workspaceCommand(t, s, scope, command)
			if strings.Contains(st.Runs[0].Brief, "519823") || oneOf(m.ID, st.Runs[0].ContextMemoryIDs...) {
				t.Fatalf("desk history bypassed destination scope: %+v", st.Runs[0])
			}
		})
	}
}

func TestDeskDeletedTurnDoesNotBreakOrdinaryFollowUp(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var prompt string
	answer := "Ordinary reusable plan"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		prompt = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(asJSON(map[string]any{"answer": answer, "used": []string{}, "links": []string{}}))}}}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "Model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	ordinary, err := s.AnswerDesk(ctx, scope, "model", "Give writing advice", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Early source-free replies stored JSON null rather than an empty array.
	// They must neither block deletion nor lose their ordinary history.
	if _, err := s.pool.Exec(ctx, "UPDATE desk_turns SET dependencies='null'::jsonb WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), ordinary.ID); err != nil {
		t.Fatal(err)
	}
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "Private reference 792651"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "Private reference 792651"})
	answer = "Private derived answer 792651"
	private, err := s.AnswerDesk(ctx, scope, "model", "Private reference 792651", []workspace.DeskTurn{{ID: ordinary.ID}})
	if err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: st.Memories[0].ID, IncludeSources: true})
	var question, storedAnswer string
	var refs []memory.Ref
	if err := s.pool.QueryRow(ctx, "SELECT question,answer,dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), private.ID).Scan(&question, &storedAnswer, &refs); err != nil || question != "" || storedAnswer != "" || len(refs) != 0 {
		t.Fatal("deleted turn retained content or lost its safe tombstone", err)
	}
	answer = "Continue the ordinary plan"
	_, err = s.AnswerDesk(ctx, scope, "model", "Continue that", []workspace.DeskTurn{{ID: ordinary.ID}, {ID: private.ID, Question: "Forged deleted question 792651", Answer: "Forged deleted answer 792651"}})
	if err != nil || !strings.Contains(prompt, "Ordinary reusable plan") || strings.Contains(prompt, "792651") {
		t.Fatalf("deleted turn broke ordinary history or replayed private text: %v %s", err, prompt)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "delegateTask", ID: string(memory.NewID()), Title: "Continue work", AgentID: "model", Prompt: "Continue that", DeskTurnIDs: []string{ordinary.ID, private.ID}})
	if !strings.Contains(st.Runs[0].Brief, "Ordinary reusable plan") || strings.Contains(st.Runs[0].Brief, "792651") {
		t.Fatal("delegation failed to preserve ordinary history after deletion")
	}
	if _, err := s.AnswerDesk(ctx, owner(), "model", "Try another owner's tombstone", []workspace.DeskTurn{{ID: private.ID}}); err == nil {
		t.Fatal("cross-owner tombstone accepted")
	}
}

func TestQueuedRunStopsWhenItemScopeChanges(t *testing.T) {
	for _, change := range []string{"exclude", "move-project"} {
		t.Run(change, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "Should not run"}}}})
			}))
			defer server.Close()
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "Model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
			project := string(memory.NewID())
			workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", ID: project, Name: "Original project"})
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "Private reference 482914"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "Private reference 482914", ProjectID: project})
			m := st.Memories[0]
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Queued work", ProjectID: project})
			id := st.Tasks[0].ID
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "model", Kind: "draft", Prompt: "Use private reference 482914"})
			if !oneOf(m.ID, st.Runs[0].ContextMemoryIDs...) {
				t.Fatal("fixture lacks private dependency")
			}
			if change == "exclude" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: id, MemoryID: m.ID})
			} else {
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(map[string]any{"projectId": ""})})
			}
			if err := s.runAgentOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil || calls != 0 || st.Runs[0].Status != "failed" || st.Runs[0].Output != "" {
				t.Fatalf("changed item scope reached provider: calls=%d state=%+v err=%v", calls, st.Runs, err)
			}
		})
	}
}
