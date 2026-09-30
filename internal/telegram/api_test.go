package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadChecksRemoteSizeAndStream(t *testing.T) {
	for _, advertised := range []bool{false, true} {
		t.Run(map[bool]string{false: "oversized-stream", true: "oversized-metadata"}[advertised], func(t *testing.T) {
			downloads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/getFile") {
					size := 1
					if advertised {
						size = maxFileSize + 1
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"file_path": "voice/audio.ogg", "file_size": size}})
					return
				}
				downloads++
				// Flush before streaming so Content-Length cannot provide the limit.
				w.(http.Flusher).Flush()
				_, _ = io.Copy(w, io.LimitReader(&zeroReader{}, maxFileSize+1))
			}))
			defer server.Close()
			a := botAPI{baseURL: server.URL, client: server.Client()}
			_, _, err := a.download(context.Background(), "fixture", file{ID: "audio"})
			if err != errFile || (advertised && downloads != 0) {
				t.Fatal("download limit ignored", err, downloads)
			}
		})
	}
}

type zeroReader struct{}

func (*zeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }
