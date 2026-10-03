package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const m1Notice = "缴费通知：10 月 15 日前交物业费 800 元"
const m1Caption = "帮我记一下；我喜欢喝茶"

type m1Model struct {
	mu                sync.Mutex
	vision, secretary int
	fail              bool
	seen              []string
	extraction        string
}

func m1Setup(t *testing.T) (*Store, memory.Scope, *m1Model) {
	t.Helper()
	s := testStore(t)
	scope := owner()
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	f := &m1Model{}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(body.Messages) < 2 {
			t.Error("missing user input")
			w.WriteHeader(400)
			return
		}
		if len(body.Messages[1].Content) > 0 && body.Messages[1].Content[0] == '[' {
			f.vision++
			if f.fail {
				w.WriteHeader(500)
				return
			}
			secretaryModelReply(w, m1Notice)
			return
		}
		var prompt string
		_ = json.Unmarshal(body.Messages[1].Content, &prompt)
		f.seen = append(f.seen, prompt)
		if strings.HasPrefix(prompt, "{\"adjacent_messages\"") {
			secretaryModelReply(w, f.extraction)
			return
		}
		f.secretary++
		secretaryModelReply(w, fmt.Sprintf(`{"reply":"看到了缴费通知，已安排。","remember":true,"actions":[{"op":"create_task","title":"交物业费 800 元","due":"%d-10-15","remind":"none"}]}`, time.Now().Year()+1))
	}))
	t.Cleanup(model.Close)
	s.SetModels(&ai.Registry{HTTP: model.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "Fake secretary", Protocol: "openai", BaseURL: model.URL, Model: "fake", MaxOutput: 100, CostMode: "free"}}}})
	// A deterministic legacy OCR executable; no installed language packs needed.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tesseract"), []byte("#!/bin/sh\nprintf 'legacy OCR text\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return s, scope, f
}
func m1Upload(t *testing.T, s *Store, scope memory.Scope) memory.Ref {
	t.Helper()
	in, err := s.IngestAttachment(context.Background(), scope, memory.IngestRequest{Connector: "file-import", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "synthetic-notice.png", MediaType: "image/png"}, strings.NewReader("synthetic image"))
	if err != nil {
		t.Fatal(err)
	}
	return in.Ref
}
func m1Parsed(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, want string) {
	t.Helper()
	got, err := s.GetSource(context.Background(), scope, ref.ID, ref.Version)
	if err != nil || len(got.Derived) != 1 {
		t.Fatal(got, err)
	}
	text, err := s.GetSource(context.Background(), scope, got.Derived[0].ID, got.Derived[0].Version)
	if err != nil || text.Source.Representation != want {
		t.Fatal(text, err)
	}
	if want == "vision" && text.Source.Text != m1Notice {
		t.Fatal(text.Source.Text)
	}
	if want == "ocr" && strings.TrimSpace(text.Source.Text) != "legacy OCR text" {
		t.Fatal(text.Source.Text)
	}
	if len(got.Processing) != 1 || got.Processing[0].Method != want {
		t.Fatal("processing method", got.Processing)
	}
}
func m1Usage(t *testing.T, s *Store, scope memory.Scope, want int) {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose='vision'", string(scope.OwnerID)).Scan(&n); err != nil || n != want {
		t.Fatal("vision usage", n, err)
	}
}

