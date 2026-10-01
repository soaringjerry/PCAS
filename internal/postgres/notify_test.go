package postgres

import (
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type fakeNotifyChannel struct {
	name   string
	calls  []notify.Message
	err    error
	before func()
}

func (c *fakeNotifyChannel) Name() string { return c.name }
func (c *fakeNotifyChannel) Send(_ context.Context, m notify.Message) error {
	if c.before != nil {
		c.before()
	}
	c.calls = append(c.calls, m)
	return c.err
}
func dueNotice(t *testing.T, s *Store, scope memory.Scope, now time.Time) workspace.Notice {
	t.Helper()
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "提醒事项"})
	item := state.Tasks[0]
	item.Triggers = []workspace.Trigger{{ID: "due-reminder", Kind: "time", Description: "截止前提醒", NextAt: now.Add(-time.Minute).UTC().Format(time.RFC3339), Active: true}}
	if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error { return saveItem(context.Background(), tx, scope, item) }); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckReminders(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Notices) != 1 {
		t.Fatal("notice missing from snapshot")
	}
	return state.Notices[0]
}
func deliveredFor(t *testing.T, s *Store, id string) map[string]json.RawMessage {
	t.Helper()
	var raw []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT delivered FROM workspace_notices WHERE id=$1", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestNotifyDeliveryAndRecheck(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Australia/Melbourne"})})
	now := time.Now().Add(time.Second)
	t.Setenv("PCAS_PUBLIC_URL", "https://example.com")
	notice := dueNotice(t, s, scope, now)
	channel := &fakeNotifyChannel{name: "mobile"} // no dispatcher changes for future channels
	for range 2 {
		if err := s.DispatchNotices(context.Background(), now, []notify.Channel{channel}); err != nil {
			t.Fatal(err)
		}
	}
	if len(channel.calls) != 1 || deliveredFor(t, s, notice.ID)["mobile"] == nil {
		t.Fatal("duplicate delivery or missing receipt")
	}
	m := channel.calls[0]
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		t.Fatal(err)
	}
	due, err := time.Parse(time.RFC3339, notice.DueAt)
	if err != nil {
		t.Fatal(err)
	}
	if m.NoticeID != notice.ID || m.Title != notice.Title || m.URL != "https://example.com/t/"+notice.ThingID || !strings.Contains(m.Body, due.In(loc).Format("2006-01-02 15:04 MST")+" · 截止前提醒") {
		t.Fatalf("wrong reminder message: %+v", m)
	}
	// Another channel completes the item after enumeration and the first send.
	complete := &fakeNotifyChannel{name: "first", before: func() {
		workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: notice.ThingID, Status: "done"})
	}}
	skipped := &fakeNotifyChannel{name: "second"}
	if err := s.DispatchNotices(context.Background(), now, []notify.Channel{complete, skipped}); err != nil {
		t.Fatal(err)
	}
	if len(complete.calls) != 1 || len(skipped.calls) != 0 {
		t.Fatal("did not recheck state before next channel")
	}
	if err := s.DispatchNotices(context.Background(), now, []notify.Channel{skipped}); err != nil {
		t.Fatal(err)
	}
	if len(skipped.calls) != 0 {
		t.Fatal("completed item sent")
	}
}
func TestNotifyRetryBackoffAndLimit(t *testing.T) {
	s := testStore(t)
	scope := owner()
	now := time.Now().Add(time.Second)
	notice := dueNotice(t, s, scope, now)
	failed := &fakeNotifyChannel{name: "telegram", err: errors.New("simulated outage")}
	unconfigured := &fakeNotifyChannel{name: "webpush", err: notify.ErrUnconfigured}
	for attempt, delay := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		if err := s.DispatchNotices(context.Background(), now, []notify.Channel{failed, unconfigured}); err != nil {
			t.Fatal(err)
		}
		var attempts map[string]int
		if err := json.Unmarshal(deliveredFor(t, s, notice.ID)["attempts"], &attempts); err != nil {
			t.Fatal(err)
		}
		if attempts["telegram"] != attempt+1 || attempts["webpush"] != 0 {
			t.Fatal("wrong attempt accounting", attempts)
		}
		if err := s.DispatchNotices(context.Background(), now.Add(delay-time.Second), []notify.Channel{failed}); err != nil {
			t.Fatal(err)
		}
		if len(failed.calls) != attempt+1 {
			t.Fatal("retried before backoff expired")
		}
		now = now.Add(delay)
	}
	if err := s.DispatchNotices(context.Background(), now.Add(time.Hour), []notify.Channel{failed}); err != nil {
		t.Fatal(err)
	}
	if len(failed.calls) != 5 {
		t.Fatal("retried after exhaustion")
	}
}
func TestNotifyConcurrentDispatch(t *testing.T) {
	s := testStore(t)
	scope := owner()
	now := time.Now().Add(time.Second)
	dueNotice(t, s, scope, now)
	entered, release := make(chan struct{}), make(chan struct{})
	channel := &fakeNotifyChannel{name: "webpush", before: func() { close(entered); <-release }}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.DispatchNotices(context.Background(), now, []notify.Channel{channel}); err != nil {
			t.Error(err)
		}
	}()
	<-entered
	if err := s.DispatchNotices(context.Background(), now, []notify.Channel{channel}); err != nil {
		t.Error(err)
	}
	close(release)
	wg.Wait()
	if len(channel.calls) != 1 {
		t.Fatal("concurrent dispatch duplicated notice")
	}
}
func TestNotifySkipsClosedOldCancelledAndDisabled(t *testing.T) {
	for _, condition := range []string{"dismissed", "old", "cancelled", "followUps"} {
		t.Run(condition, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			now := time.Now().Add(time.Second)
			notice := dueNotice(t, s, scope, now)
			ctx := context.Background()
			var err error
			switch condition {
			case "dismissed":
				_, err = s.pool.Exec(ctx, "UPDATE workspace_notices SET dismissed_at=now() WHERE id=$1", notice.ID)
			case "old":
				_, err = s.pool.Exec(ctx, "UPDATE workspace_notices SET created_at=$2 WHERE id=$1", notice.ID, now.Add(-25*time.Hour))
			case "cancelled":
				workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: notice.ThingID, Status: "cancelled"})
			case "followUps":
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: json.RawMessage(`{"followUps":false}`)})
			}
			if err != nil {
				t.Fatal(err)
			}
			channel := &fakeNotifyChannel{name: "telegram"}
			if err := s.DispatchNotices(ctx, now, []notify.Channel{channel}); err != nil {
				t.Fatal(err)
			}
			if len(channel.calls) != 0 {
				t.Fatal("ineligible notice sent")
			}
		})
	}
}

type notificationAuth struct{ scope memory.Scope }

func (a notificationAuth) Authenticate(*http.Request) (memory.Scope, bool) { return a.scope, true }
func notifyHandler(n *Notifier, scope memory.Scope) http.Handler {
	return httpapi.New(nil, nil, notificationAuth{scope}, n.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: n})
}
func notifyRequest(t *testing.T, h http.Handler, method, path string, in any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer synthetic")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestNotifyHTTPStateDismissTelegramAndOwnerBoundary(t *testing.T) {
	s := testStore(t)
	scope := owner()
	now := time.Now().Add(time.Second)
	notice := dueNotice(t, s, scope, now)
	settings := notify.Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	n := NewNotifier(s, settings, "https://example.com")
	h := notifyHandler(n, scope)
	config := notifyRequest(t, h, "GET", "/v1/notify/config", nil)
	if config.Code != 200 || strings.Contains(config.Body.String(), "vapidPrivate") || strings.Contains(config.Body.String(), "telegramToken") {
		t.Fatal("configuration missing or leaks secrets", config.Code)
	}
	state := notifyRequest(t, h, "GET", "/v1/workspace", nil)
	if state.Code != 200 || !strings.Contains(state.Body.String(), `"notices":[`) || !strings.Contains(state.Body.String(), notice.ID) {
		t.Fatal("workspace notices absent")
	}
	dismissed := notifyRequest(t, h, "POST", "/v1/notify/notices/"+notice.ID+"/dismiss", nil)
	if dismissed.Code != 200 || !strings.Contains(dismissed.Body.String(), `"dismissedAt"`) {
		t.Fatal("dismiss failed", dismissed.Code, dismissed.Body.String())
	}
	if w := notifyRequest(t, h, "POST", "/v1/notify/notices/"+string(memory.NewID())+"/dismiss", nil); w.Code != 404 {
		t.Fatal("unknown notice not 404")
	}
	other := owner()
	if w := notifyRequest(t, notifyHandler(n, other), "POST", "/v1/notify/notices/"+notice.ID+"/dismiss", nil); w.Code != 404 {
		t.Fatal("cross-owner dismissal allowed")
	}
	scope.IsOwner = false
	for _, route := range [][2]string{{"GET", "config"}, {"POST", "push-subscriptions"}, {"DELETE", "push-subscriptions"}, {"PUT", "telegram"}, {"POST", "test"}, {"POST", "notices/" + notice.ID + "/dismiss"}} {
		if w := notifyRequest(t, notifyHandler(n, scope), route[0], "/v1/notify/"+route[1], nil); w.Code != 403 {
			t.Fatal("agent can manage notifications", route, w.Code)
		}
	}
	valid := true
	messages := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !valid {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":401}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "getUpdates") {
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"message":{"chat":{"id":123,"type":"private"}}}]}`))
			return
		}
		messages++
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer api.Close()
	n.Telegram.BaseURL = api.URL
	n.Telegram.Client = api.Client()
	token := "123456:" + filepath.Base(t.TempDir())
	if w := notifyRequest(t, h, "PUT", "/v1/notify/telegram", workspace.TelegramConfig{BotToken: token}); w.Code != 200 {
		t.Fatal("chat discovery/save failed", w.Code)
	}
	keys, err := settings.Read()
	if err != nil || keys.TelegramChatID != "123" || messages != 1 {
		t.Fatal("not tested before saving")
	}
	valid = false
	if w := notifyRequest(t, h, "PUT", "/v1/notify/telegram", workspace.TelegramConfig{BotToken: "654321:" + filepath.Base(t.TempDir()), ChatID: "456"}); w.Code != 400 {
		t.Fatal("invalid token not rejected", w.Code)
	}
	after, err := settings.Read()
	if err != nil || after.TelegramChatID != keys.TelegramChatID || after.TelegramToken != keys.TelegramToken {
		t.Fatal("failed validation overwrote working credentials")
	}
	n.Channels = []notify.Channel{&fakeNotifyChannel{name: "webpush"}, &fakeNotifyChannel{name: "telegram", err: notify.ErrUnconfigured}}
	if w := notifyRequest(t, h, "POST", "/v1/notify/test", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"sent":["webpush"]`) {
		t.Fatal("test delivery result incorrect")
	}
	if w := notifyRequest(t, h, "PUT", "/v1/notify/telegram", workspace.TelegramConfig{}); w.Code != 200 || !strings.Contains(w.Body.String(), `false`) {
		t.Fatal("disconnect failed")
	}
}

