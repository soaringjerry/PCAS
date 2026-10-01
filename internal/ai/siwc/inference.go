package siwc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/soaringjerry/PCAS/internal/ai/contextwire"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type Model struct {
	Slug       string `json:"slug"`
	Name       string `json:"display_name"`
	Visibility string `json:"visibility"`
}
type Result struct {
	Text                      string
	InputTokens, OutputTokens int
}

func (m *Manager) models(ctx context.Context, a Account) ([]Model, error) {
	var result struct {
		Models []Model `json:"models"`
	}
	if err := m.getJSON(ctx, m.api+"/models", a.AccessToken, &result); err != nil {
		return nil, err
	}
	out := []Model{}
	for _, model := range result.Models {
		if model.Visibility == "list" && model.Slug != "" {
			out = append(out, model)
		}
	}
	return out, nil
}
func (m *Manager) Models(ctx context.Context) ([]Model, error) {
	a, err := m.access(ctx, false)
	if err != nil {
		return nil, err
	}
	return m.models(ctx, a)
}

// Generate consumes SSE through response.completed. No partial output is ever
// treated as a completed assistant result. This body is independent of API-key
// Responses: no max_output_tokens, previous_response_id or other forbidden keys.
func (m *Manager) Generate(ctx context.Context, model, instructions, prompt string) (Result, error) {
	a, err := m.access(ctx, false)
	if err != nil {
		return Result{}, err
	}
	models, err := m.models(ctx, a)
	if err != nil {
		return Result{}, err
	}
	if model == "" {
		model = a.Model
	}
	if model == "" && len(models) > 0 {
		model = models[0].Slug
	}
	found := false
	for _, entry := range models {
		if entry.Slug == model {
			found = true
		}
	}
	if !found {
		return Result{}, &ProviderError{Status: 400, Code: "subscription_sharing_unsupported_capability", Param: "model"}
	}
	body, _ := json.Marshal(map[string]any{"model": model, "instructions": instructions, "input": []any{map[string]string{"role": "user", "content": prompt}}, "store": false, "stream": true})
	req, err := http.NewRequestWithContext(ctx, "POST", m.api+"/responses", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	resp, err := contextwire.Do(m.http, req, memory.ContextRequestEvent{Protocol: "siwc", Model: model, Endpoint: m.api, Payload: body, ObservationLayer: "serialized_request"})
	if err != nil {
		return Result{}, fmt.Errorf("ChatGPT response interrupted")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = responseError(resp)
		m.pauseOnLimit(ctx, a, err)
		return Result{}, err
	}
	result, err := consumeStream(resp)
	if err != nil {
		m.pauseOnLimit(ctx, a, err)
		return Result{}, err
	}
	// A logout or account switch during generation invalidates the result.
	err = m.locked(ctx, func(d *diskState) error {
		current := d.account(a.ClientID)
		if current == nil || current.Session != a.Session || d.Active != a.ClientID || !current.PlanEnabled() || current.Paused {
			return memory.ErrUnavailable
		}
		current.Generated = true
		if current.Refreshed {
			current.GeneratedAfterRefresh = true
		}
		return m.save(d)
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}
func (m *Manager) pauseOnLimit(ctx context.Context, a Account, err error) {
	e, ok := err.(*ProviderError)
	if !ok || e.Code != "subscription_sharing_usage_limit_exceeded" {
		return
	}
	_ = m.locked(ctx, func(d *diskState) error {
		if current := d.account(a.ClientID); current != nil && current.Session == a.Session {
			current.Paused = true
			return m.save(d)
		}
		return nil
	})
}
func consumeStream(resp *http.Response) (Result, error) {
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 32<<20))
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var result Result
	var data strings.Builder
	var eventName string
	total := 0
	process := func() (bool, error) {
		if data.Len() == 0 {
			return false, nil
		}
		raw := strings.TrimSuffix(data.String(), "\n")
		data.Reset()
		if raw == "[DONE]" {
			return false, nil
		}
		var event struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Code     string `json:"code"`
			Param    string `json:"param"`
			Response struct {
				Error struct{ Code, Param string } `json:"error"`
				Usage struct {
					Input  int `json:"input_tokens"`
					Output int `json:"output_tokens"`
				} `json:"usage"`
				Output []struct {
					Content []struct{ Type, Text string } `json:"content"`
				} `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(raw), &event) != nil {
			return false, fmt.Errorf("invalid ChatGPT stream event")
		}
		if event.Type == "" {
			event.Type = eventName
		}
		switch event.Type {
		case "response.output_text.delta":
			result.Text += event.Delta
		case "response.failed", "response.incomplete", "error":
			code, param := event.Response.Error.Code, event.Response.Error.Param
			if code == "" {
				code, param = event.Code, event.Param
			}
			if code == "" {
				code = "stream_incomplete"
			}
			return false, &ProviderError{Status: resp.StatusCode, Code: code, Param: param, RequestID: requestID(resp), BodyShape: event.Type, Body: json.RawMessage(raw)}
		case "response.completed":
			var final strings.Builder
			for _, out := range event.Response.Output {
				for _, content := range out.Content {
					if content.Type == "output_text" {
						final.WriteString(content.Text)
					}
				}
			}
			if final.Len() > 0 {
				result.Text = final.String()
			}
			result.InputTokens, result.OutputTokens = event.Response.Usage.Input, event.Response.Usage.Output
			if strings.TrimSpace(result.Text) == "" {
				return false, fmt.Errorf("ChatGPT completed without text")
			}
			return true, nil
		}
		return false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line)
		if total > 31<<20 {
			return Result{}, fmt.Errorf("ChatGPT stream exceeded limit")
		}
		if line == "" {
			done, err := process()
			eventName = ""
			if err != nil {
				return Result{}, err
			}
			if done {
				return result, nil
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if scanner.Err() == nil {
		done, err := process()
		if err != nil {
			return Result{}, err
		}
		if done {
			return result, nil
		}
	}
	return Result{}, &ProviderError{Status: resp.StatusCode, Code: "stream_incomplete", RequestID: requestID(resp), BodyShape: "interrupted"}
}

// Import accepts PCAS's protected transfer file, validates its identities and
// credentials against OpenAI, and preserves the destination's own host ID.
// The source process must stop refreshing after transfer (official VM flow).
func (m *Manager) Import(ctx context.Context, path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("cannot open protected transfer file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("transfer file must have owner-only permissions")
	}
	var imported diskState
	if json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&imported) != nil {
		return fmt.Errorf("invalid transfer file")
	}
	if len(imported.Accounts) == 0 || len(imported.Accounts) > 100 {
		return fmt.Errorf("invalid transfer registrations")
	}
	selected := imported.account(imported.Active)
	if selected == nil {
		return fmt.Errorf("invalid transfer active registration")
	}
	// Transfer only the selected registration, as required by the VM flow.
	// Other local accounts may be signed out or managed on another machine.
	imported.Accounts = []Account{*selected}
	seen := map[string]bool{}
	for _, a := range imported.Accounts {
		if a.ClientID == "" || a.ClientID == "dynamic_agent_client" || seen[a.ClientID] || a.Issuer != m.issuer {
			return fmt.Errorf("invalid transferred registration")
		}
		seen[a.ClientID] = true
		if a.IDToken == "" || a.AccessToken == "" {
			return fmt.Errorf("transfer requires signed identity and access token")
		}
		id, err := m.verifyID(ctx, a.IDToken, a.ClientID, "", true)
		if err != nil {
			return err
		}
		if id.Subject != a.Subject {
			return fmt.Errorf("transferred identity mismatch")
		}
		if !a.PlanEnabled() {
			return fmt.Errorf("transferred account lacks plan permission")
		}
		if _, err = m.models(ctx, a); err != nil {
			return err
		}
	}
	if !seen[imported.Active] {
		return fmt.Errorf("invalid transfer active registration")
	}
	return m.locked(ctx, func(d *diskState) error {
		for _, a := range imported.Accounts {
			old := d.account(a.ClientID)
			if old != nil && old.Subject != a.Subject {
				return fmt.Errorf("transfer would replace another identity")
			}
			a.Session = randomValue()
			a.Generated, a.Refreshed, a.GeneratedAfterRefresh = false, false, false
			if old != nil {
				a.Verified = a.Verified || old.Verified
			}
			a.Default = a.Verified && a.PlanEnabled()
			if old == nil {
				d.Accounts = append(d.Accounts, a)
			} else {
				*old = a
			}
		}
		d.Active = imported.Active
		return m.save(d)
	})
}

// Verify exercises the live lifecycle with harmless text, using no PCAS memory.
// A successful revoke marks this registration verified. Sign in again to use it.
func (m *Manager) Verify(ctx context.Context) error {
	if _, err := m.Generate(ctx, "", "Reply with exactly PCAS.", "PCAS"); err != nil {
		return err
	}
	if err := m.Refresh(ctx); err != nil {
		return err
	}
	if _, err := m.Generate(ctx, "", "Reply with exactly PCAS.", "PCAS"); err != nil {
		return err
	}
	confirmed, err := m.Logout(ctx, "")
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("ChatGPT remote revocation not confirmed; disconnect the app in ChatGPT settings")
	}
	return nil
}
