package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func stabilizationSecretaryModel(t *testing.T, s *postgres.Store) *atomic.Int32 {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"reply":"安排好了","actions":[{"op":"create_task","title":"Telegram事项"}]}`}}}})
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "测试秘书", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 1000, CostMode: "free"}}}})
	return calls
}

func stabilizationSecretarySnapshot(t *testing.T, s *postgres.Store, p *poller, want int) workspace.State {
	t.Helper()
	st, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || len(st.Tasks) != want {
		t.Fatalf("persisted task count: got %d want %d error %v", len(st.Tasks), want, err)
	}
	return st
}

func TestStabilizationTelegramT1_DuplicateUpdateExecutesOnce(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	calls := stabilizationSecretaryModel(t, s)
	u := textUpdate(10, "同一条安排")
	b.enqueue(u, u)
	step(t, p)
	stabilizationSecretarySnapshot(t, s, p, 1)
	fresh := *p
	fresh.state = state{}
	step(t, &fresh)
	stabilizationSecretarySnapshot(t, s, &fresh, 1)
	if calls.Load() != 1 || len(b.of("sendMessage")) != 1 {
		t.Fatal("duplicate update re-executed or redelivered", calls.Load(), len(b.of("sendMessage")))
	}
	c, err := p.settings.Read()
	if err != nil || c.TelegramOffset != 11 {
		t.Fatal("progress not durable", c.TelegramOffset, err)
	}
}

func TestStabilizationTelegramT1_DuplicateUndoCallbackExecutesOnce(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	calls := stabilizationSecretaryModel(t, s)
	b.enqueue(textUpdate(1, "可撤销安排"))
	step(t, p)
	rows := decode[keyboard](b.of("sendMessage")[0].body["reply_markup"]).Rows
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatal("fixture did not expose undo button")
	}
	u := callbackUpdate(2, rows[0][0].Data, 101)
	b.enqueue(u, u)
	step(t, p)
	stabilizationSecretarySnapshot(t, s, p, 0)
	fresh := *p
	fresh.state = state{}
	step(t, &fresh)
	if calls.Load() != 1 || len(b.of("answerCallbackQuery")) != 1 || decode[string](b.of("answerCallbackQuery")[0].body["text"]) != "已撤销" {
		t.Fatal("duplicate callback executed or acknowledged twice")
	}
}

func TestStabilizationS6_TelegramOptionContinuesSameObject(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `{"actions":[{"op":"create_task","title":"S6渠道会议"}],"ask":{"question":"地点？","options":["办公室","线上"]}}`
		if calls.Add(1) == 2 {
			content = `{"actions":[{"op":"update","ref":"R1","set":{"notesAppend":"办公室"}}]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "测试秘书", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 1000, CostMode: "free"}}}})
	b.enqueue(textUpdate(1, "安排会议"))
	step(t, p)
	first := stabilizationSecretarySnapshot(t, s, p, 1)
	rows := decode[keyboard](b.of("sendMessage")[0].body["reply_markup"]).Rows
	found := false
	for _, row := range rows {
		for _, button := range row {
			if button.Data == "a:0" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("option button missing")
	}
	b.enqueue(callbackUpdate(2, "a:0", 101))
	step(t, p)
	next := stabilizationSecretarySnapshot(t, s, p, 1)
	if calls.Load() != 2 || next.Tasks[0].ID != first.Tasks[0].ID || next.Tasks[0].Notes != "办公室" {
		t.Fatal("callback did not continue previous object")
	}
	b.enqueue(callbackUpdate(3, "a:1", 101))
	step(t, p)
	if calls.Load() != 2 {
		t.Fatal("answered option was reused")
	}
}

