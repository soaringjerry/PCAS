package postgres

// Independent batch1 acceptance. Oracles were committed before interface reads.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/telegram"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b1Store(t *testing.T) *Store {
	t.Helper()
	u, err := url.Parse(os.Getenv("PCAS_TEST_DATABASE_URL"))
	if err != nil || u == nil || u.Hostname() != "127.0.0.1" || u.Path != "/b1_acceptance" || u.User == nil || u.User.Username() != "b1_test" {
		t.Fatal("batch1 requires its disposable loopback b1_acceptance database; run testdata/phase2/run-go.sh (missing configuration is a failure, never a skip)")
	}
	return testStore(t)
}
func b1Gold(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("../../testdata/phase2/b1-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]json.RawMessage
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return g
}
func b1Text(t *testing.T, fixture, field string) string {
	t.Helper()
	var fixtures map[string]json.RawMessage
	if err := json.Unmarshal(b1Gold(t)["fixtures"], &fixtures); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(fixtures[fixture], &fields); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := json.Unmarshal(fields[field], &text); err != nil || text == "" {
		t.Fatal(fixture, field, err)
	}
	return text
}

type b1Request struct {
	Raw    string
	System string
	Prompt string
}
type b1Fake struct {
	mu       sync.Mutex
	requests []b1Request
	reply    any
}

func b1Model(t *testing.T, s *Store, reply any) *b1Fake {
	t.Helper()
	f := &b1Fake{reply: reply}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		var in struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		req := b1Request{Raw: string(raw)}
		for _, m := range in.Messages {
			if m.Role == "system" {
				req.System += m.Content
			} else {
				req.Prompt += m.Content + "\n"
			}
		}
		f.mu.Lock()
		f.requests = append(f.requests, req)
		reply := f.reply
		f.mu.Unlock()
		secretaryModelReply(w, reply)
	})
	return f
}
func (f *b1Fake) set(reply any) { f.mu.Lock(); defer f.mu.Unlock(); f.reply = reply }
func (f *b1Fake) all() []b1Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]b1Request(nil), f.requests...)
}
func (f *b1Fake) last(t *testing.T) b1Request {
	t.Helper()
	r := f.all()
	if len(r) == 0 {
		t.Fatal("fake service received no actual HTTP request")
	}
	return r[len(r)-1]
}
func b1Contains(t *testing.T, text string, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if !strings.Contains(text, f) {
			t.Errorf("missing contract fragment %q", f)
		}
	}
}
func b1Absent(t *testing.T, text string, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if strings.Contains(text, f) {
			t.Errorf("forbidden contract fragment %q", f)
		}
	}
}
func b1Source(t *testing.T, s *Store, scope memory.Scope, title, text, connector string) memory.Ref {
	t.Helper()
	at := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)
	return mustIngest(t, s, scope, memory.IngestRequest{Connector: connector, ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: title, Text: text, MediaType: "text/plain", ExpressedAt: &at}).Ref
}
func b1Trip(t *testing.T, s *Store, scope memory.Scope) memory.Ref {
	t.Helper()
	return b1Source(t, s, scope, b1Text(t, "trip", "title"), b1Text(t, "trip", "text"), "manual")
}
func b1ZeroClaims(t *testing.T, s *Store, scope memory.Scope) {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM claims WHERE owner_id=$1", string(scope.OwnerID)).Scan(&n); err != nil || n != 0 {
		t.Fatal("fixture must have zero claims", n, err)
	}
}
func b1Delete(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref) {
	t.Helper()
	if err := s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
}
func b1Refs(t *testing.T, s *Store, scope memory.Scope, req string) []memory.Ref {
	t.Helper()
	var data []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var refs []memory.Ref
	if err := json.Unmarshal(data, &refs); err != nil {
		t.Fatal(err)
	}
	return refs
}
func b1HasRef(t *testing.T, refs []memory.Ref, want memory.Ref, present bool) {
	t.Helper()
	n := 0
	for _, r := range refs {
		if r == want {
			n++
		}
	}
	if present && n != 1 || !present && n != 0 {
		t.Errorf("dependency %v count=%d, present=%v", want, n, present)
	}
	seen := map[memory.Ref]bool{}
	for _, r := range refs {
		if seen[r] {
			t.Errorf("duplicate dependency %v", r)
		}
		seen[r] = true
	}
}
func b1Run(t *testing.T, s *Store, scope memory.Scope, agent, prompt string) workspace.Run {
	t.Helper()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "查询成都旅行"})
	task := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task, AgentID: agent, Kind: "ask", Prompt: prompt})
	run := st.Runs[0]
	if agent != "manual" {
		if err := s.runAgentOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		st, err := s.Snapshot(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range st.Runs {
			if r.ID == run.ID {
				run = r
			}
		}
		if run.Status != "done" || run.Output == "" {
			t.Errorf("deputy did not complete normally: %+v", run)
		}
	} else if run.Status != "waiting" {
		t.Errorf("manual run status %q", run.Status)
	}
	return run
}

