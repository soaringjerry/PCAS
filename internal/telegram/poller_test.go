package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type fakeStore struct {
	requests    []workspace.DeskTurnRequest
	turns       []workspace.SecretaryTurn
	undoIDs     []string
	undoErr     error
	attachments []memory.IngestRequest
	data        []string
	ask         *workspace.DeskAsk
	attachErr   error
	turnErr     error
}

func (*fakeStore) Snapshot(context.Context, memory.Scope) (workspace.State, error) {
	return workspace.State{}, nil
}

func (s *fakeStore) DeskTurn(_ context.Context, scope memory.Scope, in workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	if !scope.IsOwner || in.ThingID != nil || in.AgentID != "" {
		return workspace.DeskTurnResponse{}, memory.ErrInvalid
	}
	s.requests = append(s.requests, in)
	if s.turnErr != nil {
		return workspace.DeskTurnResponse{}, s.turnErr
	}
	id := string(memory.NewID())
	turn := workspace.SecretaryTurn{ID: string(memory.NewID()), Text: in.Text, Reply: "安排好了", Receipts: []workspace.DeskReceipt{{ActionID: &id, Text: "已建：开会", Status: "done", Undoable: true}}, Ask: s.ask}
	s.turns = append(s.turns, turn)
	return workspace.DeskTurnResponse{ConversationID: *in.ConversationID, Turn: turn}, nil
}
func (s *fakeStore) DeskTurns(context.Context, memory.Scope, string) (workspace.DeskTurnsResponse, error) {
	return workspace.DeskTurnsResponse{Turns: s.turns}, nil
}
func (s *fakeStore) Undo(_ context.Context, _ memory.Scope, id string) (workspace.State, error) {
	s.undoIDs = append(s.undoIDs, id)
	return workspace.State{}, s.undoErr
}
func (s *fakeStore) IngestAttachment(_ context.Context, _ memory.Scope, in memory.IngestRequest, r io.Reader) (memory.IngestResult, error) {
	s.attachments = append(s.attachments, in)
	b, _ := io.ReadAll(r)
	s.data = append(s.data, string(b))
	return memory.IngestResult{}, s.attachErr
}

type botCall struct {
	method string
	body   map[string]json.RawMessage
}
type botFixture struct {
	mu       sync.Mutex
	updates  []update
	calls    []botCall
	seq      int64
	fileSize int64
	fileData string
	failSend bool
}

func (b *botFixture) serve(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/file/") {
		_, _ = io.WriteString(w, b.fileData)
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(400)
		return
	}
	method := filepath.Base(r.URL.Path)
	b.calls = append(b.calls, botCall{method: method, body: body})
	var result any = true
	switch method {
	case "getUpdates":
		result = b.updates
	case "getFile":
		result = map[string]any{"file_path": "voice/test.ogg", "file_size": b.fileSize}
	case "sendMessage":
		if b.failSend {
			w.WriteHeader(500)
			return
		}
		b.seq++
		result = map[string]any{"message_id": b.seq}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}
func (b *botFixture) enqueue(updates ...update) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updates = updates
}
func (b *botFixture) of(method string) []botCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var calls []botCall
	for _, c := range b.calls {
		if c.method == method {
			calls = append(calls, c)
		}
	}
	return calls
}
func decode[T any](raw json.RawMessage) T { var v T; _ = json.Unmarshal(raw, &v); return v }

func fixture(t *testing.T) (*poller, *fakeStore, *botFixture) {
	t.Helper()
	s := &fakeStore{}
	b := &botFixture{seq: 100, fileData: "audio-bytes"}
	server := httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(server.Close)
	settings := notify.Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	if _, err := settings.EnsureVAPID(); err != nil {
		t.Fatal(err)
	}
	// Opaque fixture credentials are never a real bot token.
	if err := settings.SaveTelegram("fixture", "123"); err != nil {
		t.Fatal(err)
	}
	p := &poller{settings: settings, store: s, scope: memory.Scope{OwnerID: memory.NewID(), PrincipalID: "telegram", IsOwner: true}, api: botAPI{baseURL: server.URL, client: server.Client()}, interval: 10 * time.Millisecond, retry: 10 * time.Millisecond}
	return p, s, b
}
func textUpdate(id int64, text string) update {
	return update{ID: id, Message: &message{ID: id, Chat: chat{ID: 123, Type: "private"}, Text: text}}
}
func callbackUpdate(id int64, data string, messageID int64) update {
	q := &callback{ID: strconv.FormatInt(id, 10), Data: data, Message: &message{ID: messageID, Chat: chat{ID: 123, Type: "private"}}}
	q.From.ID = 123
	return update{ID: id, Callback: q}
}
func step(t *testing.T, p *poller) {
	t.Helper()
	if err := p.step(context.Background(), "fixture", "123"); err != nil {
		t.Fatal(err)
	}
}

