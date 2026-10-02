package postgres

// Independent acceptance: all semantic expectations were frozen in 01fc058.
// The implementation of I/L must not be used to derive these expectations.
import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type b4Fixture struct {
	Text, Question, At, Role, Nature, Confirmation string
	Active, Abandoned                              string
	InputTokens, OutputTokens                      int
	InputPrice, OutputPrice, Cost                  float64
	Model, PrivateMemory, PrivateSource            string
}
type b4DayCase struct {
	Zone            string
	Instants, Dates []string
}
type b4Golden struct {
	Fixtures map[string]json.RawMessage
	Oracles  map[string]string
}

func b4Gold(t *testing.T) b4Golden {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/phase2/b4-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold b4Golden
	if err := json.Unmarshal(raw, &gold); err != nil {
		t.Fatal(err)
	}
	return gold
}
func b4FixtureFor(t *testing.T, name string) b4Fixture {
	t.Helper()
	var result b4Fixture
	if err := json.Unmarshal(b4Gold(t).Fixtures[name], &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func b4Instant(t *testing.T, text string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return at
}
func b4Store(t *testing.T) *Store {
	t.Helper()
	s := testStore(t)
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	return s
}

type b4Auth struct {
	scope         memory.Scope
	authenticated bool
}

func (a b4Auth) Authenticate(*http.Request) (memory.Scope, bool) { return a.scope, a.authenticated }
func b4API(s *Store, scope memory.Scope, authenticated bool) http.Handler {
	return httpapi.New(s, s, b4Auth{scope, authenticated}, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Connectors: s, Attachments: s})
}
func b4HTTP(t *testing.T, s *Store, scope memory.Scope, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(string(asJSON(body))))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer synthetic-b4-auth")
	w := httptest.NewRecorder()
	b4API(s, scope, true).ServeHTTP(w, r)
	return w
}
func b4Upload(t *testing.T, api http.Handler, endpoint, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, endpoint, &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Authorization", "Bearer synthetic-b4-auth")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w
}
func b4OK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
}
func b4JSON(t *testing.T, raw []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("JSON decode: %v; body=%s", err, raw)
	}
}

type b4Message struct {
	ID, Role, Text string
	At             time.Time
}
type b4Conversation struct {
	ID, Title string
	Messages  []b4Message
	Branch    *b4Message
}

