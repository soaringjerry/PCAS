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

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestDeskAnswerCitesOnlyWhatItWasShown(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	var prompt string
	var used []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		prompt = string(body)
		content := string(asJSON(map[string]any{"answer": "周五交季度报告。", "used": used, "links": []string{"javascript:alert(1)", "https://example.com/a", "//evil", "https://u:p@example.com"}}))
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n" + content + "\n```"}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 10}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "模型", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 2}}}})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "季度报告周五交"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "季度报告周五交"})
	mem := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "季度报告的密码是 hunter2"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "季度报告的密码是 hunter2"})
	var hidden workspace.Memory
	for _, m := range st.Memories {
		if strings.Contains(m.Text, "hunter2") {
			hidden = m
		}
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: hidden.ID, AgentIDs: []string{}})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "整理发票"})

	used = []string{mem.ID, hidden.ID, "not-sent"}
	out, err := s.AnswerDesk(ctx, scope, "model", "季度报告什么时候交？", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Answer != "周五交季度报告。" || len(out.Used) != 1 || string(out.Used[0].Ref.ID) != mem.ID || len(out.Links) != 1 || out.Links[0] != "https://example.com/a" {
		t.Fatalf("answer %+v", out)
	}
	if !strings.Contains(prompt, "季度报告什么时候交") || !strings.Contains(prompt, "整理发票") || strings.Contains(prompt, "hunter2") {
		t.Fatalf("prompt leaked or missed context: %s", prompt)
	}
	// A follow-up carries the earlier exchange and the saved city.
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"city": " 上海 "})})
	if _, err := s.AnswerDesk(ctx, scope, "model", "那明天呢", []workspace.DeskTurn{{Question: "今天天气怎么样", Answer: "上海今天多云。"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "用户所在城市：上海") || !strings.Contains(prompt, "上海今天多云") || !strings.Contains(prompt, "那明天呢") {
		t.Fatalf("follow-up context missing: %s", prompt)
	}
	current, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"city": "a\nb"}), RequestID: string(memory.NewID()), ExpectedRevision: current.Revision}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("multi-line city accepted")
	}
	if _, err := s.AnswerDesk(ctx, scope, "manual", "x", nil); err == nil {
		t.Fatal("manual agent answered")
	}
	if _, err := s.AnswerDesk(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "external", IsOwner: false}, "model", "x", nil); err == nil {
		t.Fatal("non-owner answered")
	}
}
