package postgres

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestB4UnbuiltSecretarySelfchecksSameLegacyContext(t *testing.T) {
	for _, mode := range []string{"keyword", "missing", "medium", "heavy", "failure", "timeout", "extra_action"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			var calls atomic.Int32
			var draftPrompt string
			var promptMu sync.Mutex
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				_, prompt := b4RequestBody(t, r)
				if calls.Add(1) == 1 {
					promptMu.Lock()
					draftPrompt = prompt
					promptMu.Unlock()
					if !strings.Contains(prompt, "虚构旧资料的汇报每页三行") || strings.Contains(prompt, "相关的现状卡") {
						t.Error("legacy retrieval changed", prompt)
					}
					time.Sleep(100 * time.Millisecond)
					secretaryModelReply(w, map[string]any{"reply": "虚构初稿", "missingKeyInfo": mode == "missing", "actions": []map[string]any{{"op": "create_task", "title": "虚构原任务"}}})
					return
				}
				promptMu.Lock()
				samePrompt := strings.HasPrefix(prompt, draftPrompt+"\n待自查的草稿：\n")
				promptMu.Unlock()
				if !samePrompt {
					t.Error("selfcheck received different context")
				}
				switch mode {
				case "failure":
					http.Error(w, "fictional selfcheck failure", 503)
				case "timeout":
					select {
					case <-r.Context().Done():
					case <-time.After(time.Second):
					}
				default:
					secretaryModelReply(w, `{"reply":"虚构修订","actions":[{"op":"create_task","title":"虚构原任务"},{"op":"create_task","title":"虚构不许新增"}]}`)
				}
			})
			b4Memory(t, s, scope, "虚构旧资料的汇报每页三行")
			ctx, text := context.Background(), "处理虚构旧资料的汇报"
			if mode == "keyword" {
				text = "仔细" + text
			} else if mode != "missing" {
				tier := "medium"
				if mode == "heavy" {
					tier = "heavy"
				}
				ctx = WithMemoryTier(ctx, tier)
			}
			out, err := s.DeskTurn(ctx, scope, turnRequest(text))
			wantReply := "虚构修订"
			if mode == "failure" || mode == "timeout" {
				wantReply = "虚构初稿"
			}
			if err != nil || calls.Load() != 2 || out.Turn.Reply != wantReply || len(out.State.Tasks) != 1 || out.State.Tasks[0].Title != "虚构原任务" {
				t.Fatal(out.Turn, out.State.Tasks, calls.Load(), err)
			}
			// Timed-out accounting is independent of the response deadline.
			deadline := time.Now().Add(2 * time.Second)
			for {
				var good, bad, readers int
				err = s.pool.QueryRow(context.Background(), `SELECT count(*) FILTER(WHERE purpose IN('secretary','selfcheck') AND tier='medium'),count(*) FILTER(WHERE tier!='medium'),count(*) FILTER(WHERE purpose='reader') FROM model_usage WHERE owner_id=$1`, scope.OwnerID).Scan(&good, &bad, &readers)
				if err != nil {
					t.Fatal(err)
				}
				if good == 2 && bad == 0 && readers == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("usage", good, bad, readers)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

func TestB4UnbuiltDeputyUsesMediumExceptExplicitLight(t *testing.T) {
	for _, mode := range []string{"default", "light", "medium", "heavy", "queued_heavy"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			var calls atomic.Int32
			var draftPrompt string
			var promptMu sync.Mutex
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				sys, prompt := b4RequestBody(t, r)
				if strings.Contains(sys, "只读的记忆读者") || strings.Contains(prompt, "分组目录") {
					t.Error("unbuilt deputy called a reader")
				}
				if calls.Add(1) == 1 {
					promptMu.Lock()
					draftPrompt = prompt
					promptMu.Unlock()
					time.Sleep(100 * time.Millisecond)
					secretaryModelReply(w, "虚构副手初稿")
					return
				}
				promptMu.Lock()
				samePrompt := strings.HasPrefix(prompt, draftPrompt+"\n待自查草稿：\n")
				promptMu.Unlock()
				if !samePrompt {
					t.Error("deputy selfcheck received different context")
				}
				secretaryModelReply(w, "虚构副手修订")
			})
			m := b4Memory(t, s, scope, "虚构旧资料的汇报每页三行")
			if mode == "queued_heavy" {
				b4Card(t, s, scope, "self:goal", "self", "虚构旧资料", m)
			}
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构旧资料汇报"})
			ctx := context.Background()
			if mode != "default" {
				tier := mode
				if mode == "queued_heavy" {
					tier = "heavy"
				}
				ctx = WithMemoryTier(ctx, tier)
			}
			_, err := s.Execute(ctx, scope, workspace.Command{RequestID: string(memory.NewID()), ExpectedRevision: state.Revision, Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "plan", Prompt: "整理虚构旧资料汇报"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "queued_heavy" {
				b4Exec(t, s, "DELETE FROM status_cards WHERE owner_id=$1", scope.OwnerID)
			}
			if err = s.runAgentOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			state, err = s.Snapshot(context.Background(), scope)
			wantCalls, wantTier, wantText := int32(2), "medium", "虚构副手修订"
			if mode == "light" {
				wantCalls, wantTier, wantText = 1, "light", "虚构副手初稿"
			}
			if err != nil || len(state.Runs) != 1 || state.Runs[0].Status != "done" || state.Runs[0].Output != wantText || state.Runs[0].MemoryTier != wantTier || calls.Load() != wantCalls {
				t.Fatal(state.Runs, calls.Load(), err)
			}
			var got int
			if err = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND tier=$2 AND purpose IN('deputy','selfcheck')", scope.OwnerID, wantTier).Scan(&got); err != nil || got != int(wantCalls) {
				t.Fatal(fmt.Sprint("usage ", got), err)
			}
		})
	}
}