// Build genuine ChatGPT mapping topology, including the empty root node,
// parent/children links, message IDs, author roles and current_node.
func b4Export(conversations []b4Conversation) []byte {
	list := make([]any, 0, len(conversations))
	for _, conv := range conversations {
		mapping := map[string]any{"root": map[string]any{"id": "root", "parent": nil, "children": []string{}, "message": nil}}
		parent := "root"
		for _, msg := range conv.Messages {
			mapping[parent].(map[string]any)["children"] = []string{msg.ID}
			mapping[msg.ID] = map[string]any{"id": msg.ID, "parent": parent, "children": []string{}, "message": map[string]any{"id": msg.ID, "author": map[string]string{"role": msg.Role}, "create_time": float64(msg.At.Unix()), "content": map[string]any{"content_type": "text", "parts": []string{msg.Text}}, "status": "finished_successfully"}}
			parent = msg.ID
		}
		if conv.Branch != nil && len(conv.Messages) >= 2 {
			msg := *conv.Branch
			fork := conv.Messages[len(conv.Messages)-2].ID
			mapping[fork].(map[string]any)["children"] = []string{parent, msg.ID}
			mapping[msg.ID] = map[string]any{"id": msg.ID, "parent": fork, "children": []string{}, "message": map[string]any{"id": msg.ID, "author": map[string]string{"role": msg.Role}, "create_time": float64(msg.At.Unix()), "content": map[string]any{"content_type": "text", "parts": []string{msg.Text}}}}
		}
		first, last := conv.Messages[0].At, conv.Messages[len(conv.Messages)-1].At
		list = append(list, map[string]any{"id": conv.ID, "conversation_id": conv.ID, "title": conv.Title, "create_time": float64(first.Unix()), "update_time": float64(last.Unix()), "current_node": parent, "mapping": mapping})
	}
	return asJSON(list)
}
func b4Zip(t *testing.T, export []byte, media bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	files := map[string][]byte{"conversations.json": export}
	if media {
		files["photo-1.png"] = []byte("synthetic-png")
		files["audio-1.mp3"] = []byte("synthetic-audio")
	}
	for name, content := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func b4Conversations(t *testing.T, prefix string, count int) []b4Conversation {
	t.Helper()
	base := b4Instant(t, b4FixtureFor(t, "recall").At)
	result := make([]b4Conversation, count)
	for n := range count {
		id := fmt.Sprintf("%s-%04d", prefix, n)
		result[n] = b4Conversation{ID: id, Title: "成都历史聊天", Messages: []b4Message{{ID: id + "-u", Role: "user", Text: fmt.Sprintf("合成成都历史消息%s-%04d，蓝塔寄存编号%04d。", prefix, n, n), At: base.Add(time.Duration(n) * time.Hour)}}}
	}
	return result
}

type b4Fake struct {
	mu       sync.Mutex
	requests []b1Request
	reply    string
	status   int
	gate     *b4ModelGate
}

type b4ModelGate struct {
	started      chan b1Request
	release      chan struct{}
	returned     chan struct{}
	unblockOnce  sync.Once
	returnedOnce sync.Once
}

func (g *b4ModelGate) unblock() { g.unblockOnce.Do(func() { close(g.release) }) }
func b4HoldModel(t *testing.T, f *b4Fake) *b4ModelGate {
	t.Helper()
	g := &b4ModelGate{started: make(chan b1Request, 1), release: make(chan struct{}), returned: make(chan struct{})}
	f.mu.Lock()
	f.gate = g
	f.mu.Unlock()
	// Registered after the fake server cleanup, so a failing assertion cannot
	// leave its HTTP handler blocked while httptest waits for it to stop.
	t.Cleanup(g.unblock)
	return g
}

func b4Model(t *testing.T, s *Store) *b4Fake {
	t.Helper()
	f := &b4Fake{reply: `{"reply":"收到。","answer":"收到。","used":[],"actions":[],"items":[],"output":"完成。"}`, status: 200}
	usage := b4FixtureFor(t, "usage")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		status, reply, gate := f.status, f.reply, f.gate
		f.mu.Unlock()
		if gate != nil {
			select {
			case gate.started <- req:
			default:
			}
			select {
			case <-gate.release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != 200 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "synthetic model failure"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": usage.Model, "choices": []any{map[string]any{"message": map[string]string{"content": reply}}}, "usage": map[string]int{"prompt_tokens": usage.InputTokens, "completion_tokens": usage.OutputTokens}})
		if gate != nil {
			gate.returnedOnce.Do(func() { close(gate.returned) })
		}
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "验收假模型", Protocol: "openai", BaseURL: server.URL, Model: usage.Model, MaxOutput: 4096, CostMode: "token", InputPerMillion: usage.InputPrice, OutputPerMillion: usage.OutputPrice}}}})
	return f
}
func (f *b4Fake) set(reply string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply, f.status = reply, status
}
func (f *b4Fake) all() []b1Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]b1Request(nil), f.requests...)
}
func (f *b4Fake) last(t *testing.T) b1Request {
	t.Helper()
	rows := f.all()
	if len(rows) == 0 {
		t.Fatal("fake model received no actual request")
	}
	return rows[len(rows)-1]
}

type b4UsageRow struct {
	ID, Purpose, AgentID, Model string
	TurnID, RunID, JobID        *string
	InputTokens, OutputTokens   int
	Cost                        float64
	MemoryRefs                  []memory.Ref
}

