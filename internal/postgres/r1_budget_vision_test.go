package postgres

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
)

// The shared attachment reader introduced by M1 must settle vision calls before
// saving derivatives, including cancellation and the legacy OCR fallback.
func TestR1R6VisionSettlesOnlyItsReservation(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			s, scope, _ := m1Setup(t)
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				close(started)
				if outcome == "cancel" {
					<-release
				}
				if outcome == "failure" {
					w.WriteHeader(500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{map[string]any{"message": map[string]string{"content": m1Notice}}},
					"usage":   map[string]int{"prompt_tokens": 30, "completion_tokens": 4},
				})
			}))
			defer server.Close()
			defer close(release)
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "Synthetic vision", Protocol: "openai", BaseURL: server.URL, Model: "synthetic", MaxOutput: 10000, InputPerMillion: 2, OutputPerMillion: 4}}}})
			ref := m1Upload(t, s, scope)
			if err := s.reserveModelCost(context.Background(), scope.OwnerID, 0.03, nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				parsed, err := s.readAttachment(ctx, scope, ref, nil)
				if err == nil {
					want := "vision"
					if outcome == "failure" {
						want = "ocr"
					}
					if parsed.Representation != want {
						t.Errorf("attachment processing changed: got %q want %q", parsed.Representation, want)
					}
				}
				done <- err
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("vision did not reach the fake provider", ctx.Err())
			}
			if outcome == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if (outcome == "cancel") != (err != nil) {
					t.Fatal("unexpected attachment outcome", err)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("attachment reader did not finish")
			}
			var cost float64
			if err := s.pool.QueryRow(context.Background(), "SELECT coalesce(sum(reserved_cost),0) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&cost); err != nil {
				t.Fatal(err)
			}
			want := 0.03
			if outcome == "success" {
				want += (30*2.0 + 4*4.0) / 1e6
			}
			if math.Abs(cost-want) > 1e-9 {
				t.Errorf("vision did not settle only its reservation: got %g want %g", cost, want)
			}
			if calls.Load() != 1 {
				t.Errorf("vision call retried: %d calls", calls.Load())
			}
		})
	}
}
