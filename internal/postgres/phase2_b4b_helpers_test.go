package postgres

// Independent batch4b acceptance: gold a9488b2 precedes these tests.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b4bSpec[T any](t *testing.T, sequence string) T {
	t.Helper()
	data, err := os.ReadFile("../../testdata/phase2/b4b-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err = json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	var out T
	if err = json.Unmarshal(all[sequence], &out); err != nil {
		t.Fatal(sequence, err)
	}
	return out
}

type b4bLimits struct {
	Body              int    `json:"body_characters"`
	AI                int    `json:"assistant_characters"`
	Overlap           int    `json:"overlap_messages"`
	OverlapCharacters int    `json:"overlap_characters"`
	First             int    `json:"message_index_first"`
	Extractor         int    `json:"extractor"`
	Priority          int    `json:"priority"`
	Confirmation      string `json:"confirmation"`
}
type b4bMessage struct {
	Index int        `json:"message_index"`
	Role  string     `json:"role"`
	Text  string     `json:"text"`
	At    *time.Time `json:"expressed_at"`
}
type b4bEarlier struct {
	Ref  int    `json:"ref"`
	Text string `json:"text"`
}
type b4bInput struct {
	Messages []b4bMessage `json:"messages"`
	Context  []b4bMessage `json:"context_messages"`
	Earlier  []b4bEarlier `json:"earlier_memories"`
}
type b4bArchive struct {
	Root     memory.Ref
	Batch    memory.ID
	Messages []b4bMessage
	Sources  []memory.Ref
}