// Commit is used only to construct pre-existing claims/evidence, never to bypass
// the DeskTurn/requestRun/Undo entry points under acceptance.
func b1Claim(t *testing.T, s *Store, scope memory.Scope, text, nature, confirmation string, sources ...memory.Ref) memory.Ref {
	t.Helper()
	entity := memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}, Name: "用户", Type: "person"}
	claim := memory.Claim{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}}, SubjectID: entity.ID, Predicate: "验收记忆", Value: asJSON(text), Nature: nature, Acquisition: "direct", Confirmation: confirmation}
	in := memory.CommitRequest{RequestID: memory.NewID(), Entities: []memory.Entity{entity}, Claims: []memory.Claim{claim}}
	for _, src := range sources {
		in.Evidence = append(in.Evidence, memory.Evidence{Source: src, Target: claim.Ref, Acquisition: "direct", Stance: "supports"})
	}
	if _, err := s.Commit(context.Background(), scope, in); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(claim.ID), AgentIDs: []string{"model", "manual"}})
	return claim.Ref
}
func b1Active(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, want bool) {
	t.Helper()
	var active bool
	if err := s.pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active')", string(scope.OwnerID), string(ref.ID)).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != want {
		t.Errorf("record %v active=%v, want %v", ref, active, want)
	}
}
func b1Map(t *testing.T, value any) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(asJSON(value), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func b1Outdated(t *testing.T, turn workspace.SecretaryTurn, want bool) {
	t.Helper()
	wire := b1Map(t, turn)
	v, ok := wire["outdated"]
	if want && (!ok || v != true) || !want && ok {
		t.Errorf("outdated wire field=%v present=%v, want=%v", v, ok, want)
	}
}

// JSONB and HTTP round-trips may reorder object keys; every value is compared.
func b1SameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	var left, right any
	if err := json.Unmarshal(asJSON(a), &left); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(asJSON(b), &right); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(left, right)
}
func b1Preserved(t *testing.T, before, after workspace.SecretaryTurn) {
	t.Helper()
	if before.Reply != after.Reply || !b1SameJSON(t, before.Cards, after.Cards) || !b1SameJSON(t, before.Receipts, after.Receipts) {
		t.Errorf("outdated erased or changed user-visible reply/cards/receipts: before=%s after=%s", asJSON(before), asJSON(after))
	}
	b1Outdated(t, after, true)
}

type b1Auth struct{ scope memory.Scope }

func (a b1Auth) Authenticate(*http.Request) (memory.Scope, bool) { return a.scope, true }
func b1HTTP(t *testing.T, s *Store, scope memory.Scope, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	api := httpapi.New(s, s, b1Auth{scope}, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	req := httptest.NewRequest(method, path, strings.NewReader(string(asJSON(body))))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer synthetic-b1-auth")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	return w
}
func b1History(t *testing.T, s *Store, scope memory.Scope, conversation string) workspace.SecretaryTurn {
	t.Helper()
	w := b1HTTP(t, s, scope, "GET", "/v1/desk/turns?conversationId="+conversation, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out workspace.DeskTurnsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Turns) == 0 {
		t.Fatal(err, w.Body.String())
	}
	return out.Turns[0]
}
func b1TurnSource(t *testing.T, s *Store, scope memory.Scope, request string) memory.Ref {
	t.Helper()
	ref := memory.Ref{Kind: memory.SourceKind}
	if err := s.pool.QueryRow(context.Background(), "SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND lower(s.external_id)=lower($2) AND s.connector IN ('desk','capture')", string(scope.OwnerID), request).Scan(&ref.ID, &ref.Version); err != nil {
		t.Fatal(err)
	}
	return ref
}
func b1Correct(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, text string) {
	t.Helper()
	workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: string(ref.ID), Text: text, Reason: "合成资料纠正"})
}
func b1ReceiptAction(t *testing.T, turn workspace.SecretaryTurn, n int) string {
	t.Helper()
	actions := []string{}
	for _, r := range turn.Receipts {
		if r.ActionID != nil {
			actions = append(actions, *r.ActionID)
		}
	}
	if len(actions) <= n {
		t.Fatalf("missing action receipt %d: %+v", n, turn.Receipts)
	}
	return actions[n]
}
func b1Undo(t *testing.T, s *Store, scope memory.Scope, id string) workspace.State {
	t.Helper()
	return workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: id})
}
func b1AssertRetained(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, out workspace.DeskTurnResponse) {
	t.Helper()
	got, err := s.GetSource(context.Background(), scope, source.ID, source.Version)
	if err != nil || got.Source.Text != out.Turn.Text {
		t.Errorf("undo lost original source: %v", err)
	}
	turn := b1History(t, s, scope, out.ConversationID)
	if turn.ID != out.Turn.ID || turn.Text != out.Turn.Text {
		t.Error("undo lost conversation")
	}
}
func b1AskAfterUndo(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, forbidden ...string) {
	t.Helper()
	f.set(`{"reply":"对照回答","actions":[]}`)
	mustTurn(t, s, scope, turnRequest("周五张三方案开会成都是什么安排"))
	b1Absent(t, f.last(t).Prompt, forbidden...)
}
func b1SourceLine(alias string, ref memory.Ref) string {
	return fmt.Sprintf("[%s:%s@%d", alias, ref.ID, ref.Version)
}

