package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// V3 runs the actual Telegram adapter and the same PostgreSQL DeskTurn used by
// the web. Both model and Telegram network endpoints are local fake servers.
func TestM1V3TelegramImageUsesSharedDeskTurn(t *testing.T) {
	for _, caption := range []string{"帮我记一下", ""} {
		t.Run(fmt.Sprint(caption != ""), func(t *testing.T) {
			s := integrationStore(t)
			p, _, b := fixture(t)
			p.store = s
			var visions, secretaries atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				content := ""
				if len(body.Messages) > 1 && body.Messages[1].Content[0] == '[' {
					visions.Add(1)
					content = "缴费通知：10 月 15 日前交物业费 800 元"
				} else {
					secretaries.Add(1)
					var prompt string
					_ = json.Unmarshal(body.Messages[1].Content, &prompt)
					if !strings.Contains(prompt, "缴费通知：10 月 15 日前交物业费 800 元") {
						t.Error("secretary did not receive image", prompt)
					}
					content = fmt.Sprintf(`{"reply":"看到了缴费通知，已安排。","actions":[{"op":"create_task","title":"交物业费 800 元","due":"%d-10-15","remind":"none"}]}`, time.Now().Year()+1)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
			}))
			defer model.Close()
			s.SetModels(&ai.Registry{HTTP: model.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "Synthetic model", Protocol: "openai", BaseURL: model.URL, Model: "fake", MaxOutput: 100, CostMode: "free"}}}})
			photo := textUpdate(1, "")
			photo.Message.Photo = []file{{ID: "photo", MIME: "image/png"}}
			photo.Message.Caption = caption
			b.enqueue(photo)
			b.failSend = true
			if err := p.step(context.Background(), "fixture", "123"); err == nil {
				t.Fatal("delivery failure hidden")
			}
			b.failSend = false
			step(t, p)
			if visions.Load() != 1 || secretaries.Load() != 1 {
				t.Fatal("delivery retry reprocessed image", visions.Load(), secretaries.Load())
			}
			out, err := s.DeskTurnByRequest(context.Background(), p.scope, requestID(botChatMust(t, p), "message:1"))
			if err != nil || len(out.State.Tasks) != 1 || len(out.Turn.Receipts) != 2 {
				t.Fatal(out, err)
			}
			var receipt workspace.DeskReceipt
			for _, r := range out.Turn.Receipts {
				if r.Op == "attachment" {
					receipt = r
				}
			}
			if receipt.SourceID == nil || receipt.ActionID == nil || receipt.Text != "存了一张图片" {
				t.Fatal(receipt)
			}
			original, _, _, err := s.OpenAttachment(context.Background(), p.scope, memory.ID(*receipt.SourceID), receipt.SourceVersion)
			if err != nil {
				t.Fatal(err)
			}
			original.Close()
			sent := b.of("sendMessage")
			text := decode[string](sent[len(sent)-1].body["text"])
			if !strings.Contains(text, "缴费通知") || !strings.Contains(text, "存了一张图片") || strings.Contains(text, "收到，已存进资料") {
				t.Fatal(text)
			}
			if st, err := s.Undo(context.Background(), p.scope, *receipt.ActionID); err != nil || len(st.Tasks) != 1 {
				t.Fatal("cross-channel image undo affected task", err)
			}
		})
	}
}
func botChatMust(t *testing.T, p *poller) string {
	t.Helper()
	c, err := p.settings.Read()
	if err != nil {
		t.Fatal(err)
	}
	return botChat(c)
}