func b4Usage(t *testing.T, s *Store, scope memory.Scope) []b4UsageRow {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), `SELECT id::text,purpose,coalesce(agent_id,''),model,input_tokens,output_tokens,cost,turn_id::text,run_id::text,job_id::text,memory_refs FROM model_usage WHERE owner_id=$1 ORDER BY at,id`, string(scope.OwnerID))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := []b4UsageRow{}
	for rows.Next() {
		var row b4UsageRow
		var refs []byte
		if err := rows.Scan(&row.ID, &row.Purpose, &row.AgentID, &row.Model, &row.InputTokens, &row.OutputTokens, &row.Cost, &row.TurnID, &row.RunID, &row.JobID, &refs); err != nil {
			t.Fatal(err)
		}
		b4JSON(t, refs, &row.MemoryRefs)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
func b4UsageNumbers(t *testing.T, row b4UsageRow) {
	t.Helper()
	gold := b4FixtureFor(t, "usage")
	if row.ID == "" || row.Model != gold.Model || row.InputTokens != gold.InputTokens || row.OutputTokens != gold.OutputTokens || math.Abs(row.Cost-gold.Cost) > 1e-10 {
		t.Errorf("usage numbers/model: %+v; gold=%+v", row, gold)
	}
}
func b4SameRefs(t *testing.T, got, want []memory.Ref) {
	t.Helper()
	left, right := map[memory.Ref]int{}, map[memory.Ref]int{}
	for _, ref := range got {
		left[ref]++
	}
	for _, ref := range want {
		right[ref]++
	}
	if !reflect.DeepEqual(left, right) {
		t.Errorf("usage refs=%v; actual dependencies=%v", got, want)
	}
	for ref, n := range left {
		if n != 1 {
			t.Errorf("duplicated usage ref %v", ref)
		}
	}
}
func b4NoProse(t *testing.T, s *Store, scope memory.Scope, forbidden ...string) {
	t.Helper()
	// to_jsonb(u) scans EVERY table column, including future columns and plan.
	rows, err := s.pool.Query(context.Background(), `SELECT to_jsonb(u)::text FROM model_usage u WHERE owner_id=$1`, string(scope.OwnerID))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			t.Fatal(err)
		}
		count++
		b1Absent(t, text, forbidden...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("vacuous privacy check: no usage rows")
	}
}
func b4Wait(t *testing.T, description string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + description)
}
func b4SourceCount(t *testing.T, s *Store, scope memory.Scope) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM sources WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func b4SourceByText(t *testing.T, s *Store, scope memory.Scope, text string) memory.Ref {
	t.Helper()
	ref := memory.Ref{Kind: memory.SourceKind}
	if err := s.pool.QueryRow(context.Background(), `SELECT source_id::text,version FROM source_versions WHERE owner_id=$1 AND body=$2`, string(scope.OwnerID), text).Scan(&ref.ID, &ref.Version); err != nil {
		t.Fatal(err)
	}
	return ref
}
func b4RoleAndTime(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, role string, at time.Time, branch string) {
	t.Helper()
	var gotRole, gotBranch string
	var gotTime time.Time
	if err := s.pool.QueryRow(context.Background(), `SELECT c.role,c.branch,v.expressed_at FROM source_contexts c JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(c.owner_id,c.source_id,c.source_version) WHERE c.owner_id=$1 AND c.source_id=$2 AND c.source_version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&gotRole, &gotBranch, &gotTime); err != nil {
		t.Fatal(err)
	}
	if gotRole != role || !gotTime.Equal(at) || branch != "" && gotBranch != branch {
		t.Errorf("source metadata role=%q branch=%q at=%s; want %q/%q/%s", gotRole, gotBranch, gotTime, role, branch, at)
	}
}

// Wire adapters follow the coordinator's frozen section 6 at 5865411.
type b4Preview struct {
	Name                                                        string
	Conversations, Messages, FromUser, AlreadyImported, LeftOut int
	Blocked                                                     int
	Earliest, Latest                                            string
	Gaps                                                        []string
}

func b4PreviewFile(t *testing.T, s *Store, scope memory.Scope, filename string, data []byte) b4Preview {
	t.Helper()
	response := b4Upload(t, b4API(s, scope, true), "/v1/connectors/archive/preview", filename, data)
	b4OK(t, response)
	var fields map[string]json.RawMessage
	b4JSON(t, response.Body.Bytes(), &fields)
	if _, ok := fields["blocked"]; !ok {
		t.Error("preview is missing the frozen blocked field")
	}
	var preview b4Preview
	b4JSON(t, response.Body.Bytes(), &preview)
	return preview
}
func b4NonemptyText(t *testing.T, text string) {
	t.Helper()
	if strings.TrimSpace(text) == "" {
		t.Error("missing nonempty human explanation")
	}
}

type b4ImportItem struct {
	ArchiveID                                                string
	ArchiveVersion                                           int
	ID, Name, State                                          string
	Total, Stored, Organized, LeftOut                        int
	Earliest, Latest, ErrorCode, Error, CreatedAt, UpdatedAt string
}

func b4Imports(t *testing.T, s *Store, scope memory.Scope) []b4ImportItem {
	t.Helper()
	w := b4HTTP(t, s, scope, "GET", "/v1/connectors/imports", nil)
	b4OK(t, w)
	var response struct{ Items []b4ImportItem }
	b4JSON(t, w.Body.Bytes(), &response)
	if response.Items == nil {
		t.Fatal("imports must contain items array")
	}
	for _, item := range response.Items {
		if item.ArchiveID == "" || item.ArchiveVersion < 1 {
			t.Errorf("batch lacks frozen archiveId/archiveVersion: %+v", item)
		}
	}
	return response.Items
}
func b4ImportItemFor(t *testing.T, s *Store, scope memory.Scope, id string) b4ImportItem {
	t.Helper()
	for _, item := range b4Imports(t, s, scope) {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("batch %s absent from imports", id)
	return b4ImportItem{}
}
func b4ImportFile(t *testing.T, s *Store, scope memory.Scope, name string, data []byte) (string, memory.Ref) {
	t.Helper()
	w := b4Upload(t, b4API(s, scope, true), "/v1/connectors/archive", name, data)
	b4OK(t, w)
	var result struct{ BatchID string }
	b4JSON(t, w.Body.Bytes(), &result)
	if result.BatchID == "" {
		t.Fatal("archive HTTP response lacks batchId")
	}
	item := b4ImportItemFor(t, s, scope, result.BatchID)
	ref := memory.Ref{ID: memory.ID(item.ArchiveID), Version: item.ArchiveVersion, Kind: memory.SourceKind}
	var persistedID string
	if err := s.pool.QueryRow(context.Background(), `SELECT archive_id::text FROM import_batches WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), result.BatchID).Scan(&persistedID); err != nil {
		t.Fatal(err)
	}
	if persistedID != item.ArchiveID {
		t.Errorf("wire archiveId differs from persistent batch: %s vs %s", item.ArchiveID, persistedID)
	}
	w = b4HTTP(t, s, scope, "GET", fmt.Sprintf("/v1/memory/sources/%s?version=%d", ref.ID, ref.Version), nil)
	b4OK(t, w)
	var original memory.SourceResult
	b4JSON(t, w.Body.Bytes(), &original)
	if !original.Source.HasAttachment {
		t.Error("archive upload did not retain its original attachment")
	}
	return result.BatchID, ref
}
func b4ImportAction(t *testing.T, s *Store, scope memory.Scope, id, action string) b4ImportItem {
	t.Helper()
	w := b4HTTP(t, s, scope, "POST", "/v1/connectors/imports/"+id+"/"+action, nil)
	b4OK(t, w)
	var item b4ImportItem
	b4JSON(t, w.Body.Bytes(), &item)
	if item.ID != id {
		t.Errorf("%s returned wrong batch: %+v", action, item)
	}
	if item.ArchiveID == "" || item.ArchiveVersion < 1 {
		t.Errorf("%s omitted archive deletion target: %+v", action, item)
	}
	return item
}
func b4SmallChunks(t *testing.T, size int) {
	t.Helper()
	old := ImportChunkSize
	ImportChunkSize = size
	t.Cleanup(func() { ImportChunkSize = old })
}

