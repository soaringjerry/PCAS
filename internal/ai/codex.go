package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Codex owns a private, managed ChatGPT sign-in. No browser cookies, OAuth
// tokens, inherited user config, or credentials are returned to PCAS clients.
// One RPC reader routes replies and notifications; each generation is isolated
// in an ephemeral thread, including concurrent generations.
type Codex struct {
	binary, home, scratch string
	mu                    sync.Mutex
	writeMu               sync.Mutex
	cmd                   *exec.Cmd
	input                 io.WriteCloser
	done                  chan struct{}
	sequence              uint64
	pending               map[string]chan rpcMessage
	watchers              map[uint64]chan rpcMessage
	err                   error
}
type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewCodex(binary, home string) (*Codex, error) {
	if home == "" {
		return nil, nil
	}
	if binary == "" {
		binary = "codex"
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if abs == "/" {
		return nil, fmt.Errorf("PCAS_CODEX_HOME must be a dedicated directory")
	}
	scratch := filepath.Join(abs, "pcas-workspace")
	if err := os.MkdirAll(scratch, 0700); err != nil {
		return nil, err
	}
	return &Codex{binary: binary, home: abs, scratch: scratch}, nil
}
func (c *Codex) start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil && c.err == nil {
		return nil
	}
	cmd := exec.Command(c.binary, "app-server", "--listen", "stdio://",
		"-c", `forced_login_method="chatgpt"`, "-c", `cli_auth_credentials_store="file"`,
		"-c", `web_search="disabled"`, "-c", "features.shell_tool=false", "-c", "features.unified_exec=false",
		"-c", "features.apps=false", "-c", "features.multi_agent=false", "-c", "features.remote_plugin=false",
		"-c", "features.hooks=false", "-c", "features.memories=false", "-c", "tools.view_image=false",
		"-c", "project_doc_max_bytes=0", "-c", "history.persistence=\"none\"")
	cmd.Dir = c.scratch
	// Explicit allowlist: PCAS database/password/API configuration is never inherited.
	for _, name := range []string{"PATH", "HOME", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY"} {
		if v := os.Getenv(name); v != "" {
			cmd.Env = append(cmd.Env, name+"="+v)
		}
	}
	cmd.Env = append(cmd.Env, "CODEX_HOME="+c.home)
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard // provider logs may include private prompts or credentials
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Codex unavailable; install the configured CLI binary")
	}
	c.cmd = cmd
	c.input = input
	c.done = make(chan struct{})
	c.pending = map[string]chan rpcMessage{}
	c.watchers = map[uint64]chan rpcMessage{}
	c.err = nil
	done := c.done
	go c.read(cmd, output, done)
	return nil
}
func (c *Codex) read(cmd *exec.Cmd, output io.Reader, done chan struct{}) {
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	for scanner.Scan() {
		var message rpcMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		if message.Method != "" && len(message.ID) > 0 {
			// This adapter only produces text; it cannot approve external actions.
			_ = c.write(map[string]any{"id": message.ID, "error": map[string]any{"code": -32601, "message": "PCAS text adapter does not support tool or approval requests"}})
			continue
		}
		c.mu.Lock()
		if len(message.ID) > 0 {
			if ch := c.pending[string(message.ID)]; ch != nil {
				ch <- message
				delete(c.pending, string(message.ID))
			}
		}
		if message.Method != "" {
			for _, ch := range c.watchers {
				select {
				case ch <- message:
				default: // overflow is a failed run, never silently drop completion
					select {
					case <-ch:
					default:
					}
					ch <- rpcMessage{Method: "pcas/overflow"}
				}
			}
		}
		c.mu.Unlock()
	}
	_ = cmd.Wait()
	c.mu.Lock()
	if c.cmd == cmd {
		c.err = errors.New("Codex connection closed")
	}
	close(done)
	c.mu.Unlock()
}
func (c *Codex) write(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	input := c.input
	c.mu.Unlock()
	return json.NewEncoder(input).Encode(value)
}
func (c *Codex) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.sequence++
	key := strconv.FormatUint(c.sequence, 10)
	ch := make(chan rpcMessage, 1)
	c.pending[key] = ch
	done := c.done
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if err := c.write(map[string]any{"id": json.RawMessage(key), "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case result := <-ch:
		if result.Error != nil {
			return nil, fmt.Errorf("Codex %s failed (%d)", method, result.Error.Code)
		}
		return result.Result, nil
	case <-done:
		return nil, errors.New("Codex connection closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// initMu serializes initialization separately from RPC routing.
var codexInitMu sync.Mutex

func (c *Codex) ready(ctx context.Context) error {
	codexInitMu.Lock()
	defer codexInitMu.Unlock()
	c.mu.Lock()
	active := c.cmd != nil && c.err == nil
	c.mu.Unlock()
	if active {
		return nil
	}
	if err := c.start(); err != nil {
		return err
	}
	_, err := c.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "pcas", "title": "PCAS", "version": "1.0.0"}})
	if err == nil {
		err = c.write(map[string]any{"method": "initialized"})
	}
	if err != nil {
		c.Close()
	}
	return err
}
func (c *Codex) Account(ctx context.Context) (json.RawMessage, error) {
	if err := c.ready(ctx); err != nil {
		return nil, err
	}
	return c.call(ctx, "account/read", map[string]bool{"refreshToken": false})
}
func (c *Codex) Login(ctx context.Context) (json.RawMessage, error) {
	if err := c.ready(ctx); err != nil {
		return nil, err
	}
	return c.call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"})
}
func (c *Codex) Logout(ctx context.Context) error {
	if err := c.ready(ctx); err != nil {
		return err
	}
	_, err := c.call(ctx, "account/logout", map[string]any{})
	return err
}
func (c *Codex) Limits(ctx context.Context) (json.RawMessage, error) {
	if err := c.ready(ctx); err != nil {
		return nil, err
	}
	return c.call(ctx, "account/rateLimits/read", map[string]any{})
}
func (c *Codex) Models(ctx context.Context) (json.RawMessage, error) {
	if err := c.ready(ctx); err != nil {
		return nil, err
	}
	return c.call(ctx, "model/list", map[string]any{})
}
func (c *Codex) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil && c.err == nil {
		_ = c.cmd.Process.Kill()
		c.err = errors.New("closed")
	}
}
func (c *Codex) Generate(ctx context.Context, model, system, prompt string) (string, error) {
	text, _, err := c.generate(ctx, model, system, prompt, false, nil, nil)
	return text, err
}