func TestStabilizationTelegramT2_NewBotResetsProgressAndConversation(t *testing.T) {
	for _, oldID := range []int64{100, 1} {
		t.Run(map[int64]string{100: "lower_update_id", 1: "colliding_message_id"}[oldID], func(t *testing.T) {
			s := integrationStore(t)
			p, _, b := fixture(t)
			p.store = s
			calls := stabilizationSecretaryModel(t, s)
			b.enqueue(textUpdate(oldID, "旧bot安排"))
			step(t, p)
			old, err := p.settings.Read()
			if err != nil || old.TelegramConversation == "" {
				t.Fatal("old conversation missing", err)
			}
			if err = p.settings.SaveTelegram("second-fixture", "123"); err != nil {
				t.Fatal(err)
			}
			reset, err := p.settings.Read()
			if err != nil || reset.TelegramOffset != 0 || reset.TelegramConversation != "" {
				t.Fatal("configuration did not reset", err)
			}
			// An old poller must be unable to write progress over the new bot.
			if err = p.settings.UpdateTelegramProgress("fixture", "123", 999, old.TelegramConversation); err == nil {
				t.Fatal("stale poller overwrote progress")
			}
			fresh := *p
			fresh.state = state{}
			b.enqueue(textUpdate(1, "新bot安排"))
			if err = fresh.step(context.Background(), "second-fixture", "123"); err != nil {
				t.Fatal(err)
			}
			next, err := p.settings.Read()
			if err != nil || next.TelegramOffset != 2 || next.TelegramConversation == "" || next.TelegramConversation == old.TelegramConversation {
				t.Fatal("new bot did not acquire independent conversation", next.TelegramOffset, err)
			}
			stabilizationSecretarySnapshot(t, s, &fresh, 2)
			if calls.Load() != 2 {
				t.Fatal("new bot message was suppressed", calls.Load())
			}
		})
	}
}

func TestStabilizationTelegramT3_UnauthorizedAndForgedCallbacksHaveNoEffects(t *testing.T) {
	for _, mode := range []string{"other_chat", "group", "spoofed_sender", "malformed_callback", "forged_receipt"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationStore(t)
			p, _, b := fixture(t)
			p.store = s
			calls := stabilizationSecretaryModel(t, s)
			b.enqueue(textUpdate(1, "合法安排"))
			step(t, p)
			st := stabilizationSecretarySnapshot(t, s, p, 1)
			turns, err := s.DeskTurns(context.Background(), p.scope, (func() string { c, _ := p.settings.Read(); return c.TelegramConversation })())
			if err != nil || len(turns.Turns) != 1 || len(turns.Turns[0].Receipts) != 1 {
				t.Fatal("fixture missing receipt", err)
			}
			action := *turns.Turns[0].Receipts[0].ActionID
			u := textUpdate(2, "secret-unauthorized-T3")
			switch mode {
			case "other_chat":
				u.Message.Chat.ID = 999
			case "group":
				u.Message.Chat.Type = "group"
			case "spoofed_sender":
				u = callbackUpdate(2, "u:"+action, 101)
				u.Callback.From.ID = 999
			case "malformed_callback":
				u = callbackUpdate(2, "u:not-a-uuid", 101)
			case "forged_receipt":
				u = callbackUpdate(2, "u:"+action, 999)
				u.Callback.Message.Text = "伪造的回执"
			}
			b.enqueue(u)
			step(t, p)
			after := stabilizationSecretarySnapshot(t, s, p, 1)
			if after.Tasks[0].ID != st.Tasks[0].ID || calls.Load() != 1 {
				t.Fatal("untrusted update changed workspace")
			}
			data, err := os.ReadFile(p.settings.Path)
			if err != nil || strings.Contains(string(data), "secret-unauthorized-T3") {
				t.Fatal("unauthorized content persisted", err)
			}
			if mode == "other_chat" || mode == "group" || mode == "spoofed_sender" {
				if len(b.of("sendMessage")) != 1 || len(b.of("answerCallbackQuery")) != 0 {
					t.Fatal("unauthorized update received response")
				}
			}
		})
	}
}

type stabilizationSecretaryLongStore struct{ *fakeStore }

func (s stabilizationSecretaryLongStore) DeskTurn(ctx context.Context, scope memory.Scope, req workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	out, err := s.fakeStore.DeskTurn(ctx, scope, req)
	out.Turn.Reply = strings.Repeat("长回复", 3000)
	return out, err
}

func TestStabilizationTelegramT4_LongReplyFitsBotLimit(t *testing.T) {
	p, s, b := fixture(t)
	p.store = stabilizationSecretaryLongStore{s}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			var in struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(body, &in)
			if !utf8.ValidString(in.Text) || utf8.RuneCountInString(in.Text) > 4096 {
				t.Errorf("outbound bot message exceeded limit: %d", utf8.RuneCountInString(in.Text))
				http.Error(w, "too long", 400)
				return
			}
		}
		b.serve(w, r)
	}))
	defer server.Close()
	p.api = botAPI{baseURL: server.URL, client: server.Client()}
	b.enqueue(textUpdate(1, "给我长回复"))
	step(t, p)
	if len(s.requests) != 1 || len(b.of("sendMessage")) == 0 {
		t.Fatal("long reply was lost")
	}
}

