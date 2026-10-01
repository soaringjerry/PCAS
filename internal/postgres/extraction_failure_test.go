package postgres

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
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

// With no extraction model at all, retrying cannot help: keep the old result.
func TestExtractionWithoutModelStaysNotConfigured(t *testing.T) {
	s := testStore(t)
	scope := owner()
	s.SetModels(&ai.Registry{Config: ai.Configuration{}})
	source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "desk", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: "偏好清晨工作", MediaType: "text/plain"})
	err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source.Ref, "source.extract"))
	var failure *worker.JobError
	if !errors.Is(err, memory.ErrUnavailable) || errors.As(err, &failure) {
		t.Fatal(err)
	}
}
