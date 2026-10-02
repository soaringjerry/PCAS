package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/memory"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"unicode/utf8"
)

type Call struct {
	System       string
	Prompt       string
	Answer       string
	InputTokens  int
	OutputTokens int
}
type VectorCall struct {
	InputTokens int
	Model       string
}
type Observer struct {
	embeddings []VectorCall
	Base       http.RoundTripper
	mu         sync.Mutex
	calls      []Call
}

func (o *Observer) Calls() []Call {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Call(nil), o.calls...)
}
func (o *Observer) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	transport := o.Base
	if transport == nil {
		transport = http.DefaultTransport
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	if strings.HasSuffix(req.URL.Path, "/embeddings") {
		var in struct{ Model string }
		var out struct {
			Usage struct {
				Prompt int `json:"prompt_tokens"`
				Total  int `json:"total_tokens"`
			}
		}
		if err := json.Unmarshal(body, &in); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		tokens := out.Usage.Total
		if tokens == 0 {
			tokens = out.Usage.Prompt
		}
		o.mu.Lock()
		o.embeddings = append(o.embeddings, VectorCall{InputTokens: tokens, Model: in.Model})
		o.mu.Unlock()
		return resp, nil
	}
	if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return resp, nil
	}
	var in struct {
		Messages []struct{ Role, Content string }
	}
	var out struct {
		Choices []struct{ Message struct{ Content string } }
		Usage   struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		}
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("eval: invalid model response")
	}
	c := Call{InputTokens: out.Usage.Prompt, OutputTokens: out.Usage.Completion}
	for _, m := range in.Messages {
		if m.Role == "system" {
			c.System += m.Content
		} else {
			c.Prompt += m.Content
		}
	}
	if len(out.Choices) > 0 {
		c.Answer = out.Choices[0].Message.Content
	}
	o.mu.Lock()
	o.calls = append(o.calls, c)
	o.mu.Unlock()
	return resp, nil
}

// FakeModel uses no gold facts. Answers literally repeat provided context;
// extraction returns a conservative source prefix with no inferred names/dates.
// Its token counts are synthetic. It is a plumbing check, not a quality oracle.
func FakeModel() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			var in struct {
				Model string
				Input []string
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "bad input", 400)
				return
			}
			data := []any{}
			tokens := 0
			for n, text := range in.Input {
				vector := make([]float32, 64)
				vector[0] = 1
				for _, token := range memory.SearchTokens(text) {
					h := fnv.New32a()
					_, _ = h.Write([]byte(token))
					vector[int(h.Sum32()%64)]++
				}
				data = append(data, map[string]any{"index": n, "embedding": vector})
				tokens += (utf8.RuneCountInString(text) + 3) / 4
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]int{"total_tokens": tokens}})
			return
		}
		var in struct {
			Messages []struct{ Role, Content string }
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "bad input", 400)
			return
		}
		prompt := ""
		system := ""
		for _, m := range in.Messages {
			if m.Role == "system" {
				system += m.Content
			} else {
				prompt += m.Content
			}
		}
		var source struct {
			Source string `json:"source"`
		}
		var answer string
		if json.Unmarshal([]byte(prompt), &source) == nil && source.Source != "" {
			runes := []rune(source.Source)
			if len(runes) > 80 {
				runes = runes[:80]
			}
			text := string(runes)
			b, _ := json.Marshal(Extraction{Items: []Item{{Kind: "memory", Text: text, Nature: "fact", Subject: "我", Predicate: "描述", Quote: text, Confidence: 0.2, Acquisition: "inferred", Qualification: "unknown", People: []string{}, Places: []string{}, Organizations: []string{}}}})
			answer = string(b)
		} else {
			context := ContextSections(prompt)
			if context == "" {
				start := strings.Index(prompt, "<资料>\n")
				end := strings.Index(prompt, "\n</资料>")
				if start >= 0 && end > start {
					context = prompt[start+len("<资料>\n") : end]
				}
			}
			b, _ := json.Marshal(map[string]any{"reply": context, "used": []string{}, "actions": []any{}, "remember": false})
			answer = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}, "usage": map[string]int{"prompt_tokens": (utf8.RuneCountInString(system+prompt) + 3) / 4, "completion_tokens": (utf8.RuneCountInString(answer) + 3) / 4}})
	}))
}

func (o *Observer) Embeddings() []VectorCall {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]VectorCall(nil), o.embeddings...)
}
