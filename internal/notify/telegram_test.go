package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelegramAPIAndPrivateChatDiscovery(t *testing.T) {
	token := filepath.Base(t.TempDir())
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/bot"+token+"/") {
			t.Error("wrong bot endpoint")
		}
		calls = append(calls, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "getUpdates") {
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"message":{"chat":{"id":123,"type":"private"}}},{"message":{"chat":{"id":456,"type":"private"}}},{"message":{"chat":{"id":-789,"type":"group"}}}]}`))
			return
		}
		var in struct {
			ChatID    string `json:"chat_id"`
			Text      string `json:"text"`
			ParseMode string `json:"parse_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.ChatID != "456" || in.Text != "⏰ 测试事项\n2026-09-30 15:00 CST\nhttps://example.com/t/task" || in.ParseMode != "" {
			t.Errorf("unexpected sendMessage: %+v", in)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	settings := Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	telegram := &Telegram{Settings: settings, BaseURL: server.URL, Client: server.Client()}
	chat, err := telegram.ResolveChat(context.Background(), token)
	if err != nil || chat != "456" {
		t.Fatal(chat, err)
	}
	if err := settings.SaveTelegram(token, chat); err != nil {
		t.Fatal(err)
	}
	if err := telegram.Send(context.Background(), Message{Title: "测试事项", Body: "2026-09-30 15:00 CST · 截止前提醒", URL: "https://example.com/t/task"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatal("unexpected API calls")
	}
}
func TestTelegramRedactsCredentialErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401}`))
	}))
	defer server.Close()
	telegram := &Telegram{BaseURL: server.URL, Client: server.Client()}
	token := filepath.Base(t.TempDir())
	_, err := telegram.ResolveChat(context.Background(), token)
	if !errors.Is(err, ErrGone) || strings.Contains(err.Error(), token) {
		t.Fatal("credential failure not redacted")
	}
	server.Close()
	_, err = telegram.ResolveChat(context.Background(), token)
	if !errors.Is(err, ErrDelivery) || strings.Contains(err.Error(), token) {
		t.Fatal("network error exposes token")
	}
}