type b4Parsing struct {
	cancel context.CancelFunc
	done   chan error
	job    worker.Job
}

func b4ParseAsync(t *testing.T, s *Store, scope memory.Scope, archive memory.Ref) *b4Parsing {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	running := &b4Parsing{cancel: cancel, done: make(chan error, 1), job: leaseStage(t, s, scope, archive, "source.parse")}
	go func() { running.done <- s.ProcessAttachment(ctx, running.job) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-running.done:
		case <-time.After(10 * time.Second):
			t.Error("owned parser did not stop")
		}
	})
	return running
}

// Read completion once and put it back for cleanup. Errors are surfaced by
// each scenario, rather than silently retried or replaced by another run.
func (p *b4Parsing) await(t *testing.T, allowCancelled bool) {
	t.Helper()
	select {
	case err := <-p.done:
		p.done <- err
		if err != nil && !(allowCancelled && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))) {
			t.Fatal("background parse", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("background parser did not finish")
	}
}
func b4Complete(t *testing.T, s *Store, scope memory.Scope, id string, archive memory.Ref) b4ImportItem {
	t.Helper()
	p := b4ParseAsync(t, s, scope, archive)
	p.await(t, false)
	item := b4ImportItemFor(t, s, scope, id)
	if item.State != "done" || item.Stored != item.Total {
		t.Fatalf("import not complete: %+v", item)
	}
	return item
}
func b4Partial(t *testing.T, s *Store, scope memory.Scope, id string, minStored int) b4ImportItem {
	t.Helper()
	var partial b4ImportItem
	b4Wait(t, "partial import commit", func() bool { partial = b4ImportItemFor(t, s, scope, id); return partial.Stored >= minStored })
	if partial.Stored >= partial.Total || partial.State != "importing" {
		t.Fatalf("did not observe genuinely running partial import: %+v", partial)
	}
	return partial
}
func b4MessagesExactlyOnce(t *testing.T, s *Store, scope memory.Scope, conversations []b4Conversation) {
	t.Helper()
	for _, conversation := range conversations {
		for _, message := range conversation.Messages {
			var count int
			if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM source_versions WHERE owner_id=$1 AND body=$2`, string(scope.OwnerID), message.Text).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Errorf("message %s stored %d times", message.ID, count)
			}
		}
	}
}
func b4QueueWorker(t *testing.T, s *Store) (context.CancelFunc, chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	w := worker.New(s, map[string]worker.Handler{"source.parse": s.ProcessAttachment, "source.chunk": s.ProcessChunks, "source.tokenize": s.ProcessIndex, "source.extract": s.ProcessExtraction, "source.embed": s.ProcessEmbedding, "claim.embed": s.ProcessEmbedding}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go func() {
		for ctx.Err() == nil {
			worked, err := w.RunOnce(ctx)
			if err != nil {
				done <- err
				return
			}
			if !worked {
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Millisecond):
				}
			}
		}
		done <- nil
	}()
	stop := func() {
		cancel()
		select {
		case err := <-done:
			done <- err
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error("owned worker", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("owned worker did not stop")
		}
	}
	t.Cleanup(stop)
	return stop, done
}
