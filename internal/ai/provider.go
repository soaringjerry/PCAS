// Package ai adapts model providers without exposing credentials to clients or
// making their response formats part of canonical memory.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type Provider struct {
	EmbeddingQueryPrefix string  `json:"embedding_query_prefix,omitempty"`
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	Protocol             string  `json:"protocol"` // openai, responses, anthropic, codex
	BaseURL              string  `json:"base_url,omitempty"`
	KeyEnv               string  `json:"key_env,omitempty"`
	Model                string  `json:"model"`
	InputPerMillion      float64 `json:"input_cny_per_million"`
	OutputPerMillion     float64 `json:"output_cny_per_million"`
	MaxOutput            int     `json:"max_output_tokens"`
	Embedding            bool    `json:"embedding,omitempty"`
	CostMode             string  `json:"cost_mode,omitempty"` // free must be explicitly configured
	Transcription        bool    `json:"transcription,omitempty"`
	AudioPerMinute       float64 `json:"audio_cny_per_minute,omitempty"`
}
type Configuration struct {
	Providers     []Provider `json:"providers"`
	Extraction    string     `json:"extraction_provider"`
	Embedding     string     `json:"embedding_provider"`
	Transcription string     `json:"transcription_provider"`
}
type Result struct {
	Text         string
	Cost         float64
	InputTokens  int
	OutputTokens int
}
type Registry struct {
	ReloadSubscription bool // Sequential background workers reload the shared managed login per job.
	Config             Configuration
	HTTP               *http.Client
	Codex              *Codex
}

