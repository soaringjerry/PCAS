package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type Telegram struct {
	Settings Settings
	BaseURL  string // override only in tests
	Client   *http.Client
}

func (*Telegram) Name() string { return "telegram" }
func (t *Telegram) call(ctx context.Context, token, method string, input, output any) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return ErrDelivery
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/bot"+token+"/"+method, bytes.NewReader(payload))
	if err != nil {
		return ErrDelivery
	}
	req.Header.Set("Content-Type", "application/json")
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(req)
	if err != nil {
		return ErrDelivery
	} // never return an error containing the token-bearing URL
	defer response.Body.Close()
	var result struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Code   int             `json:"error_code"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil {
		return ErrDelivery
	}
	if !result.OK || response.StatusCode != 200 {
		if result.Code == 401 || result.Code == 403 || response.StatusCode == 401 || response.StatusCode == 403 {
			return ErrGone
		}
		return ErrDelivery
	}
	if output != nil && json.Unmarshal(result.Result, output) != nil {
		return ErrDelivery
	}
	return nil
}
func (t *Telegram) ResolveChat(ctx context.Context, token string) (string, error) {
	var updates []struct {
		Message *struct {
			Chat struct {
				ID   json.Number `json:"id"`
				Type string      `json:"type"`
			} `json:"chat"`
		} `json:"message"`
	}
	if err := t.call(ctx, token, "getUpdates", map[string]any{"timeout": 0, "limit": 100}, &updates); err != nil {
		return "", err
	}
	for i := len(updates) - 1; i >= 0; i-- {
		if m := updates[i].Message; m != nil && m.Chat.Type == "private" {
			return string(m.Chat.ID), nil
		}
	}
	return "", ErrUnconfigured
}
func (t *Telegram) SendTo(ctx context.Context, token, chatID string, m Message) error {
	if token == "" || chatID == "" {
		return ErrUnconfigured
	}
	clock, _, _ := strings.Cut(m.Body, " · ")
	return t.call(ctx, token, "sendMessage", map[string]any{"chat_id": chatID, "text": "⏰ " + m.Title + "\n" + clock + "\n" + m.URL, "link_preview_options": map[string]bool{"is_disabled": true}}, nil)
}
func (t *Telegram) Send(ctx context.Context, m Message) error {
	c, err := t.Settings.Read()
	if err != nil {
		return ErrDelivery
	}
	return t.SendTo(ctx, c.TelegramToken, c.TelegramChatID, m)
}