func TestM1V1V2ImageSecretaryAndReplay(t *testing.T) {
	for _, caption := range []string{m1Caption, ""} {
		t.Run(fmt.Sprintf("caption=%t", caption != ""), func(t *testing.T) {
			s, scope, f := m1Setup(t)
			ref := m1Upload(t, s, scope)
			req := turnRequest(caption)
			req.Attachments = []memory.Ref{ref}
			out := mustTurn(t, s, scope, req)
			if out.Turn.Text != caption || out.Turn.Reply == "收到，已存进资料" || out.Turn.Reply == "" || len(out.State.Tasks) != 1 || !strings.Contains(out.State.Tasks[0].Due, "10-15") {
				t.Fatal(out)
			}
			wantReceipts := 2
			if caption != "" {
				wantReceipts++
			}
			receipt := out.Turn.Receipts[len(out.Turn.Receipts)-1]
			if len(out.Turn.Receipts) != wantReceipts || receipt.Text != "存了一张图片" || !receipt.Undoable || receipt.SourceID == nil {
				t.Fatal(out.Turn.Receipts)
			}
			f.mu.Lock()
			seen := strings.Join(f.seen, "\n")
			f.mu.Unlock()
			if !strings.Contains(seen, m1Notice) || !strings.Contains(seen, "不是用户原话") {
				t.Fatal("missing labeled image context", seen)
			}
			m1Parsed(t, s, scope, ref, "vision")
			m1Usage(t, s, scope, 1)
			original, _, _, err := s.OpenAttachment(context.Background(), scope, ref.ID, ref.Version)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(original)
			original.Close()
			if string(data) != "synthetic image" {
				t.Fatal("original changed")
			}
			prior := f.secretary
			replay := mustTurn(t, s, scope, req)
			if replay.Turn.ID != out.Turn.ID || f.vision != 1 || f.secretary != prior {
				t.Fatal("replay called model")
			}
			if caption != "" {
				source := b1TurnSource(t, s, scope, req.RequestID)
				got, err := s.GetSource(context.Background(), scope, source.ID, source.Version)
				if err != nil || got.Source.Text != caption {
					t.Fatal("image polluted user evidence", got, err)
				}
			}
		})
	}
}

func TestM1V4V5FailuresRetainImageAndUseCaption(t *testing.T) {
	for _, mode := range []string{"no-vision", "500", "budget"} {
		t.Run(mode, func(t *testing.T) {
			s, scope, f := m1Setup(t)
			ref := m1Upload(t, s, scope)
			if mode == "no-vision" {
				s.models.Config.Providers[0].Protocol = "text-only-fixture"
			} // Text-only channel simulated independently of any real model.
			if mode == "500" {
				f.fail = true
			}
			if mode == "budget" {
				s.models.Config.Providers[0].InputPerMillion = 1000
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 0.001})})
			}
			req := turnRequest("帮我记一下")
			req.Attachments = []memory.Ref{ref}
			out := mustTurn(t, s, scope, req)
			if !strings.Contains(out.Turn.Reply, "图片存好了，但我现在看不了图") {
				t.Fatal(out.Turn)
			}
			if mode != "budget" && len(out.State.Tasks) != 1 {
				t.Fatal("caption action lost")
			}
			retained, _, _, err := s.OpenAttachment(context.Background(), scope, ref.ID, ref.Version)
			if err != nil {
				t.Fatal(err)
			}
			retained.Close()
			m1Parsed(t, s, scope, ref, "ocr")
			m1Usage(t, s, scope, 0)
		})
	}
}

