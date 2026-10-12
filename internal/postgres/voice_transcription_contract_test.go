package postgres

import (
	"context"
	"errors"
	"github.com/soaringjerry/PCAS/internal/input/voice"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestVoiceTranscriptionRecordsOneSettledCallWithItsCost(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	mustIngest(t, s, scope, input())
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"text":"虚构语音：明天下午三点开会"}`)
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Transcription: "asr", Providers: []ai.Provider{{ID: "asr", Transcription: true, Protocol: "openai", BaseURL: server.URL, Model: "synthetic-asr", AudioPerMinute: 0.06}}}})
	transcriber := voice.Transcriber{Models: s.Models, Calls: s.Gateway()}
	text, err := transcriber.TranscribeVoice(ctx, scope, strings.NewReader("audio-bytes"), "voice.ogg", 30)
	if err != nil || text != "虚构语音：明天下午三点开会" || calls.Load() != 1 {
		t.Fatal(text, err, calls.Load())
	}
	var outcome, accounting, operation, journal string
	var cost, reserved float64
	var tokens int
	if err := s.Pool.QueryRow(ctx, `SELECT c.outcome,c.accounting_state,c.actual_mode->>'operation',to_jsonb(c)::text,u.cost::float8,b.reserved_cost::float8,u.input_tokens
 FROM model_calls c JOIN model_usage u ON (u.owner_id,u.id)=(c.owner_id,c.usage_id) JOIN background_usage b ON (b.owner_id,b.id)=(c.owner_id,c.reservation_id)
 WHERE c.owner_id=$1 AND c.function_name='transcription' AND c.stage='voice' AND u.purpose='transcription'`, string(scope.OwnerID)).Scan(&outcome, &accounting, &operation, &journal, &cost, &reserved, &tokens); err != nil {
		t.Fatal(err)
	}
	if outcome != "returned" || accounting != "settled" || operation != "transcription" || cost != 0.03 || reserved != cost || tokens != 1500 || strings.Contains(journal, "明天下午三点") {
		t.Fatal(outcome, accounting, operation, cost, reserved, tokens)
	}
}

func TestVoiceTranscriptionWithoutAProviderStartsNothing(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	mustIngest(t, s, scope, input())
	s.SetModels(&ai.Registry{})
	transcriber := voice.Transcriber{Models: s.Models, Calls: s.Gateway()}
	if _, err := transcriber.TranscribeVoice(ctx, scope, strings.NewReader("audio-bytes"), "voice.ogg", 30); !errors.Is(err, memory.ErrUnavailable) {
		t.Fatal(err)
	}
	var rows int
	if err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM model_calls WHERE owner_id=$1)+(SELECT count(*) FROM background_usage WHERE owner_id=$1)`, string(scope.OwnerID)).Scan(&rows); err != nil || rows != 0 {
		t.Fatal(rows, err)
	}
}