func Load(path string, codex *Codex) (*Registry, error) {
	r := &Registry{HTTP: &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Codex: codex}
	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("cannot open PCAS_MODELS_FILE")
		}
		defer file.Close()
		decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&r.Config); err != nil {
			return nil, fmt.Errorf("invalid model configuration: %w", err)
		}
	}
	seen := map[string]bool{}
	for i := range r.Config.Providers {
		p := &r.Config.Providers[i]
		if p.ID == "" || p.ID == "manual" || seen[p.ID] || p.Name == "" || p.Model == "" {
			return nil, fmt.Errorf("model provider requires unique id, name and model")
		}
		seen[p.ID] = true
		if p.MaxOutput == 0 {
			p.MaxOutput = 4096
		}
		if p.MaxOutput < 1 || p.MaxOutput > 65536 {
			return nil, fmt.Errorf("invalid max_output_tokens")
		}
		if p.InputPerMillion < 0 || p.OutputPerMillion < 0 || p.AudioPerMinute < 0 || math.IsNaN(p.InputPerMillion+p.OutputPerMillion+p.AudioPerMinute) || math.IsInf(p.InputPerMillion+p.OutputPerMillion+p.AudioPerMinute, 0) {
			return nil, fmt.Errorf("invalid model prices")
		}
		if p.Embedding && p.Transcription {
			return nil, fmt.Errorf("provider roles must be separate")
		}
		if (p.Embedding || p.Transcription) && p.Protocol != "openai" {
			return nil, fmt.Errorf("embedding/transcription requires openai protocol")
		}
		if p.Transcription && p.AudioPerMinute == 0 && p.CostMode != "free" {
			return nil, fmt.Errorf("configure audio_cny_per_minute or cost_mode=free")
		}
		switch p.Protocol {
		case "openai", "responses", "anthropic":
			if p.InputPerMillion+p.OutputPerMillion+p.AudioPerMinute == 0 && p.CostMode != "free" {
				return nil, fmt.Errorf("configure provider prices or explicitly set cost_mode=free")
			}
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return nil, fmt.Errorf("invalid provider base_url")
			}
		case "codex":
			if p.Embedding || p.Transcription {
				return nil, fmt.Errorf("ChatGPT subscription does not provide embeddings")
			}
		default:
			return nil, fmt.Errorf("unsupported provider protocol %q", p.Protocol)
		}
	}
	if codex != nil && !seen["chatgpt"] {
		r.Config.Providers = append(r.Config.Providers, Provider{ID: "chatgpt", Name: "ChatGPT 订阅", Protocol: "codex", Model: "", MaxOutput: 4096})
	}
	for _, id := range []string{r.Config.Extraction, r.Config.Embedding, r.Config.Transcription} {
		if id != "" {
			if _, ok := r.Get(id); !ok {
				return nil, fmt.Errorf("unknown provider reference")
			}
		}
	}
	if p, ok := r.Get(r.Config.Extraction); ok && (p.Embedding || p.Transcription) {
		return nil, fmt.Errorf("extraction requires a text generation provider")
	}
	if p, ok := r.Get(r.Config.Embedding); ok && (!p.Embedding || p.Protocol != "openai") {
		return nil, fmt.Errorf("embedding provider must support OpenAI-compatible embeddings")
	}
	if p, ok := r.Get(r.Config.Transcription); ok && (!p.Transcription || p.Protocol != "openai" || p.AudioPerMinute < 0) {
		return nil, fmt.Errorf("invalid transcription provider")
	}
	return r, nil
}
func (r *Registry) Get(id string) (Provider, bool) {
	if r != nil {
		for _, p := range r.Config.Providers {
			if p.ID == id {
				return p, true
			}
		}
	}
	return Provider{}, false
}
func (r *Registry) Available(id string) bool {
	p, ok := r.Get(id)
	return ok && ((p.Protocol == "codex" && r.Codex != nil) || (p.Protocol != "codex" && (p.KeyEnv == "" || os.Getenv(p.KeyEnv) != "")))
}
func (p Provider) Reserve(input string) float64 {
	if p.Protocol == "codex" {
		return 0
	}
	// UTF-8 byte count is a conservative input token bound, with framing margin.
	return (float64(len(input)+4096)*p.InputPerMillion + float64(p.MaxOutput)*p.OutputPerMillion) / 1e6
}
func (r *Registry) Generate(ctx context.Context, id, system, prompt string) (Result, error) {
	p, ok := r.Get(id)
	if !ok || !r.Available(id) || p.Embedding || p.Transcription {
		return Result{}, memory.ErrUnavailable
	}
	if p.Protocol == "codex" {
		if r.ReloadSubscription {
			defer r.Codex.Close()
		}
		text, err := r.Codex.Generate(ctx, p.Model, system, prompt)
		return Result{Text: text}, err
	}
	path := "/chat/completions"
	body := map[string]any{"model": p.Model, "messages": []any{map[string]string{"role": "system", "content": system}, map[string]string{"role": "user", "content": prompt}}, "max_completion_tokens": p.MaxOutput, "stream": false}
	if p.Protocol == "responses" {
		path = "/responses"
		body = map[string]any{"model": p.Model, "instructions": system, "input": prompt, "max_output_tokens": p.MaxOutput, "store": false}
	}
	if p.Protocol == "anthropic" {
		path = "/messages"
		body = map[string]any{"model": p.Model, "system": system, "messages": []any{map[string]string{"role": "user", "content": prompt}}, "max_tokens": p.MaxOutput}
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := r.call(ctx, p, path, body, &result); err != nil {
		return Result{}, err
	}
	out := Result{InputTokens: result.Usage.Input + result.Usage.Prompt, OutputTokens: result.Usage.Output + result.Usage.Completion}
	if len(result.Choices) > 0 {
		out.Text = result.Choices[0].Message.Content
	}
	for _, item := range result.Output {
		for _, c := range item.Content {
			if c.Type == "output_text" {
				out.Text += c.Text
			}
		}
	}
	for _, c := range result.Content {
		if c.Type == "text" {
			out.Text += c.Text
		}
	}
	if strings.TrimSpace(out.Text) == "" {
		return Result{}, fmt.Errorf("provider returned no text")
	}
	out.Cost = (float64(out.InputTokens)*p.InputPerMillion + float64(out.OutputTokens)*p.OutputPerMillion) / 1e6
	if out.InputTokens+out.OutputTokens == 0 {
		out.Cost = p.Reserve(system + prompt)
	} // unknown billing must not look free
	return out, nil
}
func (r *Registry) EmbedQuery(ctx context.Context, query string) ([]memory.Embedding, error) {
	p, _ := r.Get(r.Config.Embedding)
	return r.Embed(ctx, []string{p.EmbeddingQueryPrefix + query})
}
func (r *Registry) Embed(ctx context.Context, texts []string) ([]memory.Embedding, error) {
	p, ok := r.Get(r.Config.Embedding)
	if !ok || !r.Available(p.ID) {
		return nil, memory.ErrUnavailable
	}
	var result struct {
		Data []struct {
			Index  int       `json:"index"`
			Vector []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := r.call(ctx, p, "/embeddings", map[string]any{"model": p.Model, "input": texts}, &result); err != nil {
		return nil, err
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("incomplete embeddings")
	}
	out := make([]memory.Embedding, len(texts))
	seen := map[int]bool{}
	dim := 0
	for _, d := range result.Data {
		if d.Index < 0 || d.Index >= len(texts) || seen[d.Index] || len(d.Vector) == 0 || len(d.Vector) > 16000 {
			return nil, fmt.Errorf("invalid embeddings")
		}
		seen[d.Index] = true
		if dim != 0 && dim != len(d.Vector) {
			return nil, fmt.Errorf("inconsistent embedding dimensions")
		}
		dim = len(d.Vector)
		for _, v := range d.Vector {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nil, fmt.Errorf("non-finite embedding")
			}
		}
		out[d.Index] = memory.Embedding{Model: p.ID + ":" + p.Model, Values: d.Vector}
	}
	return out, nil
}
func (r *Registry) call(ctx context.Context, p Provider, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := os.Getenv(p.KeyEnv); key != "" {
		if p.Protocol == "anthropic" {
			req.Header.Set("x-api-key", key)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if p.Protocol == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	response, err := r.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("model provider unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("model provider HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(out)
}
