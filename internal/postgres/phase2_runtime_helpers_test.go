package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type phase2RTGold struct {
	Records []struct {
		ID, Text, Claim string
		Atoms           []string
	} `json:"records"`
	Cases []struct {
		Query    string
		Required []string
	} `json:"cases"`
}

func phase2RTGoldRead(t *testing.T) phase2RTGold {
	t.Helper()
	body, err := os.ReadFile("../../testdata/phase2/gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold phase2RTGold
	if err := json.Unmarshal(body, &gold); err != nil {
		t.Fatal(err)
	}
	if len(gold.Records) != 3 || len(gold.Cases) != 4 {
		t.Fatal("original independent gold changed")
	}
	return gold
}

func phase2RTReadJSON(t *testing.T, file string, value any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../testdata/phase2", file))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, value); err != nil {
		t.Fatal(err)
	}
}

func phase2RTEvidence(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s=%s", name, body)
	if dir := os.Getenv("PCAS_PHASE2_EVIDENCE_DIR"); dir != "" {
		if !strings.HasPrefix(filepath.Clean(dir), "/tmp/pcas-phase2-c-") {
			t.Fatal("dedicated C evidence directory required")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "__")+"-"+name+".json"), append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

type phase2RTRequest struct {
	Body     []byte
	Path     string
	Received time.Time
}
type phase2RTCapture struct {
	mu       sync.Mutex
	Requests []phase2RTRequest
	Registry *ai.Registry
	Reply    string
	entered  chan struct{}
	release  chan struct{}
}

func phase2RTSetup(t *testing.T) (*Store, memory.Scope, *phase2RTCapture) {
	t.Helper()
	if os.Getenv("PCAS_TEST_DATABASE_URL") == "" {
		t.Fatal("runtime acceptance requires dedicated disposable PCAS_TEST_DATABASE_URL; no skip")
	}
	s, scope := testStore(t), owner()
	c := &phase2RTCapture{Reply: `{"reply":"合成验收回执","used":[],"links":[],"show":[],"remember":false,"actions":[],"ask":null}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			t.Error(err)
			http.Error(w, "capture failed", 500)
			return
		}
		c.mu.Lock()
		c.Requests = append(c.Requests, phase2RTRequest{Body: append([]byte(nil), body...), Path: r.URL.Path, Received: time.Now().UTC()})
		entered, release, reply := c.entered, c.release, c.Reply
		c.entered = nil
		c.release = nil
		c.mu.Unlock()
		if entered != nil {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		// The fake answers the actual requested response schema. A secretary
		// reply and a deputy output carry the same frozen synthetic answer/Used[].
		var request struct {
			Format struct {
				JSONSchema struct {
					Schema struct {
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		if _, deputy := request.Format.JSONSchema.Schema.Properties["output"]; deputy {
			var answer struct {
				Reply string `json:"reply"`
				Used  []any  `json:"used"`
			}
			if err := json.Unmarshal([]byte(reply), &answer); err != nil {
				t.Error(err)
			}
			reply = string(asJSON(map[string]any{"output": answer.Reply, "used": answer.Used}))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}, "usage": map[string]int{"prompt_tokens": 120, "completion_tokens": 20}})
	}))
	t.Cleanup(server.Close)
	c.Registry = &ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "phase2-model", Providers: []ai.Provider{{ID: "phase2-model", Name: "phase2 synthetic model", Protocol: "openai", BaseURL: server.URL, Model: "phase2-synthetic", MaxOutput: 200, CostMode: "free"}}}}
	s.SetModels(c.Registry)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"timezone": "Asia/Shanghai", "autoAccept": false})})
	return s, scope, c
}

func (c *phase2RTCapture) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.Requests) }
func (c *phase2RTCapture) request(t *testing.T, index int) []byte {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.Requests) {
		t.Fatalf("provider request %d absent (actual count=%d)", index, len(c.Requests))
	}
	r := c.Requests[index]
	hash := sha256.Sum256(r.Body)
	phase2RTEvidence(t, "provider-"+strings.ReplaceAll(r.Received.Format("150405.000000000"), ".", "-"), map[string]any{"path": r.Path, "body_base64": base64.StdEncoding.EncodeToString(r.Body), "body_bytes": len(r.Body), "body_sha256": hex.EncodeToString(hash[:]), "body": json.RawMessage(r.Body), "received_at": r.Received, "layer": "synthetic HTTP receive", "third_party_internal_context": "unknown"})
	return append([]byte(nil), r.Body...)
}
func (c *phase2RTCapture) barrier(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	c.mu.Lock()
	c.entered, c.release = entered, release
	c.mu.Unlock()
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	return entered, finish
}

func phase2RTHTTP(t *testing.T, s *Store, scope memory.Scope, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	token := strings.Repeat("phase2-synthetic-token-", 4)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), s.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Editor: s, Continuity: s, Models: s.models})
	req := httptest.NewRequest(method, path, bytes.NewReader(asJSON(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	phase2RTEvidence(t, "http-"+string(memory.NewID()), map[string]any{"method": method, "path": path, "status": w.Code, "request": body, "response_text": w.Body.String(), "response_base64": base64.StdEncoding.EncodeToString(w.Body.Bytes())})
	return w
}

type phase2RTAuthResult struct {
	Authorization memory.SourceAuthorization `json:"authorization"`
	Duplicate     bool                       `json:"duplicate"`
	ActionID      string                     `json:"action_id"`
	Undoable      bool                       `json:"undoable"`
}

func phase2RTAuthorize(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, principal, role string, hard memory.HardScope) (phase2RTAuthResult, memory.SourceAuthorizationRequest) {
	t.Helper()
	selection := memory.Recipient{PrincipalID: principal, Role: role}
	if principal == "manual" {
		selection.Provider = "phase2-model"
	}
	in := memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: source, Recipient: selection, Purpose: memory.KnowledgePurpose, Scope: hard}
	w := phase2RTHTTP(t, s, scope, http.MethodPost, "/v1/memory/sources/"+string(source.ID)+"/authorization", in)
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("formal partial-recipient authorization failed: %d %s", w.Code, w.Body.String())
	}
	var out phase2RTAuthResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Authorization.Recipient.Valid() || out.Authorization.SourceID != source.ID || out.Authorization.Revoked || out.Authorization.Revision != 1 {
		t.Fatalf("canonical authorization receipt invalid: %+v", out)
	}
	return out, in
}

func phase2RTUpdatePolicy(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, prior phase2RTAuthResult, revoke bool) phase2RTAuthResult {
	t.Helper()
	in := memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: source, PolicyID: prior.Authorization.ID, ExpectedPolicyRevision: prior.Authorization.Revision, Recipient: prior.Authorization.Recipient, Purpose: prior.Authorization.Purpose, Scope: prior.Authorization.Scope, Revoke: revoke}
	method := http.MethodPost
	if revoke {
		method = http.MethodDelete
	}
	w := phase2RTHTTP(t, s, scope, method, "/v1/memory/sources/"+string(source.ID)+"/authorization", in)
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("policy mutation failed: %d %s", w.Code, w.Body.String())
	}
	var out phase2RTAuthResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Authorization.Revoked != revoke || out.Authorization.Revision <= prior.Authorization.Revision || out.Authorization.ID != prior.Authorization.ID {
		t.Error("policy mutation lost semantic state or monotonic revision")
	}
	return out
}

type phase2RTRecipientReader interface {
	ContextRecipient(context.Context, memory.Scope, string, string, *memory.Recipient) (memory.Recipient, error)
}
type phase2RTDiagnostics interface {
	ContextAttempts(context.Context, memory.Scope, string) ([]memory.ContextAttempt, error)
	ContextAttemptSnapshot(context.Context, memory.Scope, memory.ID) ([]byte, error)
	CleanupContextAttempts(context.Context, time.Time) error
}

func phase2RTTask(t *testing.T, s *Store, scope memory.Scope, principal, role string, hard memory.HardScope) memory.Scope {
	t.Helper()
	reader, ok := any(s).(phase2RTRecipientReader)
	if !ok {
		t.Fatal("actual product ContextRecipient API missing")
	}
	var selection *memory.Recipient
	if principal == "manual" {
		selection = &memory.Recipient{Provider: "phase2-model"}
	}
	recipient, err := reader.ContextRecipient(context.Background(), scope, principal, role, selection)
	if err != nil {
		t.Fatal(err)
	}
	task := memory.TrustedTaskContext{OwnerID: scope.OwnerID, Recipient: recipient, Purpose: memory.KnowledgePurpose, Scope: hard, View: memory.VersionView{Mode: memory.Remember}, Now: time.Now().UTC(), Timezone: "Asia/Shanghai", MemoryBudget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}, TotalInputTokens: 8000}
	return memory.Scope{OwnerID: scope.OwnerID, PrincipalID: principal, Task: &task}
}

func phase2RTSource(t *testing.T, s *Store, scope memory.Scope) memory.IngestResult {
	t.Helper()
	record := phase2RTGoldRead(t).Records[0]
	return mustIngest(t, s, scope, memory.IngestRequest{Connector: "phase2-runtime-synthetic", ExternalID: record.ID, ExternalVersion: "v1", Title: "成都预约资料", Text: record.Text, MediaType: "text/plain"})
}

func phase2RTAtoms(t *testing.T, value any, atoms []string, present bool) {
	t.Helper()
	data := string(asJSON(value))
	if b, ok := value.([]byte); ok {
		data = string(b)
	}
	for _, atom := range atoms {
		if strings.Contains(data, atom) != present {
			t.Errorf("actual evidence atom %q presence=%t, want %t", atom, !present, present)
		}
	}
}

func phase2RTRecall(t *testing.T, s *Store, scope memory.Scope, query string) memory.RecallResult {
	t.Helper()
	out, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: query, Mode: memory.Remember, Budget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}})
	if err != nil {
		t.Fatal(err)
	}
	phase2RTEvidence(t, "recall-"+string(memory.NewID()), out)
	return out
}
