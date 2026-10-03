package siwc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

const requestedScopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
const resource = "https://api.openai.com/v1"

type Manager struct {
	dir, issuer, api, bind string
	port                   int
	http                   *http.Client
	mu                     sync.Mutex
	pending                *attempt
	loginError             string
}
type attempt struct {
	state, nonce, verifier, redirect, client, subject string
	token                                             string
	expires                                           time.Time
	server                                            *http.Server
	used                                              bool
	cancel                                            context.CancelFunc
}
type Login struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}
type AccountView struct {
	ClientID    string `json:"client_id"`
	Email       string `json:"email,omitempty"`
	Connected   bool   `json:"connected"`
	PlanEnabled bool   `json:"plan_enabled"`
	Verified    bool   `json:"verified"`
	Paused      bool   `json:"paused"`
	Model       string `json:"model,omitempty"`
}
type Status struct {
	Accounts     []AccountView `json:"accounts"`
	Active       string        `json:"active_client_id"`
	Pending      bool          `json:"pending"`
	Error        string        `json:"error,omitempty"`
	UsageURL     string        `json:"usage_url"`
	DefaultReady bool          `json:"default_ready"`
}

// New uses fixed official endpoints. Test endpoints are private to this package.
func New(dir, bind string, port int) (*Manager, error) {
	if dir == "" {
		return nil, nil
	}
	if bind == "" {
		bind = "127.0.0.1"
	}
	if bind != "127.0.0.1" && bind != "0.0.0.0" {
		return nil, fmt.Errorf("invalid ChatGPT callback bind address")
	}
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid ChatGPT callback port")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("ChatGPT credential directory must be a real directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, bind: bind, port: port, issuer: "https://auth.openai.com", api: resource, http: &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	err = m.locked(context.Background(), func(d *diskState) error {
		if d.HostID == "" {
			d.HostID = "urn:uuid:" + string(memory.NewID())
			return m.save(d)
		}
		return nil
	})
	return m, err
}
func randomValue() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("system randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	out := Status{Accounts: []AccountView{}, UsageURL: UsageURL}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	// Atomic file replacement makes read-only snapshots safe without holding
	// up workspace reads while another process contacts the refresh endpoint.
	d, err := m.read()
	if err != nil {
		return out, err
	}
	out.Active = d.Active
	for _, a := range d.Accounts {
		out.Accounts = append(out.Accounts, AccountView{a.ClientID, a.Email, a.AccessToken != "", a.PlanEnabled(), a.Verified, a.Paused, a.Model})
	}
	if a := d.account(d.Active); a != nil {
		out.DefaultReady = a.Verified && a.Default
	}
	m.mu.Lock()
	out.Pending = m.pending != nil
	out.Error = m.loginError
	m.mu.Unlock()
	return out, err
}
func (m *Manager) Available() bool {
	status, err := m.Status(context.Background())
	if err != nil {
		return false
	}
	for _, a := range status.Accounts {
		if a.ClientID == status.Active {
			return a.PlanEnabled && !a.Paused
		}
	}
	return false
}
func (m *Manager) DefaultReady() bool {
	status, err := m.Status(context.Background())
	return err == nil && status.DefaultReady
}

