package postgres

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// A failed or unusable extraction call names what went wrong. It is retried
// automatically only when the provider reserves no per-request cost; a metered
// provider waits for an explicit retry because the call may have been billed.
func TestExtractionFailureIsNamedAndRetriedOnlyWhenFree(t *testing.T) {
	for _, tc := range []struct {
		name    string
		metered bool
		status  int
		body    string
		code    string
	}{
		{name: "free provider, call fails", status: 500, code: "model_call_failed"},
		{name: "free provider, output unusable", status: 200, body: "这不是 JSON", code: "model_output_invalid"},
		{name: "metered provider, call fails", metered: true, status: 500, code: "model_call_failed"},
		{name: "metered provider, output unusable", metered: true, status: 200, body: "这不是 JSON", code: "model_output_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					return
				}
				secretaryModelReply(w, tc.body)
			}))
			t.Cleanup(server.Close)
			provider := ai.Provider{ID: "model", Name: "测试", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}
			if tc.metered {
				provider.CostMode, provider.InputPerMillion, provider.OutputPerMillion = "", 1, 1
			}
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{provider}}})
			source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "desk", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: "偏好清晨工作", MediaType: "text/plain"})
			err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source.Ref, "source.extract"))
			var failure *worker.JobError
			if !errors.As(err, &failure) {
				t.Fatalf("want a named job failure, got %v", err)
			}
			if failure.Code != tc.code || failure.Retry == tc.metered {
				t.Fatalf("code=%q retry=%t", failure.Code, failure.Retry)
			}
			if errors.Is(err, memory.ErrUnavailable) {
				t.Fatal("a configured model must not be reported as not configured")
			}
		})
	}
}

// With no extraction model, stop without consuming retries. Configuration plus
// an explicit retry must complete the same source successfully (batch2 R12).
func TestExtractionWithoutModelStaysNotConfigured(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	s.SetModels(&ai.Registry{Config: ai.Configuration{}})
	text := "偏好清晨工作"
	source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "desk", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: text, MediaType: "text/plain"})
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_jobs WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := s.pool.QueryRow(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage) VALUES(gen_random_uuid(),$1,$2,$3,'source.extract') RETURNING id::text`, string(scope.OwnerID), string(source.Ref.ID), source.Ref.Version).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	var handlerErr error
	w := worker.New(s, map[string]worker.Handler{"source.extract": func(ctx context.Context, job worker.Job) error {
		handlerErr = s.ProcessExtraction(ctx, job)
		return handlerErr
	}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if worked, err := w.RunOnce(ctx); err != nil || !worked {
		t.Fatal("missing provider was not processed", worked, err)
	}
	var failure *worker.JobError
	if !errors.As(handlerErr, &failure) || failure.Code != "provider_not_configured" || failure.Retry {
		t.Fatal("want a non-retryable provider_not_configured failure", handlerErr)
	}
	var state, code string
	var attempts int
	if err := s.pool.QueryRow(ctx, "SELECT state,error_code,attempts FROM memory_jobs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), jobID).Scan(&state, &code, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "blocked" || code != "provider_not_configured" || attempts != 0 {
		t.Fatal("missing provider must stop without consuming retries", state, code, attempts)
	}
	if worked, err := w.RunOnce(ctx); err != nil || worked {
		t.Fatal("missing provider was retried automatically", worked, err)
	}
	extractionTestModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		writeExtractionResponse(w, directExtractedItem(text))
	})
	workspaceCommand(t, s, scope, workspace.Command{Type: "retryJob", ID: jobID})
	if worked, err := w.RunOnce(ctx); err != nil || !worked {
		t.Fatal("configured provider did not process the manual retry", worked, err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT state FROM memory_jobs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), jobID).Scan(&state); err != nil || state != "done" {
		t.Fatal("manual retry did not complete", state, err)
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil || len(st.Memories) != 1 || st.Memories[0].Text != text {
		t.Fatal("manual retry did not retain the extracted memory", err, st.Memories)
	}
}
