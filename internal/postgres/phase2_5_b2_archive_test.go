package postgres_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func (f *phase25B234Fixture) answerAgent(t *testing.T) string {
	t.Helper()
	f.scope.Team = true
	if _, err := f.store.Snapshot(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	const id = "phase25-b234-fake"
	data, err := json.Marshal(workspace.Agent{ID: id, Name: "虚构验收副手", Channel: id, Enabled: true, MemoryInitialized: true, MemoryKinds: []string{"fact", "preference", "plan", "decision", "intention"}, IncludeInferred: false})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,$2,$3) ON CONFLICT(owner_id,id) DO UPDATE SET document=excluded.document`, f.scope.OwnerID, id, data)
	for _, principal := range []string{id, "agent:" + id} {
		f.exec(t, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,$2 FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.scope.OwnerID, principal)
	}
	return id
}
func TestPhase25B2_X2_12_ImportedUserStatementUsableWithoutConfirmation(t *testing.T) {
	t.Skip("finding F-B2-7")
	f := phase25B2NewFixture(t)
	f.store.SetBlobs(&phase25B2Blobs{data: map[string][]byte{}})
	text := "AcceptanceArchive 虚构档案：白鹭月报先写结论。"
	payload := []map[string]any{{"id": "fictitious-conversation", "title": "虚构白鹭月报对话", "create_time": time.Now().Unix(), "update_time": time.Now().Unix(), "current_node": "u1", "mapping": map[string]any{"u1": map[string]any{"id": "u1", "parent": nil, "children": []string{}, "message": map[string]any{"id": "u1", "author": map[string]any{"role": "user"}, "create_time": time.Now().Unix(), "content": map[string]any{"content_type": "text", "parts": []string{text}}}}}}}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	part, err := zw.Create("conversations.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	imported, err := f.store.ImportArchiveReader(f.ctx, f.scope, "fictitious-chat.zip", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if imported.Imported == 0 {
		t.Fatalf("fake archive imported nothing: %+v", imported)
	}
	// Parsing and extraction are independent of B2. Decode the fictitious ZIP
	// with the public parser, then ingest its normalized user message and seed
	// the ordinary direct extraction result through public Commit.
	batch, err := connectors.DecodeArchive("fictitious-chat.zip", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Records) != 1 || batch.Records[0].Role != "user" {
		t.Fatalf("decoded user records=%+v", batch.Records)
	}
	record := batch.Records[0]
	ingested, err := f.store.Ingest(f.ctx, f.scope, memory.IngestRequest{Connector: "chatgpt", ExternalID: record.ID, ExternalVersion: record.Version, Title: record.Title, Text: record.Text, ExpressedAt: record.ExpressedAt})
	if err != nil {
		t.Fatal(err)
	}
	source := ingested.Ref
	subject := f.entity(t, "person", "虚构人物陆青")
	ref := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
	value, _ := json.Marshal(text)
	eid := memory.NewID()
	// Extraction is outside B2. Seed its ordinary direct/unknown public result
	// against the real imported user source, then test B2 consumers end to end.
	if _, err := f.store.Commit(f.ctx, f.scope, memory.CommitRequest{RequestID: memory.NewID(), Claims: []memory.Claim{{Revision: memory.Revision{Ref: ref, State: "active"}, SubjectID: subject, Predicate: "acceptance_note", Value: value, Nature: "fact", Acquisition: "direct", Confirmation: "unknown", Evidence: []memory.ID{eid}}}, Evidence: []memory.Evidence{{ID: eid, Source: source, Target: ref, Acquisition: "direct", Stance: "supports", Locator: map[string]json.RawMessage{"role": json.RawMessage(`"user"`)}}}}); err != nil {
		t.Fatal(err)
	}
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if m.Trust != "stated" || m.Confirmation != "unknown" {
		t.Errorf("archive trust/legacy=%s/%s", m.Trust, m.Confirmation)
	}
	// First query the unknown item alone, ruling out text de-duplication as
	// a reason for its later absence beside the otherwise identical control.
	soloModel := f.model(t, func(_ *http.Request, _ int, _ phase25B234ModelRequest) phase25B234ModelReply {
		out, _ := json.Marshal(map[string]any{"answer": "虚构本人原话回答。", "used": []string{string(ref.ID)}})
		return phase25B234ModelReply{content: string(out)}
	})
	soloAgent := f.answerAgent(t)
	f.index(t)
	soloAnswer, err := f.store.AnswerDesk(f.ctx, f.scope, soloAgent, "AcceptanceArchive", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(soloAnswer.Used) != 1 {
		t.Errorf("unconfirmed item alone not usable: %+v", soloAnswer.Used)
	}
	soloVisible := false
	for _, request := range soloModel.calls() {
		for _, msg := range request.Messages {
			var content string
			_ = json.Unmarshal(msg.Content, &content)
			if strings.Contains(content, string(ref.ID)) {
				soloVisible = true
			}
		}
	}
	if !soloVisible {
		t.Error("unconfirmed item alone absent from model input")
	}
	// The control differs only in confirmation: same speaker, text, source,
	// locator, acquisition and visibility. This separates the legacy gate from
	// archive parsing, search matching or connector-specific behaviour.
	confirmed := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
	confirmedEvidence := memory.NewID()
	if _, err := f.store.Commit(f.ctx, f.scope, memory.CommitRequest{RequestID: memory.NewID(), Claims: []memory.Claim{{Revision: memory.Revision{Ref: confirmed, State: "active"}, SubjectID: subject, Predicate: "acceptance_note", Value: value, Nature: "fact", Acquisition: "direct", Confirmation: "confirmed", Evidence: []memory.ID{confirmedEvidence}}}, Evidence: []memory.Evidence{{ID: confirmedEvidence, Source: source, Target: confirmed, Acquisition: "direct", Stance: "supports", Locator: map[string]json.RawMessage{"role": json.RawMessage(`"user"`)}}}}); err != nil {
		t.Fatal(err)
	}
	f.index(t)
	agent := f.answerAgent(t)
	model := f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		data, _ := json.Marshal(map[string]any{"answer": "虚构回复：月报先写结论。", "used": []string{string(ref.ID), string(confirmed.ID)}})
		return phase25B234ModelReply{content: string(data)}
	})
	answer, err := f.store.AnswerDesk(f.ctx, f.scope, agent, "AcceptanceArchive", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Used) != 2 {
		t.Errorf("unconfirmed direct memory not usable: %+v", answer.Used)
	}
	contains := false
	controlVisible := false
	for _, r := range model.calls() {
		for _, msg := range r.Messages {
			var content string
			_ = json.Unmarshal(msg.Content, &content)
			if strings.Contains(content, string(ref.ID)) {
				contains = true
			}
			if strings.Contains(content, string(confirmed.ID)) {
				controlVisible = true
			}
		}
	}
	if !controlVisible {
		t.Fatal("confirmed same-source positive control absent from secretary prompt")
	}
	if !contains {
		t.Error("unconfirmed same-source user statement ID absent from secretary prompt")
	}
	f.assertRevisions(t, ref, confirmed)
}

// A disposable, in-memory attachment store permits the real archive reader to
// retain its fictitious ZIP without touching any configured host blob path.
type phase25B2Blobs struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (b *phase25B2Blobs) Put(ctx context.Context, scope memory.Scope, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%x", sha256.Sum256(data))
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = data
	return key, nil
}
func (b *phase25B2Blobs) Open(ctx context.Context, scope memory.Scope, key string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.data[key]
	if !ok {
		return nil, fmt.Errorf("missing fictitious blob")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (b *phase25B2Blobs) Delete(ctx context.Context, scope memory.Scope, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.data, key)
	return nil
}
