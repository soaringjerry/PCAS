package siwc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type fixture struct {
	t                                        *testing.T
	server                                   *httptest.Server
	key                                      *rsa.PrivateKey
	mu                                       sync.Mutex
	query                                    url.Values
	nonce, client, subject, scope            string
	refresh                                  string
	refreshes, exchanges, responses, revokes int
	refreshError                             string
	revokeStatus                             int
	identityChange                           func(jwt.MapClaims)
	stream                                   string
}

func newFixture(t *testing.T) (*fixture, *Manager) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, key: key, subject: "subject-1", scope: requestedScopes, refresh: "refresh-0", revokeStatus: 200, stream: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"PCAS\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	m, err := New(t.TempDir(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	m.issuer = f.server.URL
	m.api = f.server.URL + "/v1"
	t.Cleanup(m.Close)
	return f, m
}
func (f *fixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	write := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		write(discovery{f.server.URL, f.server.URL + "/authorize", f.server.URL + "/token", f.server.URL + "/revoke", f.server.URL + "/jwks"})
	case "/jwks":
		write(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "key-1", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}})
	case "/token":
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			f.t.Error("invalid token transport")
		}
		_ = r.ParseForm()
		if r.Form.Get("client_id") != f.client || r.Form.Get("resource") != resource || r.Form.Get("client_secret") != "" {
			f.t.Error("incorrect client/resource or unexpected secret")
		}
		if r.Form.Get("grant_type") == "authorization_code" {
			f.exchanges++
			challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(challenge[:]) != f.query.Get("code_challenge") || r.Form.Get("redirect_uri") != f.query.Get("redirect_uri") {
				f.t.Error("PKCE or callback did not match")
			}
		} else {
			f.refreshes++
			if r.Form.Get("scope") != "" {
				f.t.Error("refresh changed requested scope")
			}
			if f.refreshError != "" {
				status := 400
				if f.refreshError == "temporarily_unavailable" {
					status = 503
				}
				w.WriteHeader(status)
				write(map[string]string{"error": f.refreshError})
				return
			}
			if r.Form.Get("refresh_token") != f.refresh {
				f.t.Error("rotating refresh token reused")
			}
			f.refresh = fmt.Sprintf("refresh-%d", f.refreshes)
		}
		claims := jwt.MapClaims{"iss": f.server.URL, "sub": f.subject, "aud": f.client, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": f.nonce, "email": "same@example.invalid"}
		if f.identityChange != nil {
			f.identityChange(claims)
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "key-1"
		signed, err := token.SignedString(f.key)
		if err != nil {
			f.t.Error(err)
		}
		write(tokenResponse{Access: "access-PRIVATE", Refresh: f.refresh, ID: signed, Type: "Bearer", Scope: f.scope, Expires: 3600})
	case "/v1/models":
		if r.Header.Get("Authorization") != "Bearer access-PRIVATE" {
			f.t.Error("catalog bearer missing")
		}
		write(map[string]any{"models": []Model{{Slug: "hidden", Visibility: "hidden"}, {Slug: "first", Name: "First model", Visibility: "list"}, {Slug: "second", Name: "Second model", Visibility: "list"}}})
	case "/v1/responses":
		f.responses++
		if r.Header.Get("Authorization") != "Bearer access-PRIVATE" {
			f.t.Error("inference bearer missing")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			f.t.Error("body invalid")
		}
		if len(body) != 5 || body["stream"] != true || body["store"] != false || body["model"] == "" {
			f.t.Errorf("unsupported request body: %v", body)
		}
		if _, ok := body["input"].([]any); !ok {
			f.t.Error("input must be an array")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-request-id", "req-1")
		fmt.Fprint(w, f.stream)
	case "/revoke":
		f.revokes++
		_ = r.ParseForm()
		if r.Form.Get("token") != f.refresh || r.Form.Get("client_id") != f.client || r.Form.Get("token_type_hint") != "refresh_token" {
			f.t.Error("incorrect revocation")
		}
		w.WriteHeader(f.revokeStatus)
	default:
		http.NotFound(w, r)
	}
}
func begin(t *testing.T, f *fixture, m *Manager, client string) (Login, url.Values) {
	t.Helper()
	login, err := m.Begin(context.Background(), client, false, true)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(login.AuthorizationURL)
	q := u.Query()
	f.mu.Lock()
	f.query = q
	f.nonce = q.Get("nonce")
	f.client = client
	if client == "" {
		f.client = "oaiapp_registration"
	}
	f.mu.Unlock()
	return login, q
}
func callback(t *testing.T, q url.Values, params url.Values) int {
	t.Helper()
	resp, err := http.Get(q.Get("redirect_uri") + "?" + params.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
func signIn(t *testing.T, f *fixture, m *Manager, client string) {
	t.Helper()
	_, q := begin(t, f, m, client)
	if q.Get("resource") != resource || q.Get("scope") != requestedScopes || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("nonce") == "" || !strings.HasPrefix(q.Get("ext_agent_host_id"), "urn:uuid:") {
		t.Fatal("invalid authorization parameters")
	}
	if client == "" && (q.Get("client_id") != "dynamic_agent_client" || q.Get("agent_name_hint") != "PCAS") {
		t.Fatal("missing dynamic registration")
	}
	if client != "" && q.Get("agent_name_hint") != "" {
		t.Fatal("reauthorization registered again")
	}
	params := url.Values{"state": {q.Get("state")}, "code": {"test-code"}}
	if client == "" {
		params.Set("client_id", "oaiapp_registration")
	}
	if status := callback(t, q, params); status != 200 {
		t.Fatalf("sign-in HTTP %d", status)
	}
}

func TestLifecycleAndDefaultGate(t *testing.T) {
	f, m := newFixture(t)
	ctx := context.Background()
	signIn(t, f, m, "")
	if !m.Available() || m.DefaultReady() {
		t.Fatal("unverified channel became default")
	}
	models, err := m.Models(ctx)
	if err != nil || len(models) != 2 || models[0].Slug != "first" {
		t.Fatalf("account catalog: %v %v", models, err)
	}
	if err = m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := m.Status(ctx)
	if err != nil || status.Accounts[0].Connected || !status.Accounts[0].Verified || m.DefaultReady() {
		t.Fatalf("logout/gate: %+v %v", status, err)
	}
	info, _ := os.Stat(filepath.Join(m.dir, "credentials.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("unsafe credentials")
	}
	d, _ := m.read()
	if d.Accounts[0].AccessToken != "" || d.Accounts[0].RefreshToken != "" || d.Accounts[0].IDToken != "" {
		t.Fatal("logout retained tokens")
	}
	host := d.HostID
	signIn(t, f, m, "oaiapp_registration")
	if !m.DefaultReady() {
		t.Fatal("verified reauthorization is not default ready")
	}
	d, _ = m.read()
	if d.HostID != host {
		t.Fatal("sign-in changed host")
	}
	if err = m.locked(ctx, func(d *diskState) error { d.Accounts[0].Paused = true; return m.save(d) }); err != nil {
		t.Fatal(err)
	}
	if !m.DefaultReady() || m.Available() {
		t.Fatal("quota pause silently changed the preferred channel")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshes != 1 || f.responses != 2 || f.revokes != 1 {
		t.Fatalf("incomplete lifecycle: %+v", f)
	}
}

func TestCallbackChecksAndKeepsActiveAccount(t *testing.T) {
	for _, name := range []string{"state", "missing-client", "dynamic-client", "access-denied", "duplicate-code", "issuer", "expired", "wrong-returning-client"} {
		t.Run(name, func(t *testing.T) {
			f, m := newFixture(t)
			signIn(t, f, m, "")
			prior, _ := m.read()
			client := ""
			if name == "wrong-returning-client" {
				client = "oaiapp_registration"
			}
			_, q := begin(t, f, m, client)
			p := url.Values{"state": {q.Get("state")}, "code": {"test-code"}, "client_id": {"oaiapp_registration"}}
			switch name {
			case "state":
				p.Set("state", "wrong")
			case "missing-client":
				p.Del("client_id")
			case "dynamic-client":
				p.Set("client_id", "dynamic_agent_client")
			case "access-denied":
				p.Set("error", "access_denied")
			case "duplicate-code":
				p.Add("code", "another")
			case "issuer":
				p.Set("iss", "https://attacker.invalid")
			case "expired":
				m.mu.Lock()
				m.pending.expires = time.Now().Add(-time.Second)
				m.mu.Unlock()
			case "wrong-returning-client":
				p.Set("client_id", "oaiapp_other")
			}
			if callback(t, q, p) != 400 {
				t.Fatal("invalid callback accepted")
			}
			after, _ := m.read()
			if after.Accounts[0].Session != prior.Accounts[0].Session {
				t.Fatal("failed login replaced active account")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.exchanges != 1 {
				t.Fatal("unvalidated callback exchanged code")
			}
		})
	}
}
func TestIdentityValidation(t *testing.T) {
	for _, name := range []string{"issuer", "audience", "expiry", "nonce", "subject", "issued-at", "algorithm", "signature"} {
		t.Run(name, func(t *testing.T) {
			f, m := newFixture(t)
			claims := jwt.MapClaims{"iss": f.server.URL, "aud": "oaiapp_registration", "sub": "subject", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "nonce"}
			switch name {
			case "issuer":
				claims["iss"] = "https://attacker.invalid"
			case "audience":
				claims["aud"] = "other"
			case "expiry":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "nonce":
				claims["nonce"] = "wrong"
			case "subject":
				delete(claims, "sub")
			case "issued-at":
				delete(claims, "iat")
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "key-1"
			key := any(f.key)
			if name == "algorithm" {
				token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				key = []byte("not-a-signing-key")
			}
			if name == "signature" {
				other, _ := rsa.GenerateKey(rand.Reader, 2048)
				key = other
			}
			raw, _ := token.SignedString(key)
			if _, err := m.verifyID(context.Background(), raw, "oaiapp_registration", "nonce", false); err == nil {
				t.Fatal("invalid signed identity accepted")
			}
		})
	}
}
func TestPlanConsentAndReturningIdentity(t *testing.T) {
	f, m := newFixture(t)
	f.scope = "openid profile email offline_access"
	signIn(t, f, m, "")
	if m.Available() {
		t.Fatal("identity consent granted inference")
	}
	status, _ := m.Status(context.Background())
	if !status.Accounts[0].Connected || status.Accounts[0].PlanEnabled {
		t.Fatal("identity-only connection lost")
	}
	f.scope = requestedScopes
	f.subject = "another-subject"
	_, q := begin(t, f, m, "oaiapp_registration")
	if callback(t, q, url.Values{"state": {q.Get("state")}, "code": {"test-code"}}) != 400 {
		t.Fatal("returning sign-in changed identity")
	}
	d, _ := m.read()
	if d.Accounts[0].Subject != "subject-1" {
		t.Fatal("wrong identity persisted")
	}
}

func TestSameEmailRegistrationsStaySeparate(t *testing.T) {
	f, m := newFixture(t)
	signIn(t, f, m, "")
	original, _ := m.read()
	_, q := begin(t, f, m, "")
	f.mu.Lock()
	f.client = "oaiapp_other_workspace"
	f.mu.Unlock()
	if callback(t, q, url.Values{"state": {q.Get("state")}, "code": {"test-code"}, "client_id": {"oaiapp_other_workspace"}}) != 200 {
		t.Fatal("second workspace sign-in failed")
	}
	d, err := m.read()
	if err != nil || len(d.Accounts) != 2 || d.Accounts[0].Email != d.Accounts[1].Email || d.Accounts[0].Session != original.Accounts[0].Session || d.Active != "oaiapp_other_workspace" {
		t.Fatal("registrations with same email were merged")
	}
	// A signed-out historical registration does not prevent transferring the
	// selected workspace; its credentials must not be copied to the VM.
	if err = m.locked(context.Background(), func(d *diskState) error { d.Accounts[0].clearTokens(); return m.save(d) }); err != nil {
		t.Fatal(err)
	}
	target, err := New(t.TempDir(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	target.issuer = f.server.URL
	target.api = f.server.URL + "/v1"
	if err = target.Import(context.Background(), filepath.Join(m.dir, "credentials.json")); err != nil {
		t.Fatal(err)
	}
	transferred, _ := target.read()
	if len(transferred.Accounts) != 1 || transferred.Active != "oaiapp_other_workspace" {
		t.Fatal("transfer included other accounts")
	}
}

func TestCrossProcessRefreshAndFailureRecovery(t *testing.T) {
	f, m := newFixture(t)
	signIn(t, f, m, "")
	ctx := context.Background()
	if err := m.locked(ctx, func(d *diskState) error { d.Accounts[0].ExpiresAt = time.Now().Add(-time.Minute); return m.save(d) }); err != nil {
		t.Fatal(err)
	}
	m2, err := New(m.dir, "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	m2.issuer = f.server.URL
	m2.api = f.server.URL + "/v1"
	defer m2.Close()
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			target := m
			if i%2 == 0 {
				target = m2
			}
			if _, err := target.access(ctx, false); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	f.mu.Lock()
	count := f.refreshes
	f.refreshError = "temporarily_unavailable"
	f.mu.Unlock()
	if count != 1 {
		t.Fatalf("refreshes raced: %d", count)
	}
	if err = m.Refresh(ctx); err == nil {
		t.Fatal("temporary error ignored")
	}
	d, _ := m.read()
	if d.Accounts[0].RefreshToken != "refresh-1" {
		t.Fatal("temporary failure erased tokens")
	}
	f.mu.Lock()
	f.refreshError = "invalid_grant"
	f.mu.Unlock()
	if err = m.Refresh(ctx); err == nil {
		t.Fatal("terminal refresh error ignored")
	}
	d, _ = m.read()
	if d.Accounts[0].AccessToken != "" || d.Accounts[0].ClientID == "" {
		t.Fatal("terminal recovery lost mapping or kept unusable tokens")
	}
}

func TestEarliestRefreshAndProtectedPaths(t *testing.T) {
	f, m := newFixture(t)
	signIn(t, f, m, "")
	ctx := context.Background()
	if err := m.locked(ctx, func(d *diskState) error {
		d.Accounts[0].EarliestRefreshAt = time.Now().Add(30 * time.Minute).Unix()
		d.Accounts[0].ExpiresAt = time.Now().Add(time.Minute)
		return m.save(d)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.access(ctx, false); err != nil {
		t.Fatal("valid access was blocked before refresh time", err)
	}
	if err := m.Refresh(ctx); err == nil {
		t.Fatal("forced refresh ignored earliest refresh time")
	}
	f.mu.Lock()
	count := f.refreshes
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("early refresh reached provider")
	}
	other := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked-directory")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(link, "127.0.0.1", 0); err == nil {
		t.Fatal("symlink credential directory accepted")
	}
	if err := os.Remove(filepath.Join(m.dir, "credentials.json")); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(other, "secret.json")
	if err := os.WriteFile(secret, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(m.dir, "credentials.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Status(ctx); err == nil {
		t.Fatal("symlink credentials accepted")
	}
}

func TestStreamCompletionAndLimits(t *testing.T) {
	for _, name := range []string{"completed", "interrupted", "incomplete", "late-limit", "explicit-error", "invalid-json"} {
		t.Run(name, func(t *testing.T) {
			f, m := newFixture(t)
			signIn(t, f, m, "")
			prefix := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
			f.mu.Lock()
			switch name {
			case "interrupted":
				f.stream = prefix + "data: [DONE]\n\n"
			case "incomplete":
				f.stream = prefix + "data: {\"type\":\"response.incomplete\"}\n\n"
			case "late-limit":
				f.stream = prefix + "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"subscription_sharing_usage_limit_exceeded\"}}}\n\n"
			case "explicit-error":
				f.stream = prefix + "data: {\"type\":\"error\",\"code\":\"subscription_sharing_usage_unavailable\"}\n\n"
			case "invalid-json":
				f.stream = "data: not json\n\n"
			}
			f.mu.Unlock()
			result, err := m.Generate(context.Background(), "", "instructions", "prompt")
			if name == "completed" {
				if err != nil || result.Text != "PCAS" || result.InputTokens != 2 {
					t.Fatalf("completed result: %+v %v", result, err)
				}
				return
			}
			if err == nil || result.Text != "" {
				t.Fatal("partial result treated as successful")
			}
			if name == "late-limit" {
				d, _ := m.read()
				if !d.Accounts[0].Paused || m.Available() {
					t.Fatal("usage-limit channel not paused")
				}
				e, ok := err.(*ProviderError)
				if !ok || e.RequestID != "req-1" || e.BodyShape != "response.failed" {
					t.Fatal("lost structured terminal error")
				}
			}
		})
	}
}
func TestRevocationFailureAndImportIsolation(t *testing.T) {
	f, m := newFixture(t)
	signIn(t, f, m, "")
	d, _ := m.read()
	sourceHost := d.HostID
	target, err := New(t.TempDir(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	target.issuer = f.server.URL
	target.api = f.server.URL + "/v1"
	before, _ := target.read()
	if err = target.Import(context.Background(), filepath.Join(m.dir, "credentials.json")); err != nil {
		t.Fatal(err)
	}
	after, _ := target.read()
	if after.HostID != before.HostID || after.HostID == sourceHost || after.Accounts[0].Session == d.Accounts[0].Session {
		t.Fatal("transfer overwrote host or session")
	}
	f.mu.Lock()
	f.revokeStatus = 503
	f.mu.Unlock()
	confirmed, err := target.Logout(context.Background(), "")
	if err != nil || confirmed {
		t.Fatal("failed revocation reported success")
	}
	after, _ = target.read()
	if after.Accounts[0].AccessToken != "" || after.Accounts[0].Verified {
		t.Fatal("local logout left tokens or verified failed revocation")
	}
	if err = os.Chmod(filepath.Join(m.dir, "credentials.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = target.Import(context.Background(), filepath.Join(m.dir, "credentials.json")); err == nil {
		t.Fatal("unsafe transfer accepted")
	}
}
func TestErrorDiagnosticsNeverExposeProviderBody(t *testing.T) {
	for _, body := range []string{`{"error":{"code":"subscription_sharing_usage_limit_exceeded","param":"model","message":"PRIVATE"}}`, `{"detail":"PRIVATE"}`, `{"error":"invalid_grant","error_description":"PRIVATE"}`} {
		resp := &http.Response{StatusCode: 403, Header: http.Header{"X-Request-Id": {"req-2"}}, Body: io.NopCloser(strings.NewReader(body))}
		err := responseError(resp)
		e := err.(*ProviderError)
		public, _ := json.Marshal(e)
		if strings.Contains(err.Error(), "PRIVATE") || strings.Contains(string(public), "PRIVATE") || string(e.Body) != body || e.RequestID != "req-2" {
			t.Fatal("diagnostics leaked or were lost")
		}
	}
}
