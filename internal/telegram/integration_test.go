package telegram

import (
	"context"
	"encoding/json"
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
	"github.com/soaringjerry/PCAS/internal/postgres"
)

// These tests use only the disposable database explicitly supplied by the runner.
// Unique owners and a separate blob directory keep them independent of the
// postgres package's schema-isolated integration suite.
func integrationStore(t *testing.T) *postgres.Store {
	t.Helper()
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PCAS_TEST_DATABASE_URL to a disposable test database")
	}
	s, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	f, err := blob.NewFiles(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(f)
	return s
}

func TestIntegrationDeskTurnReplayAndUndo(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		content := `{"reply":"安排好了","actions":[{"op":"create_task","title":"开会","due":"2026-10-02T15:00"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer model.Close()
	s.SetModels(&ai.Registry{HTTP: model.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "测试秘书", Protocol: "openai", BaseURL: model.URL, Model: "test", MaxOutput: 1000, CostMode: "free"}}}})
	b.enqueue(textUpdate(1, "周五下午三点开会"))
	b.failSend = true
	if err := p.step(context.Background(), "fixture", "123"); err == nil {
		t.Fatal("delivery failure hidden")
	}
	b.failSend = false
	step(t, p)
	st, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || len(st.Tasks) != 1 || calls.Load() != 1 {
		t.Fatal("retry executed again", len(st.Tasks), calls.Load(), err)
	}
	if st.Tasks[0].History[len(st.Tasks[0].History)-1].By != "secretary" {
		t.Fatal("wrong actor")
	}
	sent := b.of("sendMessage")
	rows := decode[keyboard](sent[len(sent)-1].body["reply_markup"]).Rows
	b.enqueue(callbackUpdate(2, rows[0][0].Data, 101))
	step(t, p)
	st, err = s.Snapshot(context.Background(), p.scope)
	if err != nil || len(st.Tasks) != 0 || decode[string](b.of("answerCallbackQuery")[0].body["text"]) != "已撤销" {
		t.Fatal("Undo did not remove task", err)
	}
}

func TestIntegrationUnconfiguredVoicePreservesOriginal(t *testing.T) {
	s := integrationStore(t)
	p, _, b := fixture(t)
	p.store = s
	u := textUpdate(1, "")
	u.Message.Voice = &file{ID: "voice"}
	b.enqueue(u)
	step(t, p)
	st, err := s.Snapshot(context.Background(), p.scope)
	if err != nil || len(st.Sources) != 1 {
		t.Fatal("audio not retained", err)
	}
	f, _, media, err := s.OpenAttachment(context.Background(), p.scope, memory.ID(st.Sources[0].ID), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if media != "audio/ogg" || string(data) != "audio-bytes" {
		t.Fatal(media, string(data))
	}
	if !strings.Contains(decode[string](b.of("sendMessage")[0].body["text"]), "先发文字吧") {
		t.Fatal("missing fallback")
	}
}
