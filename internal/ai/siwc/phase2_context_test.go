package siwc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase2ContextSIWCUnselectedModelFinalBytesAndBeforeDispatchRefusal(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuse_%t", refuse), func(t *testing.T) {
			f, manager := newFixture(t)
			signIn(t, f, manager, "")
			var mu sync.Mutex
			var actual [][]byte
			original := f.server.Config.Handler
			// Wrap the existing real fake OAuth/models/SSE handler. Its original
			// transport and inference assertions all remain active.
			f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/responses" {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					mu.Lock()
					actual = append(actual, append([]byte(nil), body...))
					mu.Unlock()
					r.Body = io.NopCloser(bytes.NewReader(body))
				}
				original.ServeHTTP(w, r)
			})
			var events []memory.ContextRequestEvent
			ctx := memory.WithContextRequestObserver(context.Background(), memory.ContextRequestObserverFunc(func(_ context.Context, event memory.ContextRequestEvent) error {
				event.Payload = append([]byte(nil), event.Payload...)
				events = append(events, event)
				if refuse && event.Stage == "before_dispatch" {
					return errors.New("phase2_fence_refused")
				}
				return nil
			}))
			result, err := manager.Generate(ctx, "", "合成固定说明", "中文\n引号\"和反斜线\\以及<&>。")
			mu.Lock()
			received := append([][]byte(nil), actual...)
			mu.Unlock()
			wantStages := []string{"prepared", "before_dispatch", "dispatched"}
			if refuse {
				wantStages = wantStages[:2]
				if err == nil || result.Text != "" || len(received) != 0 {
					t.Error("SIWC final fence refusal still externally sent or produced completed result")
				}
			} else if err != nil || result.Text != "PCAS" || len(received) != 1 {
				t.Fatalf("SIWC unspecified model failed ordinary inference: %+v requests=%d error=%v", result, len(received), err)
			}
			var stages []string
			for _, event := range events {
				stages = append(stages, event.Stage)
				if event.Model != "first" || event.Protocol != "siwc" || event.ObservationLayer != "serialized_request" || event.Endpoint != f.server.URL+"/v1" {
					t.Error("SIWC actual default model first or observable transport tuple lost")
				}
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.Model != "first" || payload.Model != event.Model {
					t.Error("SIWC selected actual model differs from observed serialized payload", err)
				}
				if len(received) == 1 && !bytes.Equal(received[0], event.Payload) {
					t.Error("SIWC observed final bytes differ byte-for-byte from fake service body")
				}
			}
			if !reflect.DeepEqual(stages, wantStages) {
				t.Errorf("SIWC stages=%v want=%v", stages, wantStages)
			}
			body, _ := json.Marshal(map[string]any{"refused": refuse, "selected_model": "first", "requested_model": "", "events": events, "actual_http_bodies": received, "postgres_secretary_context": "not exercised"})
			t.Logf("phase2-siwc-observation=%s", body)
		})
	}
}
