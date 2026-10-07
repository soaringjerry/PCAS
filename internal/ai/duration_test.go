package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInvocationDurationAllAdaptersAndFailure(t *testing.T) {
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		if failed {
			http.Error(w, "fictitious failure", 503)
			return
		}
		switch r.URL.Path {
		case "/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float32{1, 0}}}})
		case "/audio/transcriptions":
			_ = json.NewEncoder(w).Encode(map[string]string{"text": "虚构录音"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "虚构回复"}}}})
		}
	}))
	defer server.Close()
	p := Provider{ID: "model", Protocol: "openai", BaseURL: server.URL, Model: "fictional", CostMode: "free", MaxOutput: 100}
	ep := p
	ep.ID = "embedding"
	ep.Embedding = true
	ap := p
	ap.ID = "audio"
	ap.Transcription = true
	registry := &Registry{HTTP: server.Client(), Config: Configuration{Providers: []Provider{p, ep, ap}, Transcription: "audio"}}
	methods := map[string]func(context.Context) (*int64, error){
		"text": func(ctx context.Context) (*int64, error) {
			r, e := registry.Generate(ctx, "model", "", "fictional")
			return r.DurationMS, e
		},
		"schema fallback": func(ctx context.Context) (*int64, error) {
			r, e := registry.GenerateSchema(ctx, "model", "", "fictional", json.RawMessage(`{}`))
			return r.DurationMS, e
		},
		"search schema fallback": func(ctx context.Context) (*int64, error) {
			r, e := registry.GenerateWithSearchSchema(ctx, "model", "", "fictional", nil)
			return r.DurationMS, e
		},
		"image": func(ctx context.Context) (*int64, error) {
			r, e := registry.Vision(ctx, "model", "fictional", Image{MediaType: "image/png", Data: []byte("fictional")})
			return r.DurationMS, e
		},
		"embedding": func(ctx context.Context) (*int64, error) {
			_, r, e := registry.EmbedProviderUsage(ctx, ep, []string{"fictional"})
			return r.DurationMS, e
		},
		"audio": func(ctx context.Context) (*int64, error) {
			_, ms, e := registry.TranscribeUsage(ctx, strings.NewReader("fictional"), "fake.wav")
			return ms, e
		},
	}
	for _, failure := range []bool{false, true} {
		failed = failure
		for name, call := range methods {
			t.Run(name+map[bool]string{true: " failure", false: " success"}[failure], func(t *testing.T) {
				count := 0
				var observed int64
				ctx := WithInvocationObserver(context.Background(), func(ms int64) { count++; observed = ms })
				ms, err := call(ctx)
				if (err != nil) != failure || ms == nil || *ms < 10 || count != 1 || observed != *ms {
					t.Fatalf("ms=%v err=%v observations=%d/%d", ms, err, count, observed)
				}
			})
		}
	}
	count := 0
	ctx := WithInvocationObserver(context.Background(), func(int64) { count++ })
	result, err := registry.Generate(ctx, "missing", "", "fictional")
	if err == nil || result.DurationMS != nil || count != 0 {
		t.Fatal("unavailable provider counted as call")
	}
}