// Begin binds the exact loopback callback before returning the authorization URL.
// It never puts access/refresh tokens in browser responses. Retained ID hints are
// sent only by the browser to OpenAI; CLI URLs intentionally omit the hint.
func (m *Manager) Begin(ctx context.Context, clientID string, consent, includeHint bool) (Login, error) {
	var selected Account
	var host string
	if err := m.locked(ctx, func(d *diskState) error {
		host = d.HostID
		if clientID != "" {
			a := d.account(clientID)
			if a == nil {
				return memory.ErrInvalid
			}
			selected = *a
		}
		return nil
	}); err != nil {
		return Login{}, err
	}
	d, err := m.discovery(ctx)
	if err != nil {
		return Login{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		return Login{}, memory.ErrConflict
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(m.bind, strconv.Itoa(m.port)))
	if err != nil {
		return Login{}, fmt.Errorf("ChatGPT callback port is unavailable; use local authorization helper")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	a := &attempt{state: randomValue(), nonce: randomValue(), verifier: randomValue(), redirect: fmt.Sprintf("http://127.0.0.1:%d/auth/callback", port), client: selected.ClientID, subject: selected.Subject, token: d.Token, expires: time.Now().Add(10 * time.Minute)}
	challenge := sha256.Sum256([]byte(a.verifier))
	client := a.client
	if client == "" {
		client = "dynamic_agent_client"
	}
	q := url.Values{"client_id": {client}, "ext_agent_host_id": {host}, "response_type": {"code"}, "redirect_uri": {a.redirect}, "scope": {requestedScopes}, "resource": {resource}, "state": {a.state}, "nonce": {a.nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}
	if a.client == "" {
		q.Set("agent_name_hint", "PCAS")
	} else {
		if includeHint && selected.IDToken != "" {
			q.Set("id_token_hint", selected.IDToken)
		}
		if selected.Email != "" {
			q.Set("login_hint", selected.Email)
		}
	}
	if consent {
		q.Set("prompt", "consent")
	}
	a.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.callback(w, r, a, d.Token) }), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Second, MaxHeaderBytes: 16 << 10}
	m.pending = a
	m.loginError = ""
	deadline, stop := context.WithDeadline(context.Background(), a.expires)
	a.cancel = stop
	go func() { _ = a.server.Serve(listener) }()
	go func() {
		<-deadline.Done()
		if deadline.Err() != context.DeadlineExceeded {
			return
		}
		m.mu.Lock()
		if m.pending == a {
			m.pending = nil
			m.loginError = "ChatGPT 登录已超时，请重试。"
		}
		m.mu.Unlock()
		_ = a.server.Close()
	}()
	return Login{d.Authorize + "?" + q.Encode(), a.expires}, nil
}
func (m *Manager) callback(w http.ResponseWriter, r *http.Request, a *attempt, tokenEndpoint string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	u, _ := url.Parse(a.redirect)
	if r.Method != "GET" || r.URL.Path != u.Path || r.Host != u.Host {
		http.Error(w, "Invalid callback", 400)
		return
	}
	m.mu.Lock()
	if m.pending != a || a.used {
		m.mu.Unlock()
		http.Error(w, "Callback already consumed", 400)
		return
	}
	a.used = true // Consume exactly once; remain pending while validation runs.
	m.mu.Unlock()
	defer func() {
		a.cancel()
		m.mu.Lock()
		if m.pending == a {
			m.pending = nil
		}
		m.mu.Unlock()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = a.server.Shutdown(ctx)
		}()
	}()
	q := r.URL.Query()
	err := error(nil)
	if len(q["state"]) != 1 || time.Now().After(a.expires) || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(a.state)) != 1 {
		err = fmt.Errorf("invalid OAuth state")
	}
	if err == nil && q.Get("iss") != "" && q.Get("iss") != m.issuer {
		err = fmt.Errorf("invalid OAuth issuer")
	}
	if err == nil && q.Get("error") != "" {
		err = fmt.Errorf("OAuth authorization was declined")
	}
	client := a.client
	if err == nil {
		if len(q["client_id"]) > 1 {
			err = fmt.Errorf("invalid issued client ID")
		} else if client == "" {
			client = q.Get("client_id")
			if client == "" || client == "dynamic_agent_client" || len(client) > 512 {
				err = fmt.Errorf("registration did not issue a client ID")
			}
		} else if q.Get("client_id") != "" && q.Get("client_id") != client {
			err = fmt.Errorf("registration mismatch")
		}
	}
	if err == nil && (len(q["code"]) != 1 || q.Get("code") == "") {
		err = fmt.Errorf("missing authorization code")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err == nil {
		var t tokenResponse
		t, err = m.exchange(ctx, tokenEndpoint, url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {q.Get("code")}, "code_verifier": {a.verifier}, "redirect_uri": {a.redirect}, "resource": {resource}})
		if e, ok := err.(*ProviderError); ok && e.Code == "invalid_grant" && a.client == "" {
			// Retain only the issued registration, never an unverified identity.
			_ = m.locked(ctx, func(d *diskState) error {
				if d.account(client) == nil {
					d.Accounts = append(d.Accounts, Account{ClientID: client, Issuer: m.issuer})
					return m.save(d)
				}
				return nil
			})
		}
		if err == nil {
			var id identity
			id, err = m.verifyID(ctx, t.ID, client, a.nonce, false)
			if err == nil && a.subject != "" && a.subject != id.Subject {
				err = fmt.Errorf("selected ChatGPT identity changed")
			}
			if err == nil {
				err = m.locked(ctx, func(d *diskState) error {
					old := d.account(client)
					if old != nil && old.Subject != "" && old.Subject != id.Subject {
						return fmt.Errorf("ChatGPT registration identity mismatch")
					}
					next := Account{ClientID: client, Subject: id.Subject, Issuer: m.issuer, Email: id.Email, Scopes: strings.Fields(t.Scope), AccessToken: t.Access, RefreshToken: t.Refresh, IDToken: t.ID, ExpiresAt: time.Now().Add(time.Duration(t.Expires) * time.Second), EarliestRefreshAt: t.Earliest, Session: randomValue()}
					if hasScope(next.Scopes, "chatgpt.tokens.use.direct") && (t.Access == "" || !hasScope(next.Scopes, "resource.invoke")) {
						return fmt.Errorf("missing ChatGPT plan credential")
					}
					if hasScope(next.Scopes, "offline_access") && t.Refresh == "" {
						return fmt.Errorf("missing renewable credential")
					}
					if old != nil {
						next.Verified = old.Verified
						next.Default = old.Verified && next.PlanEnabled()
						next.Model = old.Model
						*old = next
					} else {
						d.Accounts = append(d.Accounts, next)
					}
					d.Active = client
					return m.save(d)
				})
			}
		}
	}
	if err != nil {
		m.mu.Lock()
		m.loginError = "ChatGPT 登录未完成，请重试并检查账户授权。"
		m.mu.Unlock()
		http.Error(w, "PCAS: ChatGPT sign-in failed. Return to PCAS settings and retry.", 400)
		return
	}
	fmt.Fprintln(w, "PCAS: ChatGPT connected. You may close this window and return to PCAS.")
}

