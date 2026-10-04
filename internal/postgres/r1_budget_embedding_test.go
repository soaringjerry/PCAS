package postgres

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestR1R6FailedEmbeddingReleasesReservation(t *testing.T) {
	for _, kind := range []string{"query", "background"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
			defer server.Close()
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Embedding: "vector", Providers: []ai.Provider{{ID: "vector", Name: "Synthetic vector", Protocol: "openai", BaseURL: server.URL, Model: "synthetic", Embedding: true, InputPerMillion: 2}}}})
			if _, err := s.Snapshot(context.Background(), scope); err != nil {
				t.Fatal(err)
			}
			if kind == "query" {
				if _, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "R1 embedding question", Mode: memory.Remember}); err != nil {
					t.Fatal(err)
				}
			} else {
				source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: "r1-vector", ExternalVersion: "1", Title: "R1 vector", Text: "R1 vector content", MediaType: "text/plain"})
				if err := s.ProcessChunks(context.Background(), leaseStage(t, s, scope, source.Ref, "source.chunk")); err != nil {
					t.Fatal(err)
				}
				job := leaseStage(t, s, scope, source.Ref, "source.embed")
				if err := s.ProcessEmbedding(context.Background(), job); err == nil {
					t.Fatal("expected model failure")
				}
			}
			var cost float64
			if err := s.pool.QueryRow(context.Background(), "SELECT coalesce(sum(reserved_cost),0) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&cost); err != nil {
				t.Fatal(err)
			}
			if cost != 0 {
				t.Errorf("failed embedding retained reservation: %g", cost)
			}
		})
	}
}

func TestR1R6FailedTranscriptionReleasesReservation(t *testing.T) {
	s, scope := testStore(t), owner()
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	// Keep the test independent of host audio tools and codecs.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffprobe"), []byte("#!/bin/sh\necho 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Transcription: "audio", Providers: []ai.Provider{{ID: "audio", Name: "Synthetic audio", Protocol: "openai", BaseURL: server.URL, Model: "synthetic", Transcription: true, AudioPerMinute: 0.01}}}})
	source, err := s.IngestAttachment(context.Background(), scope, memory.IngestRequest{Connector: "manual", ExternalID: "r1-audio", ExternalVersion: "1", Title: "R1 audio", MediaType: "audio/wav"}, strings.NewReader("synthetic audio"))
	if err != nil {
		t.Fatal(err)
	}
	job := leaseStage(t, s, scope, source.Ref, "source.parse")
	if err := s.ProcessAttachment(context.Background(), job); err == nil {
		t.Fatal("expected transcription failure")
	}
	var cost float64
	if err := s.pool.QueryRow(context.Background(), "SELECT coalesce(sum(reserved_cost),0) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	if cost != 0 {
		t.Errorf("failed transcription retained reservation: %g", cost)
	}
}