func TestStabilizationTelegramT5_UnconfiguredTranscriptionPreservesAudio(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	u := textUpdate(1, "")
	u.Message.Voice = &file{ID: "voice"}
	b.enqueue(u)
	step(t, p)
	st, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || len(st.Sources) != 1 || len(st.Tasks) != 0 {
		t.Fatal("audio not preserved or false task created", err)
	}
	f, _, media, err := s.OpenAttachment(context.Background(), p.scope, memory.ID(st.Sources[0].ID), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if media != "audio/ogg" || string(data) != "audio-bytes" {
		t.Fatal("wrong retained original audio")
	}
	text := decode[string](b.of("sendMessage")[0].body["text"])
	if !strings.Contains(text, "配置语音转写") {
		t.Fatal("missing configuration advice", text)
	}
	fresh := *p
	fresh.state = state{}
	step(t, &fresh)
	after, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || len(after.Sources) != 1 {
		t.Fatal("voice redelivery duplicated source", err)
	}
}

func TestStabilizationTelegramT6_RestartAfterCommittedTurnBeforeDelivery(t *testing.T) {
	for _, mode := range []string{"text", "voice"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationStore(t)
			p, _, b := fixture(t)
			p.store = s
			calls := stabilizationSecretaryModel(t, s)
			u := textUpdate(10, "重启中途安排")
			if mode == "voice" {
				u.Message.Text = ""
				u.Message.Voice = &file{ID: "voice"}
				p.models = &changingTranscriber{}
			}
			b.enqueue(u)
			b.failSend = true
			if err := p.step(context.Background(), "fixture", "123"); err == nil {
				t.Fatal("delivery failure hidden")
			}
			stabilizationSecretarySnapshot(t, s, p, 1)
			before, err := p.settings.Read()
			if err != nil || before.TelegramOffset != 0 {
				t.Fatal("unacknowledged update advanced", err)
			}
			fresh := *p
			fresh.state = state{} // discard every transient cache, as a restart does
			b.failSend = false
			step(t, &fresh)
			if mode == "voice" && p.models.(*changingTranscriber).calls != 1 {
				t.Errorf("restart transcribed committed input again: calls=%d want=1", p.models.(*changingTranscriber).calls)
			}
			stabilizationSecretarySnapshot(t, s, &fresh, 1)
			after, err := p.settings.Read()
			if err != nil || after.TelegramOffset != 11 || after.TelegramConversation != before.TelegramConversation || calls.Load() != 1 {
				t.Fatal("restart lost progress, conversation, or executed again", err, calls.Load())
			}
			sent := b.of("sendMessage")
			last := decode[string](sent[len(sent)-1].body["text"])
			if !strings.Contains(last, "已建：Telegram事项") {
				t.Error("restart did not recover stored receipt", last)
			}
			b.enqueue(textUpdate(11, "之后的消息"))
			step(t, &fresh)
			stabilizationSecretarySnapshot(t, s, &fresh, 2)
		})
	}
}

// Rotation changes a credential, not the bot's identity. Returning to an older
// bot must also recover its own receipt rather than execute that input again.
func TestTelegramBotIdentitySurvivesRotationAndReturn(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	calls := stabilizationSecretaryModel(t, s)
	b.enqueue(textUpdate(1, "身份一的安排"))
	step(t, p)
	for _, token := range []string{"rotated-fixture", "second-fixture", "fixture"} {
		if err := p.settings.SaveTelegram(token, "123"); err != nil {
			t.Fatal(err)
		}
		fresh := *p
		fresh.state = state{}
		text := "身份一的安排"
		if token == "second-fixture" {
			text = "身份二的安排"
		}
		b.enqueue(textUpdate(1, text))
		if err := fresh.step(context.Background(), token, "123"); err != nil {
			t.Fatal(err)
		}
	}
	stabilizationSecretarySnapshot(t, s, p, 2)
	if calls.Load() != 2 {
		t.Fatal("credential rotation or returning bot repeated action", calls.Load())
	}
}