func TestTextDuplicateAndRestart(t *testing.T) {
	p, s, b := fixture(t)
	b.enqueue(textUpdate(10, "周五下午三点开会"), textUpdate(10, "周五下午三点开会"))
	step(t, p)
	if len(s.requests) != 1 || !memory.ID(s.requests[0].RequestID).Valid() || s.requests[0].RequestID != requestID("123", "message:10") {
		t.Fatal(s.requests)
	}
	if got := decode[string](b.of("sendMessage")[0].body["text"]); !strings.Contains(got, "✓ 已建：开会") {
		t.Fatal(got)
	}
	rows := decode[keyboard](b.of("sendMessage")[0].body["reply_markup"]).Rows
	if len(rows) != 1 || !strings.HasPrefix(rows[0][0].Data, "u:") {
		t.Fatal(rows)
	}
	c, err := p.settings.Read()
	if err != nil || c.TelegramOffset != 11 || c.TelegramConversation == "" {
		t.Fatal(c.TelegramOffset, err)
	}
	// A fresh poller reads the actual persisted file and skips redelivery.
	fresh := *p
	fresh.settings = notify.Settings{Path: p.settings.Path}
	step(t, &fresh)
	if len(s.requests) != 1 || decode[int64](b.of("getUpdates")[1].body["offset"]) != 11 {
		t.Fatal("replayed old input")
	}
	if decode[int](b.of("getUpdates")[0].body["timeout"]) != 30 {
		t.Fatal("not long polling")
	}
	info, _ := os.Stat(p.settings.Path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if _, err := p.settings.EnsureVAPID(); err != nil {
		t.Fatal("C1 cannot read progress:", err)
	}
}

func TestUnauthorizedUpdates(t *testing.T) {
	p, s, b := fixture(t)
	other := textUpdate(1, "private text")
	other.Message.Chat.ID = 999
	group := textUpdate(2, "group text")
	group.Message.Chat.Type = "group"
	q := callbackUpdate(3, "u:"+string(memory.NewID()), 101)
	q.Callback.From.ID = 999
	b.enqueue(other, group, q)
	step(t, p)
	if len(s.requests)+len(s.undoIDs)+len(s.attachments)+len(b.of("sendMessage"))+len(b.of("answerCallbackQuery")) != 0 {
		t.Fatal("unauthorized input handled")
	}
	data, _ := os.ReadFile(p.settings.Path)
	if strings.Contains(string(data), "private text") || strings.Contains(string(data), "group text") {
		t.Fatal("recorded unauthorized contents")
	}
}

func TestUndoCallbacks(t *testing.T) {
	for _, test := range []struct {
		err  error
		text string
	}{
		{nil, "已撤销"}, {workspace.ErrChangedSince, "这件事之后又改过，没法直接撤销。"}, {workspace.ErrWorkStarted, "副手已经开始做了，没法撤销。"}, {workspace.ErrAlreadyUndone, "已经撤销过了。"}, {workspace.ErrExpired, "超过 30 天或相关资料已删除，无法撤销"}, {memory.ErrNotFound, "这条回执已失效。"},
	} {
		t.Run(test.text, func(t *testing.T) {
			p, s, b := fixture(t)
			s.undoErr = test.err
			id := string(memory.NewID())
			b.enqueue(callbackUpdate(1, "u:"+id, 101))
			step(t, p)
			if len(s.undoIDs) != 1 || s.undoIDs[0] != id || decode[string](b.of("answerCallbackQuery")[0].body["text"]) != test.text {
				t.Fatal("wrong undo response")
			}
		})
	}
}

func TestAskAndNewConversation(t *testing.T) {
	p, s, b := fixture(t)
	s.ask = &workspace.DeskAsk{Question: "哪位张三？", Options: []string{"同事", "房东"}}
	b.enqueue(textUpdate(1, "给张三回邮件"))
	step(t, p)
	rows := decode[keyboard](b.of("sendMessage")[0].body["reply_markup"]).Rows
	if rows[2][0].Data != "a:1" {
		t.Fatal(rows)
	}
	b.enqueue(callbackUpdate(2, "a:1", 101))
	step(t, p)
	if len(s.requests) != 2 || s.requests[1].Text != "房东" || *s.requests[0].ConversationID != *s.requests[1].ConversationID {
		t.Fatal(s.requests)
	}
	// Another selection on the same prompt cannot execute another action.
	b.enqueue(callbackUpdate(3, "a:0", 101))
	step(t, p)
	if len(s.requests) != 2 {
		t.Fatal("answered prompt reused")
	}
	b.enqueue(textUpdate(4, "/new"), textUpdate(5, "买牛奶"))
	step(t, p)
	if len(s.requests) != 3 || *s.requests[2].ConversationID == *s.requests[0].ConversationID {
		t.Fatal(s.requests)
	}
	b.enqueue(callbackUpdate(6, "a:1", 102))
	step(t, p)
	if len(s.requests) != 3 {
		t.Fatal("old conversation prompt reused")
	}
}

func TestDeletedPromptIsNotReplayed(t *testing.T) {
	p, s, b := fixture(t)
	s.ask = &workspace.DeskAsk{Question: "选择", Options: []string{"一", "二"}}
	b.enqueue(textUpdate(1, "secret"))
	step(t, p)
	s.turns[0].Text = ""
	b.enqueue(callbackUpdate(2, "a:1", 101))
	step(t, p)
	if len(s.requests) != 1 {
		t.Fatal("deleted prompt reused")
	}
	data, _ := os.ReadFile(p.settings.Path)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "选择") {
		t.Fatal("stored content in notify.json")
	}
}

