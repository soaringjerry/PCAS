package ai

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Connection stores credentials only in the private server-side settings file.
type Connection struct {
	BaseURL     string  `json:"base_url"`
	APIKey      string  `json:"api_key,omitempty"`
	Model       string  `json:"model"`
	InputPrice  float64 `json:"input_cny_per_million"`
	OutputPrice float64 `json:"output_cny_per_million"`
	Default     bool    `json:"default"`
}

type connectionFile struct {
	Text      *Connection `json:"text,omitempty"`
	Embedding *Connection `json:"embedding,omitempty"`
	// RetiredDecision accepts the key of the removed desk-routing interface so
	// that an existing settings file still loads. It is never used or written.
	// Remove this field when no deployed settings file contains "decision".
	RetiredDecision json.RawMessage `json:"decision,omitempty"`
}

type ConnectionStatus struct {
	BaseURL       string  `json:"base_url"`
	Model         string  `json:"model"`
	InputPrice    float64 `json:"input_cny_per_million"`
	OutputPrice   float64 `json:"output_cny_per_million"`
	Default       bool    `json:"default"`
	KeyConfigured bool    `json:"key_configured"`
}

func (r *Registry) readSettings() (connectionFile, error) {
	var settings connectionFile
	if r == nil || r.SettingsPath == "" {
		return settings, nil
	}
	f, err := os.Open(r.SettingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	err = d.Decode(&settings)
	settings.RetiredDecision = nil
	return settings, err
}

func roleID(role string) string {
	if role == "text" {
		return "openai-api"
	}
	return "openai-embedding"
}

// Providers reads an atomically replaced file, so API and worker see the same
// settings without mutating shared registry state or restarting either process.
func (r *Registry) Providers() []Provider {
	if r == nil {
		return nil
	}
	settings, err := r.readSettings()
	out := append([]Provider(nil), r.Config.Providers...)
	if err != nil {
		// A damaged credential file must not silently fall back to old keys.
		for i := range out {
			if out[i].ID == "openai-api" || out[i].ID == "openai-embedding" {
				out[i].managed = true
			}
		}
		return out
	}
	for _, entry := range []struct {
		role       string
		connection *Connection
	}{{"text", settings.Text}, {"embedding", settings.Embedding}} {
		if entry.connection == nil {
			continue
		}
		c := entry.connection
		p := Provider{ID: roleID(entry.role), Name: "OpenAI 兼容 API", Protocol: "openai", BaseURL: c.BaseURL, Model: c.Model, MaxOutput: 4096, InputPerMillion: c.InputPrice, OutputPerMillion: c.OutputPrice, Embedding: entry.role == "embedding", apiKey: c.APIKey, managed: true}
		if p.Embedding {
			p.Name = "OpenAI 向量索引"
		}
		found := false
		for i := range out {
			if out[i].ID == p.ID {
				out[i] = p
				found = true
				break
			}
		}
		if !found {
			out = append(out, p)
		}
	}
	return out
}

func (r *Registry) EmbeddingID() string {
	if r == nil {
		return ""
	}
	if settings, err := r.readSettings(); err == nil && settings.Embedding != nil {
		return "openai-embedding"
	}
	return r.Config.Embedding
}

func (r *Registry) ConnectionStatus(role string) ConnectionStatus {
	id := roleID(role)
	p, ok := r.Get(id)
	if !ok {
		p = Provider{BaseURL: "https://api.openai.com/v1", Model: "gpt-6.1-sol", InputPerMillion: 1, OutputPerMillion: 4}
		if role == "embedding" {
			p.Model = "text-embedding-3-small"
			p.InputPerMillion = 0.2
			p.OutputPerMillion = 0
		}
	}
	return ConnectionStatus{BaseURL: p.BaseURL, Model: p.Model, InputPrice: p.InputPerMillion, OutputPrice: p.OutputPerMillion, Default: role == "text" && r.ExtractionID() == id, KeyConfigured: ok && r.Available(id)}
}

func validConnection(c Connection, role string) bool {
	u, err := url.Parse(c.BaseURL)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https") && strings.TrimSpace(c.BaseURL) == c.BaseURL && strings.TrimSpace(c.Model) != "" && len(c.Model) <= 200 && len(c.APIKey) <= 8192 && !strings.ContainsAny(c.APIKey, "\r\n") && c.InputPrice > 0 && c.OutputPrice >= 0 && !math.IsNaN(c.InputPrice+c.OutputPrice) && !math.IsInf(c.InputPrice+c.OutputPrice, 0) && (role == "text" || c.OutputPrice == 0 && !c.Default)
}

// A blank key preserves an existing key only for the same endpoint.
func (r *Registry) SaveConnection(role string, c Connection) error {
	if r == nil || r.SettingsPath == "" {
		return memory.ErrUnavailable
	}
	if role != "text" && role != "embedding" {
		return memory.ErrInvalid
	}
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Model = strings.TrimSpace(c.Model)
	c.APIKey = strings.TrimSpace(c.APIKey)
	if !validConnection(c, role) {
		return memory.ErrInvalid
	}
	r.settingsMu.Lock()
	defer r.settingsMu.Unlock()
	settings, err := r.readSettings()
	if err != nil {
		return err
	}
	if c.APIKey == "" {
		if p, ok := r.Get(roleID(role)); ok && p.BaseURL == c.BaseURL {
			c.APIKey = p.apiKey
			if !p.managed {
				c.APIKey = os.Getenv(p.KeyEnv)
			}
		}
	}
	if c.APIKey == "" {
		return memory.ErrInvalid
	}
	if role == "text" {
		settings.Text = &c
	} else {
		settings.Embedding = &c
	}
	return r.writeSettings(settings)
}

// writeSettings replaces the file atomically; callers hold settingsMu.
func (r *Registry) writeSettings(settings connectionFile) error {
	if err := os.MkdirAll(filepath.Dir(r.SettingsPath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.SettingsPath), ".model-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(settings); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), r.SettingsPath)
}