// Complete finishes the pending sign-in from a callback address the owner copied
// out of a browser that cannot reach this host's loopback port. A wrong or stale
// address is rejected without consuming the attempt, so the owner can paste again.
func (m *Manager) Complete(ctx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 8<<10 {
		return memory.ErrInvalid
	}
	m.mu.Lock()
	a := m.pending
	m.mu.Unlock()
	if a == nil {
		return memory.ErrNotFound
	}
	want, _ := url.Parse(a.redirect)
	q := u.Query()
	if u.Scheme != want.Scheme || u.Host != want.Host || u.Path != want.Path || u.User != nil || len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(a.state)) != 1 {
		return memory.ErrInvalid
	}
	r, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return memory.ErrInvalid
	}
	w := &pastedResponse{header: http.Header{}, status: 200}
	m.callback(w, r, a, a.token)
	if w.status != 200 {
		return fmt.Errorf("ChatGPT sign-in was not completed")
	}
	return nil
}

type pastedResponse struct {
	header http.Header
	status int
}

func (p *pastedResponse) Header() http.Header         { return p.header }
func (p *pastedResponse) Write(b []byte) (int, error) { return len(b), nil }
func (p *pastedResponse) WriteHeader(status int)      { p.status = status }
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		m.pending.cancel()
		_ = m.pending.server.Close()
		m.pending = nil
	}
}