// Saving Telegram only needs the settings file and transport, so these HTTP
// regressions also run without an integration database.
func TestNotifyHTTPTelegramErrors(t *testing.T) {
	const token = "123456:synthetic_token_for_tests"
	for _, tt := range []struct {
		name, token, chatID, method, body, code string
		status                                  int
	}{
		{name: "token/missing separator", token: "invalid-token", code: "telegram_token_invalid"},
		{name: "token/invalid bot ID", token: "bot:secret", code: "telegram_token_invalid"},
		{name: "token/URL characters", token: "123:secret?query", code: "telegram_token_invalid"},
		{name: "token/too long", token: "123:" + strings.Repeat("a", 253), code: "telegram_token_invalid"},
		{name: "token/HTTP 401", token: token, method: "getUpdates", status: 401, body: `{"ok":false,"error_code":401}`, code: "telegram_token_invalid"},
		{name: "token/HTTP 404 non JSON", token: token, method: "getUpdates", status: 404, body: "Not Found", code: "telegram_token_invalid"},
		{name: "token/JSON 404", token: token, method: "getUpdates", status: 200, body: `{"ok":false,"error_code":404}`, code: "telegram_token_invalid"},
		{name: "token/send HTTP 401", token: token, chatID: "123", method: "sendMessage", status: 401, body: `{"ok":false,"error_code":401}`, code: "telegram_token_invalid"},
		{name: "webhook/HTTP 409 non JSON", token: token, method: "getUpdates", status: 409, body: "Conflict", code: "telegram_webhook_active"},
		{name: "webhook/JSON 409", token: token, method: "getUpdates", status: 200, body: `{"ok":false,"error_code":409}`, code: "telegram_webhook_active"},
		{name: "no chat/empty updates", token: token, method: "getUpdates", status: 200, body: `{"ok":true,"result":[]}`, code: "telegram_no_chat"},
		{name: "no chat/group only", token: token, method: "getUpdates", status: 200, body: `{"ok":true,"result":[{"message":{"chat":{"id":-123,"type":"group"}}}]}`, code: "telegram_no_chat"},
		{name: "send/chat not found", token: token, method: "sendMessage", status: 400, body: `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, code: "telegram_send_failed"},
		{name: "send/bot blocked", token: token, chatID: "123", method: "sendMessage", status: 403, body: `{"ok":false,"error_code":403}`, code: "telegram_send_failed"},
		{name: "send/invalid chat ID", token: token, chatID: "not-a-number", code: "telegram_send_failed"},
		{name: "send/getUpdates unavailable", token: token, method: "getUpdates", status: 500, body: "Unavailable", code: "telegram_send_failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := []string{}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method := filepath.Base(r.URL.Path)
				calls = append(calls, method)
				if method != tt.method {
					if method != "getUpdates" {
						t.Error("unexpected Telegram method")
					}
					_, _ = w.Write([]byte(`{"ok":true,"result":[{"message":{"chat":{"id":123,"type":"private"}}}]}`))
					return
				}
				w.WriteHeader(tt.status)
				// Even upstream descriptions containing credentials must stay redacted.
				_, _ = w.Write([]byte(strings.ReplaceAll(tt.body, "chat not found", tt.token)))
			}))
			defer api.Close()
			settings := notify.Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
			if err := settings.SaveTelegram("987654:previous_test_token", "456"); err != nil {
				t.Fatal(err)
			}
			n := NewNotifier(nil, settings, "https://example.com")
			n.Telegram.BaseURL, n.Telegram.Client = api.URL, api.Client()
			in := workspace.TelegramConfig{BotToken: tt.token, ChatID: tt.chatID}
			// Check both the store's typed error and the externally visible response.
			_, err := n.SaveTelegram(context.Background(), owner(), in)
			want := map[string]error{
				"telegram_token_invalid":  notify.ErrTelegramTokenInvalid,
				"telegram_webhook_active": notify.ErrTelegramWebhookActive,
				"telegram_no_chat":        notify.ErrTelegramNoChat,
				"telegram_send_failed":    notify.ErrTelegramSendFailed,
			}[tt.code]
			if !errors.Is(err, want) || strings.Contains(err.Error(), tt.token) {
				t.Fatal("wrong typed error or credential leak")
			}
			calls = nil
			w := notifyRequest(t, notifyHandler(n, owner()), "PUT", "/v1/notify/telegram", in)
			var out struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusBadRequest || out.Error != tt.code || strings.Contains(w.Body.String(), tt.token) {
				t.Fatalf("got %d %s, want 400 %s", w.Code, w.Body.String(), tt.code)
			}
			if tt.method == "" && len(calls) != 0 {
				t.Fatal("invalid input reached Telegram")
			}
			if tt.method != "" && (len(calls) == 0 || calls[len(calls)-1] != tt.method) {
				t.Fatal("expected Telegram failure was not exercised")
			}
			after, err := settings.Read()
			if err != nil || after.TelegramToken != "987654:previous_test_token" || after.TelegramChatID != "456" {
				t.Fatal("failed validation overwrote working credentials")
			}
		})
	}
}

func TestNotifyPushSubscriptionScopeAndHTTP(t *testing.T) {
	s := testStore(t)
	scope := owner()
	other := owner()
	n := NewNotifier(s, notify.Settings{Path: filepath.Join(t.TempDir(), "notify.json")}, "https://example.com")
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	sub := webpush.Subscription{Endpoint: "https://push.example.com/device", Keys: webpush.Keys{
		P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)), Auth: base64.RawURLEncoding.EncodeToString(auth),
	}}
	h := notifyHandler(n, scope)
	for range 2 {
		if w := notifyRequest(t, h, "POST", "/v1/notify/push-subscriptions", sub); w.Code != 204 {
			t.Fatal("subscription rejected", w.Code)
		}
	}
	if err := n.SavePushSubscription(context.Background(), other, sub); err != nil {
		t.Fatal(err)
	}
	config, err := n.NotifyConfig(context.Background(), scope)
	if err != nil || config.WebPush.Subscriptions != 1 {
		t.Fatal("duplicate subscription", err)
	}
	invalid := sub
	invalid.Endpoint = "http://localhost:5432/"
	if w := notifyRequest(t, h, "POST", "/v1/notify/push-subscriptions", invalid); w.Code != 400 {
		t.Fatal("invalid subscription accepted")
	}
	if w := notifyRequest(t, h, "DELETE", "/v1/notify/push-subscriptions", map[string]string{"endpoint": sub.Endpoint}); w.Code != 204 {
		t.Fatal("delete failed")
	}
	own, err := s.Subscriptions(context.Background(), string(scope.OwnerID))
	if err != nil || len(own) != 0 {
		t.Fatal("own subscription retained")
	}
	remaining, err := s.Subscriptions(context.Background(), string(other.OwnerID))
	if err != nil || len(remaining) != 1 {
		t.Fatal("another owner's subscription removed")
	}
}