// Telegram keeps its production poller/receipt validation. Only the transport
// is redirected to a local fake; no request can reach Telegram or a real model.
type b1LocalTransport struct {
	base      *url.URL
	transport http.RoundTripper
}

func (x b1LocalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	clone.URL = &u
	if r.URL.Hostname() == "api.telegram.org" {
		clone.URL.Scheme = x.base.Scheme
		clone.URL.Host = x.base.Host
	} else if r.URL.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("acceptance blocked external host")
	}
	return x.transport.RoundTrip(clone)
}

type b1Bot struct {
	updates  chan any
	sends    chan string
	answers  chan string
	settings notify.Settings
}

func b1Telegram(t *testing.T, s *Store, scope memory.Scope) *b1Bot {
	t.Helper()
	b := &b1Bot{updates: make(chan any, 8), sends: make(chan string, 8), answers: make(chan string, 8), settings: notify.Settings{Path: filepath.Join(t.TempDir(), "notify.json")}}
	if err := b.settings.SaveTelegram("synthetic-b1-token", "123"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var result any = true
		switch filepath.Base(r.URL.Path) {
		case "getMe":
			result = map[string]any{"id": 991, "is_bot": true}
		case "getUpdates":
			select {
			case u := <-b.updates:
				result = []any{u}
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
				result = []any{}
			}
		case "sendMessage":
			b.sends <- in["text"].(string)
			result = map[string]any{"message_id": 101, "chat": map[string]any{"id": 123, "type": "private"}}
		case "answerCallbackQuery":
			b.answers <- in["text"].(string)
		default:
			t.Error("unexpected fake Telegram call", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = b1LocalTransport{base: u, transport: previous}
	t.Cleanup(func() { http.DefaultTransport = previous })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		telegram.Run(ctx, s, nil, b.settings, scope.OwnerID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("owned Telegram poller failed to stop")
		}
	})
	return b
}
func (b *b1Bot) message(update, id int64, text string) {
	b.updates <- map[string]any{"update_id": update, "message": map[string]any{"message_id": id, "chat": map[string]any{"id": 123, "type": "private"}, "text": text}}
}
func (b *b1Bot) sent(t *testing.T) string {
	t.Helper()
	select {
	case text := <-b.sends:
		return text
	case <-time.After(5 * time.Second):
		t.Fatal("fake Telegram received no sendMessage")
		return ""
	}
}
func (b *b1Bot) binding(t *testing.T) notify.TelegramReceipt {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := b.settings.Read()
		if err != nil {
			t.Fatal(err)
		}
		if len(c.TelegramReceipts) > 0 {
			return c.TelegramReceipts[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no receipt delivery binding")
	return notify.TelegramReceipt{}
}
func (b *b1Bot) undo(t *testing.T, action string) {
	t.Helper()
	b.updates <- map[string]any{"update_id": 2, "callback_query": map[string]any{"id": "synthetic-callback", "from": map[string]any{"id": 123}, "data": "u:" + action, "message": map[string]any{"message_id": 101, "chat": map[string]any{"id": 123, "type": "private"}}}}
	select {
	case answer := <-b.answers:
		if answer != "已撤销" {
			t.Error("Telegram undo acknowledgement", answer)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Telegram undo callback not acknowledged")
	}
}

func b1RefusedUndo(t *testing.T, s *Store, scope memory.Scope, action, code string) {
	t.Helper()
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	before := b1DatabaseRows(t, s, false)
	cmd := workspace.Command{Type: "undoAction", ID: action, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
	w := b1HTTP(t, s, scope, "POST", "/v1/workspace/commands", cmd)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"`+code+`"`) {
		t.Errorf("refused undo want 409/%s, got %d %s", code, w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(before, b1DatabaseRows(t, s, false)) {
		t.Error("refused undo mutated persistent business/audit/source/memory data")
	}
}
