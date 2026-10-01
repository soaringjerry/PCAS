// Package telegram connects one private Telegram chat to the workspace secretary.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxFileSize = 20 << 20

// Errors deliberately omit credential-bearing URLs and remote response bodies.
var errAPI = errors.New("telegram request failed")
var errFile = errors.New("telegram attachment unavailable or exceeds 20 MB")

type botAPI struct {
	baseURL string
	client  *http.Client
}

func newAPI() botAPI {
	return botAPI{baseURL: "https://api.telegram.org", client: &http.Client{
		Timeout:       40 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (a botAPI) call(ctx context.Context, token, method string, input, output any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return errAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/bot"+token+"/"+method, bytes.NewReader(b))
	if err != nil {
		return errAPI
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return errAPI
	}
	defer resp.Body.Close()
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&envelope) != nil || !envelope.OK {
		return errAPI
	}
	if output != nil && json.Unmarshal(envelope.Result, output) != nil {
		return errAPI
	}
	return nil
}

// identity asks Telegram for the stable bot user ID; credentials never become
// part of a request ID or a user-visible identifier.
func (a botAPI) identity(ctx context.Context, token string) (int64, error) {
	var user struct {
		ID  int64 `json:"id"`
		Bot bool  `json:"is_bot"`
	}
	if err := a.call(ctx, token, "getMe", map[string]any{}, &user); err != nil {
		return 0, err
	}
	if user.ID <= 0 || !user.Bot {
		return 0, errAPI
	}
	return user.ID, nil
}

func (a botAPI) download(ctx context.Context, token string, f file) ([]byte, string, error) {
	if f.Size > maxFileSize {
		return nil, "", errFile
	}
	var remote struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if err := a.call(ctx, token, "getFile", map[string]string{"file_id": f.ID}, &remote); err != nil {
		return nil, "", err
	}
	if remote.Size > maxFileSize || remote.Path == "" {
		return nil, "", errFile
	}
	// file_path is a relative path supplied by Telegram, never an arbitrary URL.
	for _, part := range strings.Split(remote.Path, "/") {
		if part == ".." || part == "." || part == "" {
			return nil, "", errFile
		}
	}
	path := (&url.URL{Path: remote.Path}).EscapedPath()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/file/bot"+token+"/"+path, nil)
	if err != nil {
		return nil, "", errAPI
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, "", errAPI
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxFileSize {
		return nil, "", errFile
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFileSize+1))
	if err != nil || len(b) > maxFileSize {
		return nil, "", errFile
	}
	return b, remote.Path, nil
}

type chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type file struct {
	ID   string `json:"file_id"`
	Size int64  `json:"file_size"`
	Name string `json:"file_name"`
	MIME string `json:"mime_type"`
}
type message struct {
	ID       int64  `json:"message_id"`
	Chat     chat   `json:"chat"`
	Text     string `json:"text"`
	Caption  string `json:"caption"`
	Voice    *file  `json:"voice"`
	Audio    *file  `json:"audio"`
	Photo    []file `json:"photo"`
	Document *file  `json:"document"`
}
type callback struct {
	ID   string `json:"id"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Message *message `json:"message"`
	Data    string   `json:"data"`
}
type update struct {
	ID       int64     `json:"update_id"`
	Message  *message  `json:"message"`
	Callback *callback `json:"callback_query"`
}
type button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}
type keyboard struct {
	Rows [][]button `json:"inline_keyboard"`
}

func (a botAPI) send(ctx context.Context, token, chatID, text string, rows [][]button) (int64, error) {
	in := map[string]any{"chat_id": chatID, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}
	if len(rows) > 0 {
		in["reply_markup"] = keyboard{Rows: rows}
	}
	var sent message
	err := a.call(ctx, token, "sendMessage", in, &sent)
	return sent.ID, err
}

func (a botAPI) answer(ctx context.Context, token, id, text string) error {
	return a.call(ctx, token, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": clip(text, 180)}, nil)
}