func b4bHTTP(t *testing.T, s *Store, scope memory.Scope, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	api := httpapi.New(s, s, b1Auth{scope}, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Connectors: s})
	req := httptest.NewRequest(method, path, bytes.NewReader(asJSON(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer synthetic-b4b-auth")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	return w
}
func b4bOperation(t *testing.T, s *Store, scope memory.Scope, batch memory.ID, operation string) connectors.ImportBatch {
	t.Helper()
	w := b4bHTTP(t, s, scope, "POST", "/v1/connectors/imports/"+string(batch)+"/"+operation, nil)
	var out connectors.ImportBatch
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func b4bProgress(t *testing.T, s *Store, scope memory.Scope, batch memory.ID) connectors.ImportBatch {
	t.Helper()
	w := b4bHTTP(t, s, scope, "GET", "/v1/connectors/imports", nil)
	var out struct {
		Items []connectors.ImportBatch `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, item := range out.Items {
		if item.ID == batch {
			return item
		}
	}
	t.Fatal("missing import batch", batch)
	return connectors.ImportBatch{}
}
func b4bMessages(t *testing.T, roles, texts []string) []b4bMessage {
	t.Helper()
	if len(roles) != len(texts) {
		t.Fatal("role/text fixture size mismatch")
	}
	at := b2Anchor(t, "Asia/Shanghai").UTC().Truncate(time.Second)
	first := b4bSpec[b4bLimits](t, "limits").First
	out := make([]b4bMessage, len(texts))
	for n, text := range texts {
		when := at.Add(time.Duration(n) * time.Minute)
		out[n] = b4bMessage{Index: first + n, Role: roles[n], Text: text, At: &when}
	}
	return out
}
func b4bImport(t *testing.T, s *Store, scope memory.Scope, messages []b4bMessage) b4bArchive {
	t.Helper()
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	mapping := map[string]any{}
	for n, m := range messages {
		key := fmt.Sprintf("m%d", n+1)
		var parent any
		if n > 0 {
			parent = fmt.Sprintf("m%d", n)
		}
		mapping[key] = map[string]any{"parent": parent, "message": map[string]any{"id": key, "author": map[string]string{"role": m.Role}, "create_time": m.At.Unix(), "content": map[string]any{"parts": []string{m.Text}}}}
	}
	data := asJSON([]any{map[string]any{"id": string(memory.NewID()), "title": "合成历史对话", "current_node": fmt.Sprintf("m%d", len(messages)), "mapping": mapping}})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "conversations.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("organize", "later"); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	api := httpapi.New(s, s, b1Auth{scope}, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Connectors: s})
	req := httptest.NewRequest("POST", "/v1/connectors/archive", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer synthetic-b4b-auth")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("archive upload: %d %s", w.Code, w.Body.String())
	}
	var imported connectors.Result
	if err = json.Unmarshal(w.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if len(imported.Refs) != 1 {
		t.Fatal("one archive expected", w.Body.String())
	}
	a := b4bArchive{Root: imported.Refs[0], Batch: imported.BatchID, Messages: messages, Sources: make([]memory.Ref, len(messages))}
	// Parse the actual uploaded archive to completion before any organization.
	if err = s.ProcessAttachment(context.Background(), leaseStage(t, s, scope, a.Root, "source.parse")); err != nil {
		t.Fatal(err)
	}
	for n, m := range messages {
		ref := memory.Ref{Kind: memory.SourceKind}
		if err = s.pool.QueryRow(context.Background(), `SELECT v.source_id::text,v.version FROM source_versions v JOIN archive_entries e ON(e.owner_id,e.source_id,e.source_version)=(v.owner_id,v.source_id,v.version) WHERE v.owner_id=$1 AND e.archive_id=$2 AND v.body=$3`, string(scope.OwnerID), string(a.Root.ID), m.Text).Scan(&ref.ID, &ref.Version); err != nil {
			t.Fatal("imported fixture message missing", n, err)
		}
		a.Sources[n] = ref
	}
	b2Equal(t, b4bProgress(t, s, scope, a.Batch).Stored, len(messages))
	b4bOnlyArchiveJobs(t, s, scope, a.Root)
	return a
}
func b4bOnlyArchiveJobs(t *testing.T, s *Store, scope memory.Scope, root memory.Ref) {
	t.Helper()
	b2Exec(t, s, `DELETE FROM memory_jobs WHERE owner_id=$1 AND (stage NOT LIKE 'source.extract%' OR record_id NOT IN(SELECT source_id FROM archive_entries WHERE owner_id=$1 AND archive_id=$2))`, string(scope.OwnerID), string(root.ID))
}
func b4bDrain(t *testing.T, s *Store) {
	t.Helper()
	w := b2Worker(s)
	for n := 0; n < 1000; n++ {
		worked, err := w.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			return
		}
	}
	t.Fatal("conversation queue did not drain within fixture bound")
}
func b4bItem(index int, text, quote, nature string) map[string]any {
	i := b2Item(text, quote, nature)
	i["message_index"] = index
	return i
}
func b4bInputFrom(t *testing.T, request b1Request) b4bInput {
	t.Helper()
	var out b4bInput
	if err := json.Unmarshal([]byte(request.Prompt), &out); err != nil {
		t.Fatal("actual model prompt is not JSON", err, request.Prompt)
	}
	if len(out.Messages) == 0 {
		t.Fatal("model received no conversation messages", request.Prompt)
	}
	return out
}
func b4bModel(t *testing.T, s *Store, reply func(b4bInput) any) *b1Fake {
	t.Helper()
	f := &b1Fake{}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "fixture read", 500)
			return
		}
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err = json.Unmarshal(raw, &body); err != nil {
			t.Error(err)
			http.Error(w, "fixture JSON", 500)
			return
		}
		request := b1Request{Raw: string(raw)}
		for _, m := range body.Messages {
			if m.Role == "system" {
				request.System += m.Content
			} else {
				request.Prompt += m.Content + "\n"
			}
		}
		f.mu.Lock()
		f.requests = append(f.requests, request)
		f.mu.Unlock()
		var input b4bInput
		if err = json.Unmarshal([]byte(request.Prompt), &input); err != nil || len(input.Messages) == 0 {
			t.Error("conversation request missing", err, request.Prompt)
			http.Error(w, "fixture protocol", 500)
			return
		}
		secretaryModelReply(w, reply(input))
	})
	return f
}
func b4bRecord(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, state string, items int) {
	t.Helper()
	var got string
	var count, extractor int
	if err := s.pool.QueryRow(context.Background(), `SELECT state,items,extractor FROM source_extractions WHERE owner_id=$1 AND source_id=$2 AND source_version=$3`, string(scope.OwnerID), string(source.ID), source.Version).Scan(&got, &count, &extractor); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, got, state)
	b2Equal(t, count, items)
	b2Equal(t, extractor, b4bSpec[b4bLimits](t, "limits").Extractor)
}
func b4bEvidence(t *testing.T, s *Store, scope memory.Scope, m workspace.Memory, source memory.Ref, quote string, at *time.Time) {
	t.Helper()
	b2Equal(t, len(m.Sources), 1)
	for _, ref := range m.Sources {
		b2Equal(t, ref.SourceID, string(source.ID))
		b2Equal(t, ref.Version, source.Version)
		b2Equal(t, ref.Excerpt, quote)
	}
	b2Time(t, m, "expressedAt", at)
	b2Confirmation(t, s, scope, m, b4bSpec[b4bLimits](t, "limits").Confirmation)
	b2Equal(t, len(b2Snapshot(t, s, scope).Tasks), 0)
}

// Adapt the approved legacy fixtures to the public queue dispatcher. No
// conversation stage or manifest is manufactured by the test.
func b4bLegacyExtract(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, source memory.Ref, items ...map[string]any) []workspace.Memory {
	t.Helper()
	root := b4bEnableSourceImport(t, s, scope, source)
	b4bOnlyArchiveJobs(t, s, scope, root)
	f.set(map[string]any{"items": items})
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source, "source.extract")); err != nil {
		t.Fatal(err)
	}
	b4bDrain(t, s)
	return b2Snapshot(t, s, scope).Memories
}
func b4bEnableSourceImport(t *testing.T, s *Store, scope memory.Scope, source memory.Ref) memory.Ref {
	t.Helper()
	var rootID, batch string
	if err := s.pool.QueryRow(context.Background(), `SELECT e.archive_id::text,b.id::text FROM archive_entries e JOIN import_batches b ON(b.owner_id,b.archive_id)=(e.owner_id,e.archive_id) WHERE e.owner_id=$1 AND e.source_id=$2 AND e.source_version=$3`, string(scope.OwnerID), string(source.ID), source.Version).Scan(&rootID, &batch); err != nil {
		t.Fatal(err)
	}
	b4bOperation(t, s, scope, memory.ID(batch), "organize")
	return memory.Ref{ID: memory.ID(rootID), Kind: memory.SourceKind}
}

func b4bSized(prefix string, n int) string {
	return prefix + strings.Repeat("字", n-len([]rune(prefix)))
}
