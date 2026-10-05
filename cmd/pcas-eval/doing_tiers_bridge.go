package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/internal/ai"
)

// This semaphore covers ALL real model calls, including parallel product
// readers and the two judges. Worker count alone does not bound heavy readers.
type tierLimitedModel struct {
	model doing.Model
	slots chan struct{}
}

func (m *tierLimitedModel) Generate(ctx context.Context, system, prompt string) (string, error) {
	return m.generateCounted(ctx, system, prompt, nil)
}
func (m *tierLimitedModel) generateCounted(ctx context.Context, system, prompt string, started func()) (string, error) {
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if started != nil {
		started()
	}
	return m.model.Generate(ctx, system, prompt)
}

type tierBridge struct {
	mu                      sync.Mutex
	model                   doing.Model
	fake                    bool
	stage                   string
	suite                   doing.Suite
	task                    doing.Task
	usage                   *doing.TierUsage
	mainInput, contextChars int
	server                  *httptest.Server
}

func newTierBridge(model doing.Model, fake bool) *tierBridge {
	b := &tierBridge{model: model, fake: fake}
	b.reset("", doing.Suite{}, doing.Task{}, "")
	b.server = httptest.NewServer(http.HandlerFunc(b.serve))
	return b
}
func (b *tierBridge) reset(stage string, s doing.Suite, t doing.Task, tier string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stage, b.suite, b.task = stage, s, t
	b.usage = &doing.TierUsage{Requested: tier, Calls: map[string]int{}, Failed: map[string]int{}, NotStarted: map[string]int{}, Malformed: map[string]int{}, InputChars: map[string]int{}}
	b.mainInput, b.contextChars = 0, 0
}
func (b *tierBridge) metrics() (doing.TierUsage, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var copy doing.TierUsage
	_ = json.Unmarshal([]byte(stringJSON(b.usage)), &copy)
	return copy, b.mainInput, b.contextChars
}
func (b *tierBridge) registry() *ai.Registry {
	return &ai.Registry{HTTP: b.server.Client(), Config: ai.Configuration{Extraction: "v2-tiers", Providers: []ai.Provider{{ID: "v2-tiers", Name: "档位评测", Protocol: "openai", BaseURL: b.server.URL, Model: "v2-tiers", CostMode: "free"}}}}
}