func TestTelegramUndoBindingSurvivesRestartAndRejectsAnotherTurn(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	calls := stabilizationSecretaryModel(t, s)
	b.enqueue(textUpdate(1, "第一项"), textUpdate(2, "第二项"))
	step(t, p)
	sent := b.of("sendMessage")
	first := decode[keyboard](sent[0].body["reply_markup"]).Rows[0][0].Data
	second := decode[keyboard](sent[1].body["reply_markup"]).Rows[0][0].Data
	fresh := *p
	fresh.state = state{}
	b.enqueue(callbackUpdate(3, second, 101))
	step(t, &fresh)
	stabilizationSecretarySnapshot(t, s, p, 2)
	b.enqueue(callbackUpdate(4, first, 101))
	step(t, &fresh)
	stabilizationSecretarySnapshot(t, s, p, 1)
	if calls.Load() != 2 {
		t.Fatal("callback generated a model turn")
	}
}

func TestTelegramForgedOptionCannotRecoverFromCopiedText(t *testing.T) {
	p, s, b := fixture(t)
	s.ask = &workspace.DeskAsk{Question: "选地点", Options: []string{"办公室"}}
	b.enqueue(textUpdate(1, "安排会议"))
	step(t, p)
	q := callbackUpdate(2, "a:0", 999)
	q.Callback.Message.Text = decode[string](b.of("sendMessage")[0].body["text"])
	fresh := *p
	fresh.state = state{}
	b.enqueue(q)
	step(t, &fresh)
	if len(s.requests) != 1 {
		t.Fatal("copied text recovered an unsent option message")
	}
}

func TestTelegramLegacyRecoveryRequiresUnchangedConversationAnchor(t *testing.T) {
	for _, mode := range []string{"text", "voice", "different_conversation", "changed_bot"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationStore(t)
			p, _, b := fixture(t)
			p.store = s
			calls := stabilizationSecretaryModel(t, s)
			conversation := string(memory.NewID())
			if err := p.settings.UpdateTelegramProgress("fixture", "123", 0, conversation); err != nil {
				t.Fatal(err)
			}
			legacyConversation := conversation
			if mode == "different_conversation" {
				legacyConversation = string(memory.NewID())
			}
			legacyID := requestID("123", "message:10")
			original, err := s.DeskTurn(context.Background(), p.scope, workspace.DeskTurnRequest{RequestID: legacyID, ConversationID: &legacyConversation, Text: "升级前已提交的安排"})
			if err != nil {
				t.Fatal(err)
			}
			token := "fixture"
			if mode == "changed_bot" {
				token = "second-fixture"
				if err := p.settings.SaveTelegram(token, "123"); err != nil {
					t.Fatal(err)
				}
			}
			u := textUpdate(10, "升级前已提交的安排")
			transcript := &changingTranscriber{}
			if mode == "voice" {
				u.Message.Text = ""
				u.Message.Voice = &file{ID: "voice"}
				p.models = transcript
			}
			b.enqueue(u)
			if err = p.step(context.Background(), token, "123"); err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "different_conversation" || mode == "changed_bot" {
				want = 2
			}
			stabilizationSecretarySnapshot(t, s, p, want)
			if calls.Load() != int32(want) || transcript.calls != 0 {
				t.Fatal("unsafe migration or repeated voice processing", calls.Load(), transcript.calls)
			}
			c, err := p.settings.Read()
			if err != nil || len(c.TelegramReceipts) != 1 {
				t.Fatal("migration missing sent association", err)
			}
			if want == 1 && (c.TelegramReceipts[0].RequestID != legacyID || c.TelegramReceipts[0].TurnID != original.Turn.ID) {
				t.Fatal("legacy receipt was replaced")
			}
			if want == 2 && c.TelegramReceipts[0].RequestID == legacyID {
				t.Fatal("foreign legacy request acquired binding")
			}
		})
	}
}