// GenerateWithSearch lets this one thread use the hosted web search tool; the
// process-wide default stays off. It returns the queries the model searched.
func (c *Codex) GenerateWithSearch(ctx context.Context, model, system, prompt string) (string, []string, error) {
	return c.GenerateWithSearchSchema(ctx, model, system, prompt, nil)
}

// GenerateWithSearchSchema constrains only this turn's final message. Callers
// that need plain text or another JSON shape keep using GenerateWithSearch.
func (c *Codex) GenerateWithSearchSchema(ctx context.Context, model, system, prompt string, schema json.RawMessage) (string, []string, error) {
	return c.generate(ctx, model, system, prompt, true, schema, nil)
}

func (c *Codex) Vision(ctx context.Context, model, instruction string, image Image) (string, error) {
	text, _, err := c.generate(ctx, model, "", instruction, false, nil, &image)
	return text, err
}

func (c *Codex) generate(ctx context.Context, model, system, prompt string, web bool, schema json.RawMessage, image *Image) (string, []string, error) {
	if err := c.ready(ctx); err != nil {
		return "", nil, err
	}
	account, err := c.Account(ctx)
	if err != nil {
		return "", nil, err
	}
	var auth struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
	}
	if json.Unmarshal(account, &auth) != nil || auth.Account == nil || auth.Account.Type != "chatgpt" {
		return "", nil, fmt.Errorf("ChatGPT sign-in required")
	}
	params := map[string]any{"cwd": c.scratch, "ephemeral": true, "sandbox": "read-only", "approvalPolicy": "never", "baseInstructions": system, "developerInstructions": "Follow the output format specified in the base instructions. Use the provided context. Do not use tools or inspect local files."}
	if web {
		params["config"] = map[string]any{"web_search": "live"}
		params["developerInstructions"] = "Follow the output format specified in the base instructions. You may use web search for public or current information. Search queries leave this conversation: never put names, numbers or other private details from the provided context into them. Do not use other tools or inspect local files."
	}
	if model != "" {
		params["model"] = model
	}
	data, err := c.call(ctx, "thread/start", params)
	if err != nil {
		return "", nil, err
	}
	var thread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err = json.Unmarshal(data, &thread); err != nil || thread.Thread.ID == "" {
		return "", nil, fmt.Errorf("invalid Codex thread")
	}
	c.mu.Lock()
	c.sequence++
	watchID := c.sequence
	events := make(chan rpcMessage, 1024)
	c.watchers[watchID] = events
	done := c.done
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.watchers, watchID)
		c.mu.Unlock()
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = c.call(cleanup, "thread/archive", map[string]string{"threadId": thread.Thread.ID})
	}()
	turnParams := map[string]any{"threadId": thread.Thread.ID, "input": []any{map[string]string{"type": "text", "text": prompt}}}
	if image != nil {
		turnParams["input"] = []any{map[string]string{"type": "text", "text": prompt}, map[string]string{"type": "image", "url": image.DataURL()}}
	}
	if len(schema) > 0 {
		turnParams["outputSchema"] = schema
	}
	turnData, err := c.call(ctx, "turn/start", turnParams)
	if err != nil {
		return "", nil, err
	}
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(turnData, &turn) != nil || turn.Turn.ID == "" {
		return "", nil, errors.New("invalid Codex turn")
	}
	var output strings.Builder
	var searches []string
	for {
		select {
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, _ = c.call(cleanup, "turn/interrupt", map[string]string{"threadId": thread.Thread.ID, "turnId": turn.Turn.ID})
			cancel()
			return "", nil, ctx.Err()
		case <-done:
			return "", nil, errors.New("Codex connection closed")
		case event := <-events:
			if event.Method == "pcas/overflow" {
				return "", nil, errors.New("Codex event buffer exceeded")
			}
			var p struct {
				ThreadID string `json:"threadId"`
				Item     struct {
					Type  string `json:"type"`
					Text  string `json:"text"`
					Query string `json:"query"`
				} `json:"item"`
				Turn struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			if json.Unmarshal(event.Params, &p) != nil || p.ThreadID != thread.Thread.ID {
				continue
			}
			if event.Method == "item/completed" && p.Item.Type == "webSearch" && p.Item.Query != "" {
				searches = append(searches, p.Item.Query)
			}
			if event.Method == "item/completed" && p.Item.Type == "agentMessage" {
				output.WriteString(p.Item.Text)
				output.WriteByte('\n')
			}
			if event.Method == "turn/completed" {
				if p.Turn.Status != "completed" || strings.TrimSpace(output.String()) == "" {
					return "", nil, errors.New("Codex turn did not complete")
				}
				return strings.TrimSpace(output.String()), searches, nil
			}
		}
	}
}
