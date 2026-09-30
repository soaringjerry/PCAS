package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Desk intents: answer from memory, file it for background sorting, or hand
// it to the assistant as a task.
const (
	IntentAsk      = "ask"
	IntentRecord   = "record"
	IntentDelegate = "delegate"
)

// Router asks TypeSafe's Jev one bounded question per desk entry. It returns
// a choice and a confidence, never text, so nothing it says reaches the user.
type Router struct {
	HTTP    *http.Client
	BaseURL string
	// Key is read on every call so a key saved in Settings applies at once.
	Key func() string
}

type Route struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
}

func NewRouter(key func() string) *Router {
	return &Router{HTTP: &http.Client{Timeout: 5 * time.Second}, BaseURL: "https://api.typesafe.ai", Key: key}
}

// Configured is false without a key; callers then fall back to the rule.
func (r *Router) Configured() bool {
	return r != nil && r.Key() != ""
}

func (r *Router) Route(ctx context.Context, text string) (Route, error) {
	key := r.Key()
	if key == "" {
		return Route{}, fmt.Errorf("decision provider not configured")
	}
	body, err := json.Marshal(map[string]any{
		"model": "jev-latest",
		"state": text,
		"questions": map[string]any{
			"intent": map[string]any{
				"type":         "choice",
				"instructions": "A person typed this into their personal assistant's single input box (usually Chinese). What do they want done with it right now?",
				"criteria": map[string]string{
					IntentAsk:      "They are asking about something they did, said, planned or saved before and want an answer now, e.g. 我上次体检是哪天 / 周五要交什么",
					IntentRecord:   "They are noting a fact, plan, reminder, idea or feeling to keep; nothing needs to be produced now, e.g. 下周三下午三点牙医 / 想做一个记账小工具",
					IntentDelegate: "They want the assistant to produce or work on something: write, draft, plan, summarise, research, compare, break down, e.g. 帮我写封请假邮件 / 把这周的会议整理一下",
				},
			},
		},
	})
	if err != nil {
		return Route{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.BaseURL, "/")+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return Route{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	response, err := r.HTTP.Do(req)
	if err != nil {
		return Route{}, fmt.Errorf("decision provider unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Route{}, fmt.Errorf("decision provider HTTP %d", response.StatusCode)
	}
	var out struct {
		Answers struct {
			Intent struct {
				Choice     string  `json:"choice"`
				Confidence float64 `json:"confidence"`
			} `json:"intent"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&out); err != nil {
		return Route{}, fmt.Errorf("decision provider returned invalid JSON")
	}
	a := out.Answers.Intent
	if a.Choice != IntentAsk && a.Choice != IntentRecord && a.Choice != IntentDelegate || a.Confidence < 0 || a.Confidence > 1 {
		return Route{}, fmt.Errorf("decision provider returned an unknown choice")
	}
	return Route{Intent: a.Choice, Confidence: a.Confidence}, nil
}
