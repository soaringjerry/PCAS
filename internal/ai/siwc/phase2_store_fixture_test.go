package siwc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
)

// Phase2StoreFixture is a test-only bridge to the existing synthetic OIDC,
// catalog, and SSE fixture. It introduces no production injection point.
type Phase2StoreFixture struct {
	endpoint string
	mu       sync.Mutex
	bodies   [][]byte
}

func NewPhase2StoreFixture(t *testing.T, reply string) (*Manager, *Phase2StoreFixture) {
	t.Helper()
	f, manager := newFixture(t)
	signIn(t, f, manager, "")
	delta, err := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": reply})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.stream = fmt.Sprintf("data: %s\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":120,\"output_tokens\":20}}}\n\n", delta)
	f.mu.Unlock()
	capture := &Phase2StoreFixture{endpoint: f.server.URL + "/v1"}
	original := f.server.Config.Handler
	// Install only after the existing fake login has completed. The original
	// handler's authentication and request-shape assertions remain active.
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses" {
			body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
			if err != nil {
				t.Error(err)
				http.Error(w, "synthetic capture failed", http.StatusInternalServerError)
				return
			}
			capture.mu.Lock()
			capture.bodies = append(capture.bodies, append([]byte(nil), body...))
			capture.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		original.ServeHTTP(w, r)
	})
	return manager, capture
}

func (f *Phase2StoreFixture) Endpoint() string { return f.endpoint }
func (f *Phase2StoreFixture) Bodies() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.bodies))
	for i, body := range f.bodies {
		out[i] = append([]byte(nil), body...)
	}
	return out
}
