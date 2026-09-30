package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestAttachmentRetentionIdempotencyAndDeletion(t *testing.T) {
	s := testStore(t)
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	scope := owner()
	ctx := context.Background()
	in := memory.IngestRequest{Connector: "test", ExternalID: "file-1", ExternalVersion: "1", Title: "原图.png", MediaType: "image/png"}
	first, err := s.IngestAttachment(ctx, scope, in, strings.NewReader("test image payload"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.IngestAttachment(ctx, scope, in, strings.NewReader("test image payload"))
	if err != nil || !again.Duplicate || first.ID != again.ID {
		t.Fatal("attachment replay", err)
	}
	if err := s.CleanupBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	file, _, _, err := s.OpenAttachment(ctx, scope, first.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(file)
	file.Close()
	if string(data) != "test image payload" {
		t.Fatal("original changed")
	}
	metadata, err := s.GetSource(ctx, scope, first.ID, 1)
	if err != nil || !metadata.Source.HasAttachment || metadata.Source.Text != "" {
		t.Fatal("wrong attachment metadata", metadata, err)
	}
	derived := mustIngest(t, s, scope, memory.IngestRequest{Connector: "attachment-text", ExternalID: string(first.ID), ExternalVersion: "1", Title: "解析文本", Text: "OCR 内容", MediaType: "text/plain"})
	if _, err := s.pool.Exec(ctx, "UPDATE source_versions SET representation='ocr',derived_from_id=$3,derived_from_version=1 WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(derived.ID), string(first.ID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{first.Ref}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.OpenAttachment(ctx, scope, first.ID, 1); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("deleted original accessible", err)
	}
}

func TestOGGAttachmentRetainedAndTranscribedInBackground(t *testing.T) {
	s := testStore(t)
	f, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(f)
	// Substitute only the duration probe; actual Registry.Transcribe talks to
	// the fake model so this covers the complete source.parse audio branch.
	probeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(probeDir, "ffprobe"), []byte("#!/bin/sh\nprintf '2\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", probeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "OGG-original-bytes" || header.Filename != "voice.ogg" {
			t.Error("transcription did not receive original audio")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "明天下午三点开会"})
	}))
	defer model.Close()
	s.SetModels(&ai.Registry{HTTP: model.Client(), Config: ai.Configuration{Transcription: "asr", Providers: []ai.Provider{{ID: "asr", Transcription: true, Protocol: "openai", BaseURL: model.URL, Model: "test", CostMode: "free"}}}})
	ctx, scope := context.Background(), owner()
	in := memory.IngestRequest{Connector: "telegram", ExternalID: "123:1", ExternalVersion: "1", Title: "voice.ogg", MediaType: "audio/ogg"}
	result, err := s.IngestAttachment(ctx, scope, in, strings.NewReader("OGG-original-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	original, _, media, err := s.OpenAttachment(ctx, scope, result.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(original)
	original.Close()
	if string(data) != "OGG-original-bytes" || media != "audio/ogg" {
		t.Fatal("OGG not retained")
	}
	var queued int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.parse' AND state='queued'", string(scope.OwnerID), string(result.ID)).Scan(&queued); err != nil || queued != 1 {
		t.Fatal("OGG did not enter the background parse queue", queued, err)
	}
	job := leaseStage(t, s, scope, result.Ref, "source.parse")
	if err := s.ProcessAttachment(ctx, job); err != nil {
		t.Fatal(err)
	}
	var text, representation, status string
	if err := s.pool.QueryRow(ctx, "SELECT body,representation FROM source_versions WHERE owner_id=$1 AND derived_from_id=$2", string(scope.OwnerID), string(result.ID)).Scan(&text, &representation); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT state FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.parse'", string(scope.OwnerID), string(result.ID)).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || text != "明天下午三点开会" || representation != "transcript" || status != "done" {
		t.Fatal("background transcription not completed", calls.Load(), representation, status)
	}
}