func TestTelegramCommittedVoiceRecoveryRespectsDeletionAndOwner(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	calls := stabilizationSecretaryModel(t, s)
	p.models = &changingTranscriber{}
	u := textUpdate(10, "")
	u.Message.Voice = &file{ID: "voice"}
	b.enqueue(u)
	b.failSend = true
	if err := p.step(context.Background(), "fixture", "123"); err == nil {
		t.Fatal("delivery failure hidden")
	}
	c, _ := p.settings.Read()
	id := requestID(botChat(c), "message:10")
	if _, err := s.DeskTurnByRequest(context.Background(), memory.Scope{OwnerID: p.scope.OwnerID}, id); err == nil {
		t.Fatal("non-owner could recover turn")
	}
	if _, err := s.DeskTurnByRequest(context.Background(), memory.Scope{OwnerID: memory.NewID(), PrincipalID: "telegram", IsOwner: true}, id); err != memory.ErrNotFound {
		t.Fatal("foreign owner read request", err)
	}
	stabilizationSecretarySnapshot(t, s, p, 1)
	// Resolve the already-ingested source through its public idempotent input.
	source, err := s.Ingest(context.Background(), p.scope, memory.IngestRequest{Connector: "desk", ExternalID: id, ExternalVersion: "1", Title: "秘书原话", Text: "明天下午三点开会", MediaType: "text/plain"})
	if err != nil || !source.Duplicate {
		t.Fatal("original source not idempotently resolvable", err)
	}
	if err := s.Delete(context.Background(), p.scope, memory.DeleteRequest{Targets: []memory.Ref{source.Ref}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.DeskTurnByRequest(context.Background(), p.scope, id)
	if err != nil || recovered.Turn.Text != "" || len(recovered.Turn.Cards) != 0 {
		t.Fatal("lookup restored deleted content", err)
	}
	fresh := *p
	fresh.state = state{}
	b.failSend = false
	step(t, &fresh)
	sent := b.of("sendMessage")
	last := decode[string](sent[len(sent)-1].body["text"])
	if strings.Contains(last, "明天下午三点开会") || strings.Contains(last, "🎤 听到") || strings.Contains(last, "已建：Telegram事项") {
		t.Fatal("deleted content redelivered", last)
	}
	if p.models.(*changingTranscriber).calls != 1 || calls.Load() != 1 {
		t.Fatal("deleted input regenerated")
	}
}

func TestTelegramSelectedTurnRecoveryAfterDeliveryFailure(t *testing.T) {
	p, s, b := fixture(t)
	s.ask = &workspace.DeskAsk{Question: "选地点", Options: []string{"办公室", "线上"}}
	b.enqueue(textUpdate(1, "安排会议"))
	step(t, p)
	s.ask = nil
	b.enqueue(callbackUpdate(2, "a:0", 101))
	b.failSend = true
	if err := p.step(context.Background(), "fixture", "123"); err == nil {
		t.Fatal("selected-turn delivery failure hidden")
	}
	if len(s.requests) != 2 {
		t.Fatal("selection did not commit")
	}
	fresh := *p
	fresh.state = state{}
	b.failSend = false
	step(t, &fresh)
	if len(s.requests) != 2 {
		t.Fatal("committed selection executed again")
	}
	sent := b.of("sendMessage")
	if !strings.Contains(decode[string](sent[len(sent)-1].body["text"]), "已建：开会") {
		t.Fatal("selection result not recovered")
	}
	b.enqueue(callbackUpdate(3, "a:1", 101))
	step(t, &fresh)
	if len(s.requests) != 2 || len(b.of("sendMessage")) != len(sent) {
		t.Fatal("delivered selection reused")
	}
}

func TestTelegramSameBotRotationKeepsSentUndoBinding(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	calls := stabilizationSecretaryModel(t, s)
	b.enqueue(textUpdate(1, "轮换凭据前的安排"))
	step(t, p)
	data := decode[keyboard](b.of("sendMessage")[0].body["reply_markup"]).Rows[0][0].Data
	if err := p.settings.SaveTelegram("rotated-fixture", "123"); err != nil {
		t.Fatal(err)
	}
	fresh := *p
	fresh.state = state{}
	b.enqueue(callbackUpdate(2, data, 101))
	if err := fresh.step(context.Background(), "rotated-fixture", "123"); err != nil {
		t.Fatal(err)
	}
	stabilizationSecretarySnapshot(t, s, p, 0)
	if calls.Load() != 1 || decode[string](b.of("answerCallbackQuery")[0].body["text"]) != "已撤销" {
		t.Fatal("rotation lost a genuine receipt")
	}
}

func TestTelegramUnconfirmedIdentityDoesNotProcessInput(t *testing.T) {
	p, s, b := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"id": 7001, "is_bot": false}})
			return
		}
		b.serve(w, r)
	}))
	defer server.Close()
	p.api = botAPI{baseURL: server.URL, client: server.Client()}
	b.enqueue(textUpdate(1, "身份未确认"))
	if err := p.step(context.Background(), "fixture", "123"); err == nil {
		t.Fatal("invalid getMe response accepted")
	}
	c, err := p.settings.Read()
	if err != nil || c.TelegramOffset != 0 || c.TelegramBotID != "" || len(s.requests) != 0 || len(b.of("getUpdates")) != 0 {
		t.Fatal("input processed without confirmed bot identity", err)
	}
}
