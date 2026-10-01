// Package ai adapts model providers without exposing credentials to clients or
// making their response formats part of canonical memory.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai/contextwire"
	"github.com/soaringjerry/PCAS/internal/ai/siwc"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type Provider struct {
	apiKey               string
	managed              bool
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
	Searches     []string
	Cost         float64
	InputTokens  int
	OutputTokens int
}
type Registry struct {
	// The same assembly events serve bounded persistence and independent
	// capture/barriers. Returning an error cancels before external dispatch.
	ContextObserver    memory.ContextRequestObserver
	SettingsPath       string
	settingsMu         sync.Mutex
	ReloadSubscription bool // Sequential background workers reload the shared managed login per job.
	Config             Configuration
	HTTP               *http.Client
	Codex              *Codex
	ChatGPT            *siwc.Manager
}

func Load(path string, codex *Codex, direct ...*siwc.Manager) (*Registry, error) {
	r := &Registry{SettingsPath: os.Getenv("PCAS_MODEL_SETTINGS_FILE"), HTTP: &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Codex: codex}
	if len(direct) > 0 {
		r.ChatGPT = direct[0]
	}
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
		r.Config.Providers = append(r.Config.Providers, Provider{ID: "chatgpt", Name: "ChatGPT · Codex 登录", Protocol: "codex", Model: "gpt-6.1-sol", MaxOutput: 4096})
	}
	if r.ChatGPT != nil {
		if seen["chatgpt-direct"] {
			return nil, fmt.Errorf("chatgpt-direct is reserved for Sign in with ChatGPT")
		}
		r.Config.Providers = append(r.Config.Providers, Provider{ID: "chatgpt-direct", Name: "ChatGPT · 套餐授权", Protocol: "siwc", MaxOutput: 4096})
	}
	for _, id := range []string{r.Config.Extraction, r.Config.Embedding, r.Config.Transcription} {
		if id != "" {
			if id == r.Config.Extraction && id == "chatgpt" && codex == nil && r.ChatGPT != nil {
				continue
			}
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
		for _, p := range r.Providers() {
			if p.ID == id {
				return p, true
			}
		}
	}
	return Provider{}, false
}
func (r *Registry) Available(id string) bool {
	p, ok := r.Get(id)
	if !ok {
		return false
	}
	return r.providerAvailable(p)
}

func (r *Registry) providerAvailable(p Provider) bool {
	if p.Protocol == "siwc" {
		return r.ChatGPT != nil && r.ChatGPT.Available()
	}
	if p.managed {
		return p.apiKey != ""
	}
	return (p.Protocol == "codex" && r.Codex != nil) || (p.Protocol != "codex" && (p.KeyEnv == "" || os.Getenv(p.KeyEnv) != ""))
}

// Keep legacy provider IDs intact. Only the implicit subscription default moves
// after the direct account has passed the complete live lifecycle verification.
func (r *Registry) ExtractionID() string {
	if r == nil {
		return ""
	}
	if settings, err := r.readSettings(); err == nil && settings.Text != nil && settings.Text.Default {
		return "openai-api"
	}
	p, exists := r.Get(r.Config.Extraction)
	subscriptionDefault := r.Config.Extraction == "" || r.Config.Extraction == "chatgpt" && (!exists || p.Protocol == "codex")
	if subscriptionDefault && r.ChatGPT != nil && r.ChatGPT.DefaultReady() {
		return "chatgpt-direct"
	}
	return r.Config.Extraction
}
func (p Provider) Reserve(input string) float64 {
	if p.Protocol == "codex" || p.Protocol == "siwc" {
		return 0
	}
	// UTF-8 byte count is a conservative input token bound, with framing margin.
	return (float64(len(input)+4096)*p.InputPerMillion + float64(p.MaxOutput)*p.OutputPerMillion) / 1e6
}

// GenerateWithSearch is Generate with web search where the provider offers it
// (the ChatGPT subscription through Codex); other providers answer offline.
func (r *Registry) GenerateWithSearch(ctx context.Context, id, system, prompt string) (Result, error) {
	return r.GenerateWithSearchSchema(ctx, id, system, prompt, nil)
}

// GenerateWithSearchSchema uses Codex's turn-scoped structured output. Other
// providers retain their existing generation request and instruction format.
func (r *Registry) GenerateWithSearchSchema(ctx context.Context, id, system, prompt string, schema json.RawMessage) (Result, error) {
	p, ok := r.Get(id)
	if !ok || p.Protocol != "codex" || !r.providerAvailable(p) {
		return r.Generate(ctx, id, system, prompt)
	}
	if r.ReloadSubscription {
		defer r.Codex.Close()
	}
	text, searches, err := r.Codex.GenerateWithSearchSchema(r.generationContext(ctx, p), p.Model, system, prompt, schema)
	return Result{Text: text, Searches: searches}, err
}

// GenerateSchema keeps generation offline while constraining Codex output.
// Other providers retain their existing generation request/instruction format.
func (r *Registry) GenerateSchema(ctx context.Context, id, system, prompt string, schema json.RawMessage) (Result, error) {
	p, ok := r.Get(id)
	if !ok || p.Protocol != "codex" || !r.providerAvailable(p) {
		return r.Generate(ctx, id, system, prompt)
	}
	if r.ReloadSubscription {
		defer r.Codex.Close()
	}
	text, err := r.Codex.GenerateSchema(r.generationContext(ctx, p), p.Model, system, prompt, schema)
	return Result{Text: text}, err
}

func (r *Registry) Generate(ctx context.Context, id, system, prompt string) (Result, error) {
	p, ok := r.Get(id)
	if !ok || !r.providerAvailable(p) || p.Embedding || p.Transcription {
		return Result{}, memory.ErrUnavailable
	}
	ctx = r.generationContext(ctx, p)
	if p.Protocol == "siwc" {
		result, err := r.ChatGPT.Generate(ctx, p.Model, system, prompt)
		return Result{Text: result.Text, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens}, err
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
	p, _ := r.Get(r.EmbeddingID())
	return r.EmbedProvider(ctx, p, []string{p.EmbeddingQueryPrefix + query})
}
func (r *Registry) Embed(ctx context.Context, texts []string) ([]memory.Embedding, error) {
	p, ok := r.Get(r.EmbeddingID())
	if !ok {
		return nil, memory.ErrUnavailable
	}
	return r.EmbedProvider(ctx, p, texts)
}

// Pin the provider for an entire indexing job while settings may change.
func (r *Registry) EmbedProvider(ctx context.Context, p Provider, texts []string) ([]memory.Embedding, error) {
	if !p.Embedding || !r.providerAvailable(p) {
		return nil, memory.ErrUnavailable
	}
	var result struct {
		Data []struct {
			Index  int       `json:"index"`
			Vector []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := r.call(ctx, p, "/embeddings", map[string]any{"model": p.Model, "input": texts, "encoding_format": "float"}, &result); err != nil {
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
	key := os.Getenv(p.KeyEnv)
	if p.managed {
		key = p.apiKey
	}
	if key != "" {
		if p.Protocol == "anthropic" {
			req.Header.Set("x-api-key", key)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if p.Protocol == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	response, err := contextwire.Do(r.HTTP, req, memory.ContextRequestEvent{ProviderID: p.ID, Protocol: p.Protocol, Model: p.Model, Endpoint: p.BaseURL, Payload: data, ObservationLayer: "serialized_request"})
	if err != nil {
		var networkErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkErr) && networkErr.Timeout() {
			return context.DeadlineExceeded
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		return fmt.Errorf("model provider unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("model provider HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(out)
}