func (m *Manager) Select(ctx context.Context, client, model string, resume bool) error {
	return m.locked(ctx, func(d *diskState) error {
		a := d.account(client)
		if a == nil {
			return memory.ErrInvalid
		}
		if model != "" {
			a.Model = model
		}
		if resume {
			a.Paused = false
		}
		d.Active = client
		return m.save(d)
	})
}
func (m *Manager) access(ctx context.Context, force bool) (Account, error) {
	var out Account
	err := m.locked(ctx, func(d *diskState) error {
		a := d.account(d.Active)
		if a == nil || !a.PlanEnabled() || a.Paused {
			return memory.ErrUnavailable
		}
		if force || time.Until(a.ExpiresAt) < 2*time.Minute {
			if a.EarliestRefreshAt > time.Now().Unix() {
				if !force && time.Now().Before(a.ExpiresAt) {
					out = *a
					return nil
				}
				return fmt.Errorf("ChatGPT refresh is not yet allowed; retry after %s", time.Unix(a.EarliestRefreshAt, 0).UTC().Format(time.RFC3339))
			}
			if a.RefreshToken == "" {
				return memory.ErrUnavailable
			}
			discovery, err := m.discovery(ctx)
			if err != nil {
				return err
			}
			t, err := m.exchange(ctx, discovery.Token, url.Values{"grant_type": {"refresh_token"}, "client_id": {a.ClientID}, "refresh_token": {a.RefreshToken}, "resource": {resource}})
			if err != nil {
				if e, ok := err.(*ProviderError); ok && contains([]string{"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused"}, e.Code) {
					a.clearTokens()
					if saveErr := m.save(d); saveErr != nil {
						return saveErr
					}
				}
				return err
			}
			if t.Access == "" || t.Refresh == "" {
				return fmt.Errorf("refresh did not return replacement credentials")
			}
			// Refresh is bound to this client and renewable session. Preserve the
			// originally validated identity token as a login hint; a refresh never
			// replaces identity. This also avoids losing a rotated refresh token
			// if a subsequent JWKS request is temporarily unavailable.
			a.AccessToken, a.RefreshToken = t.Access, t.Refresh
			a.ExpiresAt = time.Now().Add(time.Duration(t.Expires) * time.Second)
			a.EarliestRefreshAt = t.Earliest
			if t.Scope != "" {
				a.Scopes = strings.Fields(t.Scope)
			}
			a.Refreshed = true
			if err := m.save(d); err != nil {
				return err
			}
			if !a.PlanEnabled() {
				return memory.ErrUnavailable
			}
		}
		out = *a
		return nil
	})
	return out, err
}
func (m *Manager) Refresh(ctx context.Context) error { _, err := m.access(ctx, true); return err }

// Logout stops further requests under the cross-process lock, attempts remote
// revocation, then clears tokens while retaining the client/identity mapping.
func (m *Manager) Logout(ctx context.Context, client string) (bool, error) {
	m.Close() // Cancel pending callbacks before another credential write can win.
	confirmed := false
	err := m.locked(ctx, func(d *diskState) error {
		if client == "" {
			client = d.Active
		}
		a := d.account(client)
		if a == nil {
			return memory.ErrInvalid
		}
		a.Paused = true
		if err := m.save(d); err != nil {
			return err
		}
		if a.RefreshToken != "" {
			if config, err := m.discovery(ctx); err == nil {
				for retry := 0; retry < 2; retry++ {
					req, _ := http.NewRequestWithContext(ctx, "POST", config.Revoke, strings.NewReader(url.Values{"token": {a.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {a.ClientID}}.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					resp, err := m.http.Do(req)
					if err == nil {
						_ = resp.Body.Close()
						if resp.StatusCode == 200 {
							confirmed = true
							break
						}
						if resp.StatusCode < 500 {
							break
						}
					}
					select {
					case <-ctx.Done():
					case <-time.After(200 * time.Millisecond):
					}
				}
			}
		}
		if confirmed && a.Generated && a.Refreshed && a.GeneratedAfterRefresh {
			a.Verified = true
		}
		a.clearTokens()
		return m.save(d)
	})
	m.Close()
	return confirmed, err
}