func TestM1V6BackgroundImageUsesVisionOnce(t *testing.T) {
	s, scope, f := m1Setup(t)
	ref := m1Upload(t, s, scope)
	job := leaseStage(t, s, scope, ref, "source.parse")
	if err := s.ProcessAttachment(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	m1Parsed(t, s, scope, ref, "vision")
	m1Usage(t, s, scope, 1)
	if f.vision != 1 {
		t.Fatal(f.vision)
	}
}

func TestM1V7ImageUndoPreservesTaskAndReplay(t *testing.T) {
	s, scope, f := m1Setup(t)
	ref := m1Upload(t, s, scope)
	req := turnRequest("帮我记一下")
	req.Attachments = []memory.Ref{ref}
	out := mustTurn(t, s, scope, req)
	receipt := out.Turn.Receipts[len(out.Turn.Receipts)-1]
	st, err := s.Undo(context.Background(), scope, *receipt.ActionID)
	if err != nil || len(st.Tasks) != 1 {
		t.Fatal("image undo affected task", st, err)
	}
	if _, _, _, err := s.OpenAttachment(context.Background(), scope, ref.ID, ref.Version); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("image survives", err)
	}
	if _, err := s.Undo(context.Background(), scope, *receipt.ActionID); !errors.Is(err, workspace.ErrAlreadyUndone) {
		t.Fatal(err)
	}
	var attachmentContext string
	if err := s.pool.QueryRow(context.Background(), "SELECT coalesce(response->>'attachmentContext','') FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&attachmentContext); err != nil || attachmentContext != "" {
		t.Fatal("image undo retained attachment context", attachmentContext, err)
	}
	before := f.secretary
	replay := mustTurn(t, s, scope, req)
	if f.vision != 1 || f.secretary != before || replay.Turn.ID != out.Turn.ID {
		t.Fatal("undo replay called model")
	}
	if st, err = s.Undo(context.Background(), scope, *out.Turn.Receipts[0].ActionID); err != nil || len(st.Tasks) != 0 {
		t.Fatal("task undo blocked by image undo", err)
	}
	if err := s.CleanupBlobs(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Randomized sequences exercise independent source/task receipts over multiple
// turns, then reverse every reversible operation back to the initial contents.
func TestM1RandomizedReverseUndoRestoresContents(t *testing.T) {
	s, scope, _ := m1Setup(t)
	ctx := context.Background()
	initial, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(20261003))
	var undo []string
	for turn := 0; turn < 8; turn++ {
		req := turnRequest("")
		for n := 1 + rng.Intn(4); n > 0; n-- {
			req.Attachments = append(req.Attachments, m1Upload(t, s, scope))
		}
		out := mustTurn(t, s, scope, req)
		for _, receipt := range out.Turn.Receipts {
			if receipt.Undoable && receipt.ActionID != nil {
				undo = append(undo, *receipt.ActionID)
			}
		}
	}
	var final workspace.State
	for i := len(undo) - 1; i >= 0; i-- {
		final, err = s.Undo(ctx, scope, undo[i])
		if err != nil {
			t.Fatalf("reverse operation %d: %v", i, err)
		}
	}
	if string(asJSON(final.Tasks)) != string(asJSON(initial.Tasks)) || string(asJSON(final.Projects)) != string(asJSON(initial.Projects)) || string(asJSON(final.Ideas)) != string(asJSON(initial.Ideas)) {
		t.Fatal("reverse undo did not restore workspace contents", final)
	}
	var remaining int
	// Existing action audit sources survive task undo. Originals, derivatives and
	// invented empty user evidence must not survive these image-only sequences.
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM memory_records r JOIN sources s ON (s.owner_id,s.id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.state='active' AND s.connector IN ('file-import','attachment-text','desk')", string(scope.OwnerID)).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("original or derived attachment remains", remaining, err)
	}
}

func TestM1V8ImageTextCannotGroundUserMemory(t *testing.T) {
	s, scope, f := m1Setup(t)
	ref := m1Upload(t, s, scope)
	req := turnRequest(m1Caption)
	req.Attachments = []memory.Ref{ref}
	mustTurn(t, s, scope, req)
	source := b1TurnSource(t, s, scope, req.RequestID)
	f.extraction = string(asJSON(map[string]any{"items": []any{
		map[string]any{"kind": "memory", "text": "要交物业费 800 元", "nature": "fact", "subject": "我", "predicate": "缴费", "quote": m1Notice, "confidence": 0.99, "acquisition": "direct", "qualification": "asserted"},
		map[string]any{"kind": "memory", "text": "我喜欢喝茶", "nature": "preference", "subject": "我", "predicate": "偏好", "quote": "我喜欢喝茶", "confidence": 0.99, "acquisition": "direct", "qualification": "asserted"},
	}}))
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source, "source.extract")); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM evidence WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(source.ID)).Scan(&n); err != nil || n != 1 {
		t.Fatal("image-grounded memory retained", n, err)
	}
	f.mu.Lock()
	seen := strings.Join(f.seen, "\n")
	f.mu.Unlock()
	if !strings.Contains(seen, "attachment_context") || !strings.Contains(seen, `\"role\":\"assistant\"`) && !strings.Contains(seen, `"role":"assistant"`) {
		t.Fatal("attachment context missing", seen)
	}
}