func TestAskRecoversAfterRestartWithoutExtraSettings(t *testing.T) {
	p, s, b := fixture(t)
	s.ask = &workspace.DeskAsk{Question: "哪位张三？", Options: []string{"同事", "房东"}}
	b.enqueue(textUpdate(1, "给张三回邮件"))
	step(t, p)
	q := callbackUpdate(2, "a:1", 101)
	q.Callback.Message.Text = decode[string](b.of("sendMessage")[0].body["text"])
	fresh := *p
	fresh.state = state{}
	b.enqueue(q)
	step(t, &fresh)
	if len(s.requests) != 2 || s.requests[1].Text != "房东" || *s.requests[1].ConversationID != *s.requests[0].ConversationID {
		t.Fatal("prompt not recovered")
	}
	// An answered prompt cannot be recovered a second time after another restart.
	fresh.state = state{}
	q.ID = 3
	q.Callback.ID = "3"
	b.enqueue(q)
	step(t, &fresh)
	if len(s.requests) != 2 {
		t.Fatal("answered prompt recovered twice")
	}
	data, _ := os.ReadFile(p.settings.Path)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 6 {
		t.Fatal("added extra settings fields")
	}
}

func TestVoiceTranscriptionAndFallback(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(strconv.FormatBool(unavailable), func(t *testing.T) {
			p, s, b := fixture(t)
			calls := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/audio/transcriptions" {
					t.Error(r.URL.Path)
				}
				f, _, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					return
				}
				defer f.Close()
				data, _ := io.ReadAll(f)
				if string(data) != "audio-bytes" {
					t.Error(string(data))
				}
				_, _ = io.WriteString(w, `{"text":"明天下午三点开会"}`)
			}))
			defer model.Close()
			r := &ai.Registry{HTTP: model.Client()}
			if !unavailable {
				r.Config = ai.Configuration{Transcription: "asr", Providers: []ai.Provider{{ID: "asr", Transcription: true, Protocol: "openai", BaseURL: model.URL, Model: "test", CostMode: "free"}}}
			}
			p.models = r
			u := textUpdate(1, "")
			u.Message.Voice = &file{ID: "voice"}
			b.enqueue(u)
			step(t, p)
			text := decode[string](b.of("sendMessage")[0].body["text"])
			if unavailable {
				if calls != 0 || len(s.requests) != 0 || len(s.attachments) != 1 || s.attachments[0].MediaType != "audio/ogg" || s.data[0] != "audio-bytes" || text != "还没有配置语音转写，先发文字吧" {
					t.Fatal(text, s.attachments)
				}
			} else if calls != 1 || len(s.requests) != 1 || s.requests[0].Text != "明天下午三点开会" || !strings.HasPrefix(text, "🎤 听到：明天下午三点开会\n") {
				t.Fatal(text, s.requests)
			}
		})
	}
}

func TestAttachmentsCaptionsAndLimits(t *testing.T) {
	p, s, b := fixture(t)
	photo := textUpdate(1, "")
	photo.Message.Photo = []file{{ID: "small"}, {ID: "large", MIME: "image/jpeg"}}
	photo.Message.Caption = "帮我安排"
	doc := textUpdate(2, "")
	doc.Message.Document = &file{ID: "pdf", Name: "brief.pdf", MIME: "application/pdf"}
	big := textUpdate(3, "")
	big.Message.Document = &file{ID: "big", Size: maxFileSize + 1}
	b.enqueue(photo, doc, big)
	step(t, p)
	if len(s.attachments) != 2 || s.attachments[0].MediaType != "image/jpeg" || s.attachments[1].Title != "brief.pdf" || len(s.requests) != 1 || s.requests[0].Text != "帮我安排" {
		t.Fatal(s.attachments, s.requests)
	}
	if len(b.of("getFile")) != 2 || !strings.Contains(decode[string](b.of("sendMessage")[2].body["text"]), "20 MB") {
		t.Fatal("size limit ignored")
	}
	s.attachErr = memory.ErrInvalid
	b.enqueue(textUpdate(4, ""))
	b.updates[0].Message.Document = &file{ID: "unsupported", MIME: "application/octet-stream"}
	step(t, p)
	if got := decode[string](b.of("sendMessage")[3].body["text"]); strings.Contains(got, "已存") {
		t.Fatal("false storage receipt", got)
	}
}

