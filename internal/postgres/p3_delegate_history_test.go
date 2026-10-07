package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The model's delegate prompt deliberately contains none of the project words.
// Both a new task and an existing task must obtain them from server history.
func TestP3SecretaryDelegateHistoryRetrievesMemory(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			delegate := false
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				if !delegate {
					secretaryModelReply(w, `{"reply":"收到。","actions":[]}`)
					return
				}
				ref := "new"
				if existing {
					ref = "THIS"
				}
				secretaryModelReply(w, fmt.Sprintf(`{"reply":"已交给副手。","actions":[{"op":"delegate","ref":%q,"title":"写方案","kind":"plan","prompt":"交给副手写方案"}]}`, ref))
			})
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "AuroraCedar 项目的验收口令为松果灯塔，方案必须安排无障碍通道。"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "AuroraCedar 项目的验收口令为松果灯塔，方案必须安排无障碍通道。"})
			relevant := st.Memories[0].ID
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "写方案"})
			task := st.Tasks[0].ID
			var conversation *string
			ids := []string{}
			texts := []string{"我们聊 AuroraCedar 项目的方案。", "面向居民，安排两天。", "先列目标和验收方式。"}
			for _, text := range texts {
				req := turnRequest(text)
				req.ConversationID = conversation
				if existing {
					req.ThingID = &task
				}
				out := mustTurn(t, s, scope, req)
				conversation = &out.ConversationID
				ids = append(ids, out.Turn.ID)
			}
			delegate = true
			req := turnRequest("交给副手写方案")
			req.ConversationID = conversation
			if existing {
				req.ThingID = &task
			}
			out := mustTurn(t, s, scope, req)
			if len(out.State.Runs) != 1 {
				t.Fatalf("delegate failed: %+v", out.Turn)
			}
			run := out.State.Runs[0]
			for _, text := range texts {
				if !strings.Contains(run.Brief, text) {
					t.Fatalf("history missing %q: %s", text, run.Brief)
				}
			}
			if !strings.Contains(run.Brief, "松果灯塔") || !oneOf(relevant, run.ContextMemoryIDs...) {
				t.Fatalf("history query did not retrieve memory: %+v", run)
			}
			manual := workspaceCommand(t, s, scope, workspace.Command{Type: "delegateTask", ID: string(memory.NewID()), Title: "手动写方案", AgentID: "model", Kind: "plan", Prompt: "接着刚才聊的做", DeskTurnIDs: ids})
			for _, r := range manual.Runs {
				if r.ID != run.ID && (!strings.Contains(r.Brief, "松果灯塔") || !oneOf(relevant, r.ContextMemoryIDs...)) {
					t.Fatalf("manual history retrieval lost: %+v", r)
				}
			}

		})
	}
}

func TestP3HistoryQueryKeepsRecentUTF8(t *testing.T) {
	history := []storedDeskContext{{Question: strings.Repeat("旧", 5000)}, {Question: "AuroraCedar", Answer: "最近的决定"}}
	query := tail(runHistoryQuery("旧事项", history, "写方案"), delegateContextBytes)
	if len(query) > delegateContextBytes || !strings.Contains(query, "AuroraCedar") || !strings.HasSuffix(query, "写方案") {
		t.Fatal(query)
	}
}

// A recording HTTP bridge forwards the actual product prompts to our own
// Codex login. It changes neither instructions nor the hand-written schema.
func TestP3LiveDelegateHistory(t *testing.T) {
	home := os.Getenv("PCAS_P3_CODEX_HOME")
	if home == "" {
		t.Skip("own login opt-in")
	}
	s := testStore(t)
	scope := owner()
	codex, err := ai.NewCodex("", home)
	if err != nil {
		t.Fatal(err)
	}
	defer codex.Close()
	real, err := ai.Load("", codex)
	if err != nil {
		t.Fatal(err)
	}
	type trace struct {
		System     string `json:"system"`
		Input      string `json:"input"`
		Output     string `json:"output"`
		Error      string `json:"error,omitempty"`
		DurationMS int64  `json:"durationMs"`
	}
	traces := []trace{}
	var mu sync.Mutex
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var system, prompt string
		for _, m := range body.Messages {
			if m.Role == "system" {
				system = m.Content
			} else {
				prompt = m.Content
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		started := time.Now()
		var result ai.Result
		var err error
		if system == secretaryInstructions {
			result, err = real.GenerateWithSearchSchema(ctx, "chatgpt", system, prompt, secretaryOutputSchema)
		} else {
			result, err = real.GenerateWithSearch(ctx, "chatgpt", system, prompt)
		}
		tr := trace{System: system, Input: prompt, Output: result.Text, DurationMS: time.Since(started).Milliseconds()}
		if err != nil {
			tr.Error = err.Error()
		}
		mu.Lock()
		traces = append(traces, tr)
		mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		secretaryModelReply(w, result.Text)
	})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "AuroraCedar 虚构社区项目的验收口令为松果灯塔，方案必须安排无障碍通道。"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "AuroraCedar 虚构社区项目的验收口令为松果灯塔，方案必须安排无障碍通道。"})
	relevant := st.Memories[0].ID
	var conversation *string
	var out workspace.DeskTurnResponse
	texts := []string{"我们先聊 AuroraCedar 虚构社区项目。先不用写方案。", "面向居民，活动安排两天。先记住这个讨论背景。", "方案应列目标、活动流程和验收方式。先不生成。", "交给副手写方案"}
	defer func() {
		if path := os.Getenv("PCAS_P3_RAW_REPORT"); path != "" {
			data, _ := json.MarshalIndent(map[string]any{"turns": texts, "calls": traces, "runs": out.State.Runs}, "", "  ")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	for _, text := range texts {
		req := turnRequest(text)
		req.ConversationID = conversation
		out = mustTurn(t, s, scope, req)
		conversation = &out.ConversationID
	}
	if len(out.State.Runs) != 1 {
		t.Fatalf("live delegation not created: %+v", out.Turn)
	}
	run := out.State.Runs[0]
	for _, text := range texts[:3] {
		if !strings.Contains(run.Brief, text) {
			t.Fatalf("missing history %q", text)
		}
	}
	if !oneOf(relevant, run.ContextMemoryIDs...) {
		t.Fatal("relevant memory missing")
	}
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	out.State, err = s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if out.State.Runs[0].Status != "done" || out.State.Runs[0].Output == "" {
		t.Fatalf("deputy failed: %+v", out.State.Runs[0])
	}
}