func TestM1V1ExistingUploadAndDeskHTTP(t *testing.T) {
	s, scope, _ := m1Setup(t)
	handler := httpapi.New(memory.NewService(s), s, httpapi.NewOwnerToken("synthetic-token", scope.OwnerID), s.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Attachments: s, Workspace: s})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer synthetic-token")
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="notice.png"`)
	header.Set("Content-Type", "image/png")
	part, err := form.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("synthetic image"))
	form.Close()
	resp, err := http.Post(server.URL+"/v1/memory/attachments", form.FormDataContentType(), &payload)
	if err != nil {
		t.Fatal(err)
	}
	var upload memory.IngestResult
	err = json.NewDecoder(resp.Body).Decode(&upload)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 201 {
		t.Fatal(resp.StatusCode, err)
	}
	req := turnRequest("帮我记一下")
	req.Attachments = []memory.Ref{upload.Ref}
	resp, err = http.Post(server.URL+"/v1/desk/turn", "application/json", bytes.NewReader(asJSON(req)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out workspace.DeskTurnResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 200 || len(out.State.Tasks) != 1 {
		t.Fatal(out, resp.StatusCode, err)
	}
	resp2, err := http.Get(server.URL + fmt.Sprintf("/v1/memory/sources/%s/attachment?version=1", upload.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	data, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != 200 || string(data) != "synthetic image" {
		t.Fatal(resp2.StatusCode, string(data))
	}
}

func TestM1AttachmentIsolationAndRebinding(t *testing.T) {
	s, scope, _ := m1Setup(t)
	ref := m1Upload(t, s, scope)
	foreign := turnRequest("帮我记一下")
	foreign.Attachments = []memory.Ref{ref}
	if _, err := s.DeskTurn(context.Background(), owner(), foreign); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("foreign image", err)
	}
	req := turnRequest("帮我记一下")
	req.Attachments = []memory.Ref{ref}
	mustTurn(t, s, scope, req)
	req.RequestID = string(memory.NewID())
	if _, err := s.DeskTurn(context.Background(), scope, req); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("image bound to another turn", err)
	}
}

func TestM1ConcurrentSameImageRequestIsProcessedOnce(t *testing.T) {
	s, scope, f := m1Setup(t)
	ref := m1Upload(t, s, scope)
	req := turnRequest("帮我记一下")
	req.Attachments = []memory.Ref{ref}
	results := make(chan workspace.DeskTurnResponse, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { out, err := s.DeskTurn(context.Background(), scope, req); results <- out; errs <- err }()
	}
	one, two := <-results, <-results
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if one.Turn.ID != two.Turn.ID || f.vision != 1 || f.secretary != 1 {
		t.Fatal("parallel replay", f.vision, f.secretary)
	}
}

func TestM1ScannedPDFPagesPreferVision(t *testing.T) {
	s, scope, f := m1Setup(t)
	bin := t.TempDir()
	scripts := map[string]string{"pdfinfo": "printf 'Pages: 2\\n'", "pdftotext": "exit 0", "pdftoppm": "for arg; do last=\"$arg\"; done\nprintf 'synthetic image' > \"$last.png\""}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	upload, err := s.IngestAttachment(context.Background(), scope, memory.IngestRequest{Connector: "file-import", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "synthetic.pdf", MediaType: "application/pdf"}, strings.NewReader("synthetic PDF"))
	if err != nil {
		t.Fatal(err)
	}
	req := turnRequest("帮我记一下")
	req.Attachments = []memory.Ref{upload.Ref}
	out := mustTurn(t, s, scope, req)
	if len(out.State.Tasks) != 1 || f.vision != 2 {
		t.Fatal("scanned pages not read", f.vision)
	}
	m1Usage(t, s, scope, 2)
	var text, representation string
	if err := s.pool.QueryRow(context.Background(), "SELECT body,representation FROM source_versions WHERE owner_id=$1 AND derived_from_id=$2", string(scope.OwnerID), string(upload.ID)).Scan(&text, &representation); err != nil || representation != "vision" || !strings.Contains(text, "[第 2 页]") {
		t.Fatal(text, representation, err)
	}
}

func TestM1WebAudioUsesExistingTranscription(t *testing.T) {
	s, scope, _ := m1Setup(t)
	probe := t.TempDir()
	if err := os.WriteFile(filepath.Join(probe, "ffprobe"), []byte("#!/bin/sh\nprintf '2\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", probe+string(os.PathListSeparator)+os.Getenv("PATH"))
	asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer file.Close()
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "明天交物业费"})
	}))
	defer asr.Close()
	s.models.Config.Transcription = "asr"
	s.models.Config.Providers = append(s.models.Config.Providers, ai.Provider{ID: "asr", Protocol: "openai", Transcription: true, BaseURL: asr.URL, Model: "fake", CostMode: "free"})
	upload, err := s.IngestAttachment(context.Background(), scope, memory.IngestRequest{Connector: "file-import", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "synthetic.ogg", MediaType: "audio/ogg"}, strings.NewReader("synthetic audio"))
	if err != nil {
		t.Fatal(err)
	}
	req := turnRequest("")
	req.Attachments = []memory.Ref{upload.Ref}
	out := mustTurn(t, s, scope, req)
	if len(out.State.Tasks) != 1 || out.Turn.Text != "" {
		t.Fatal(out)
	}
	var representation, text string
	if err := s.pool.QueryRow(context.Background(), "SELECT representation,body FROM source_versions WHERE owner_id=$1 AND derived_from_id=$2", string(scope.OwnerID), string(upload.ID)).Scan(&representation, &text); err != nil || representation != "transcript" || text != "明天交物业费" {
		t.Fatal(text, representation, err)
	}
}

func TestM1ExternallyDeletedImageExpiresUndoAndClearsContext(t *testing.T) {
	s, scope, _ := m1Setup(t)
	ref := m1Upload(t, s, scope)
	req := turnRequest("帮我记一下")
	req.Attachments = []memory.Ref{ref}
	out := mustTurn(t, s, scope, req)
	receipt := out.Turn.Receipts[len(out.Turn.Receipts)-1]
	if err := s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(context.Background(), scope, *receipt.ActionID); !errors.Is(err, workspace.ErrExpired) {
		t.Fatal("deleted image undo", err)
	}
	var contextText string
	if err := s.pool.QueryRow(context.Background(), "SELECT coalesce(response->>'attachmentContext','') FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&contextText); err != nil || contextText != "" {
		t.Fatal("deleted context retained", contextText, err)
	}
}

func TestM1ParsedLegacyImagesAreNotReread(t *testing.T) {
	s, scope, f := m1Setup(t)
	ref := m1Upload(t, s, scope)
	s.models.Config.Providers[0].Protocol = "text-only-fixture"
	job := leaseStage(t, s, scope, ref, "source.parse")
	if err := s.ProcessAttachment(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	s.models.Config.Providers[0].Protocol = "openai"
	req := turnRequest("帮我记一下")
	req.Attachments = []memory.Ref{ref}
	out := mustTurn(t, s, scope, req)
	if f.vision != 0 || !strings.Contains(out.Turn.Reply, "看不了图") {
		t.Fatal("legacy image was reread", f.vision)
	}
	m1Parsed(t, s, scope, ref, "ocr")
}