func TestFailedReplyDoesNotAdvanceOffset(t *testing.T) {
	p, _, b := fixture(t)
	b.enqueue(textUpdate(10, "安排"))
	b.failSend = true
	if err := p.step(context.Background(), "fixture", "123"); err == nil {
		t.Fatal("failure hidden")
	}
	c, _ := p.settings.Read()
	if c.TelegramOffset != 0 || c.TelegramConversation == "" {
		t.Fatal("lost input or conversation")
	}
}

type changingTranscriber struct{ calls int }

func (m *changingTranscriber) Transcribe(context.Context, io.Reader, string) (string, error) {
	m.calls++
	if m.calls == 1 {
		return "明天下午三点开会", nil
	}
	return "不一样的转写", nil
}

func TestVoiceReplyRetryKeepsOriginalRequest(t *testing.T) {
	p, s, b := fixture(t)
	model := &changingTranscriber{}
	p.models = model
	u := textUpdate(1, "")
	u.Message.Voice = &file{ID: "voice"}
	b.enqueue(u)
	b.failSend = true
	if err := p.step(context.Background(), "fixture", "123"); err == nil {
		t.Fatal("delivery failure hidden")
	}
	b.failSend = false
	step(t, p)
	if model.calls != 1 || len(s.requests) != 2 || s.requests[1].Text != s.requests[0].Text || s.requests[1].RequestID != s.requests[0].RequestID || *s.requests[1].ConversationID != *s.requests[0].ConversationID {
		t.Fatal("retry changed voice request")
	}
	if p.state.Pending != nil {
		t.Fatal("acknowledged voice retained in transient cache")
	}
}

func TestChangedRetranscriptionDoesNotBlockLaterMessages(t *testing.T) {
	p, s, b := fixture(t)
	s.turnErr = memory.ErrConflict
	b.enqueue(textUpdate(1, "已经处理过的消息"))
	step(t, p)
	if !strings.Contains(decode[string](b.of("sendMessage")[0].body["text"]), "已经处理过") {
		t.Fatal("idempotency conflict not reported")
	}
	s.turnErr = nil
	b.enqueue(textUpdate(2, "下一句"))
	step(t, p)
	if len(s.requests) != 2 || s.requests[1].Text != "下一句" {
		t.Fatal("later updates stalled")
	}
}

func TestConfigurationRemovalCancelsPoll(t *testing.T) {
	p, _, b := fixture(t)
	started, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) != "getUpdates" {
			b.serve(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	p.api = botAPI{baseURL: server.URL, client: server.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); p.run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	if err := p.settings.SaveTelegram("", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("poll did not stop")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor leaked")
	}
}

func TestAPIErrorDoesNotExposeCredentials(t *testing.T) {
	a := newAPI()
	a.baseURL = "http://127.0.0.1:1"
	if err := a.call(context.Background(), "sensitive-fixture", "getUpdates", nil, nil); err == nil || strings.Contains(err.Error(), "sensitive-fixture") || !errors.Is(err, errAPI) {
		t.Fatal(err)
	}
}

func TestConfigurationRequiresChatAndRestartsOnTokenChange(t *testing.T) {
	p, _, _ := fixture(t)
	oldStarted, newStarted, oldCanceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasPrefix(r.URL.Path, "/botfixture/") {
			close(oldStarted)
			<-r.Context().Done()
			close(oldCanceled)
		} else {
			close(newStarted)
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	p.api = botAPI{baseURL: server.URL, client: server.Client()}
	if err := p.settings.SaveTelegram("fixture", ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); p.run(ctx) }()
	select {
	case <-oldStarted:
		t.Fatal("polled before chat detection")
	case <-time.After(3 * p.interval):
	}
	if err := p.settings.SaveTelegram("fixture", "123"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-oldStarted:
	case <-time.After(time.Second):
		t.Fatal("poll did not start after chat configured")
	}
	if err := p.settings.SaveTelegram("next-fixture", "123"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-oldCanceled:
	case <-time.After(time.Second):
		t.Fatal("old poll not canceled")
	}
	select {
	case <-newStarted:
	case <-time.After(time.Second):
		t.Fatal("new poll not started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
}