func tierContextSections(prompt string) (string, error) {
	start := strings.Index(prompt, "\n交接说明：")
	if start < 0 {
		start = strings.Index(prompt, "召回的记忆")
	}
	if start < 0 {
		return "", fmt.Errorf("unknown product context format")
	}
	end := strings.Index(prompt[start:], "\nTHIS：")
	if end < 0 {
		return "", fmt.Errorf("unknown product context boundary")
	}
	text := prompt[start : start+end]
	// DeskTurn appends heavy-reader selections after the ordinary prompt's
	// final JSON instruction. They are part of supplied evidence too.
	if extra := strings.Index(prompt[start+end:], "\n重档读者补充 "); extra >= 0 {
		text += prompt[start+end+extra:]
	}
	return text, nil
}
func (b *tierBridge) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	if decoder.Decode(&req) != nil {
		http.Error(w, "invalid model request", http.StatusBadRequest)
		return
	}
	var system, prompt string
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
		}
		if m.Role == "user" {
			prompt = m.Content
		}
	}
	b.mu.Lock()
	stage, s, t, ledger := b.stage, b.suite, b.task, b.usage
	b.mu.Unlock()
	purpose := stage
	main := false
	if purpose == "" {
		switch {
		case strings.Contains(system, "这是自查"):
			purpose = "selfcheck"
		case strings.Contains(system, "只读的记忆读者"):
			purpose = "reader"
			if strings.Contains(prompt, "分组目录（只选这些 key") {
				purpose = "selector"
			}
		case strings.Contains(system, "用户的前台秘书"):
			purpose, main = "secretary", true
		default:
			http.Error(w, "unrecognized product call", http.StatusBadRequest)
			return
		}
	}
	if main {
		contextText, err := tierContextSections(prompt)
		if err != nil {
			http.Error(w, "product context format changed", http.StatusBadRequest)
			return
		}
		// Preserve the old frozen answer instruction and date without providing
		// gold. The product still constructs context, runs readers/selfcheck and
		// validates the reply. This evaluator has always measured draft results.
		system, prompt = doing.AnswerSystem, doing.AnswerPrompt(s, t, contextText)
		b.mu.Lock()
		b.mainInput = utf8.RuneCountInString(system + prompt)
		b.contextChars = utf8.RuneCountInString(contextText)
		b.mu.Unlock()
	}
	started := false
	onStart := func() {
		started = true
		b.mu.Lock()
		ledger.Calls[purpose]++
		ledger.InputChars[purpose] += utf8.RuneCountInString(system + prompt)
		b.mu.Unlock()
	}
	var raw string
	var err error
	if b.fake {
		onStart()
		raw, err = tierFakeReply(purpose, prompt)
	} else if limited, ok := b.model.(*tierLimitedModel); ok {
		raw, err = limited.generateCounted(r.Context(), system, prompt, onStart)
	} else {
		onStart()
		raw, err = b.model.Generate(r.Context(), system, prompt)
	}
	if err != nil {
		b.mu.Lock()
		ledger.Failed[purpose]++
		if !started {
			ledger.NotStarted[purpose]++
		}
		b.mu.Unlock()
		// Raw provider errors can contain private text. Only a fixed category
		// crosses the local bridge; the product applies its real fallback/retry.
		http.Error(w, "eval_model_"+tierModelError(err), http.StatusBadGateway)
		return
	}
	if purpose == "selfcheck" || purpose == "reader" || purpose == "selector" {
		var fields map[string]json.RawMessage
		field := map[string]string{"selfcheck": "reply", "reader": "used", "selector": "groups"}[purpose]
		invalid := json.Unmarshal([]byte(raw), &fields) != nil || fields[field] == nil || string(fields[field]) == "null"
		if purpose == "selfcheck" {
			var reply string
			invalid = invalid || json.Unmarshal(fields[field], &reply) != nil
		}
		if invalid {
			b.mu.Lock()
			ledger.Malformed[purpose]++
			b.mu.Unlock()
		}
	}
	if main {
		raw = stringJSON(map[string]any{"reply": raw, "used": []string{}, "links": []string{}, "show": []string{}, "remember": false, "missingKeyInfo": false, "actions": []any{}, "ask": nil})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": raw}}}})
}
func tierModelError(err error) string {
	if err == context.DeadlineExceeded {
		return "timeout"
	}
	if err == context.Canceled {
		return "canceled"
	}
	return "failed"
}

// Fake replies exercise only schemas. They never inspect checks/gold or imply
// semantic success. Real preparation sends only product prompts to the model.
func tierFakeReply(purpose, prompt string) (string, error) {
	var input struct {
		Memories []struct {
			N int `json:"n"`
		} `json:"memories"`
	}
	_ = json.Unmarshal([]byte(prompt), &input)
	switch purpose {
	case "organize":
		items := []any{}
		for _, m := range input.Memories {
			items = append(items, map[string]any{"n": m.N, "category": "taste", "durable": true, "area": "兴趣"})
		}
		return stringJSON(map[string]any{"items": items, "new": []any{}}), nil
	case "compare":
		return `{"duplicates":[],"superseded":[]}`, nil
	case "entity_compare":
		return `{"same":false,"keep":null}`, nil
	case "card":
		ids := []int{}
		for i, m := range input.Memories {
			if i < 25 {
				ids = append(ids, m.N)
			}
		}
		return stringJSON(map[string]any{"fields": map[string]any{"preference": ids}, "rules": []any{}, "deadlines": []any{}}), nil
	case "handover":
		sections := []any{}
		for _, title := range []string{"他是谁和现在的处境", "怎么跟他配合", "现在手上的事", "时间和节奏", "资源和限制", "口味和标准", "重要的人", "他的叫法", "他看重什么"} {
			sections = append(sections, map[string]any{"title": title, "body": "（暂无依据）", "refs": []int{}})
		}
		return stringJSON(map[string]any{"sections": sections}), nil
	case "selector":
		return `{"groups":["self:taste"]}`, nil
	case "reader":
		return `{"used":[]}`, nil
	case "selfcheck":
		return `{"reply":"假模型输出；只验证流程。","used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":false,"actions":[],"ask":null}`, nil
	case "secretary":
		return "假模型输出；只验证流程。", nil
	}
	return "", fmt.Errorf("unknown fake purpose")
}
