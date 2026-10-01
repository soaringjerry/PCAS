// A test-only local reverse proxy captures the complete model request before
// forwarding to the existing deterministic golden fixture. Golden's public
// events intentionally contain only the current utterance, which cannot prove
// that a recalled excerpt reached the model.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func main() {
	modelPath, envPath := os.Args[1], os.Args[2]
	data, err := os.ReadFile(modelPath)
	if err != nil {
		panic(err)
	}
	var models map[string]any
	if err := json.Unmarshal(data, &models); err != nil {
		panic(err)
	}
	providers := models["providers"].([]any)
	upstream, err := url.Parse(providers[0].(map[string]any)["base_url"].(string))
	if err != nil || upstream.Hostname() != "127.0.0.1" {
		panic("capture requires local fake upstream")
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var mu sync.Mutex
	requests := []json.RawMessage{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b1/requests" {
			mu.Lock()
			defer mu.Unlock()
			if r.Method == "DELETE" {
				requests = []json.RawMessage{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"requests": requests})
			return
		}
		if r.Method == "POST" && r.URL.Path != "/control" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "capture read failed", 500)
				return
			}
			if !json.Valid(body) {
				http.Error(w, "invalid model request", 400)
				return
			}
			mu.Lock()
			requests = append(requests, append(json.RawMessage(nil), body...))
			mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	for _, p := range providers {
		p.(map[string]any)["base_url"] = server.URL
	}
	data, err = json.Marshal(models)
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(modelPath, data, 0600); err != nil {
		panic(err)
	}
	if err = os.WriteFile(envPath, []byte(fmt.Sprintf("export PCAS_TEST_B1_CAPTURE_URL=%q\n", server.URL)), 0600); err != nil {
		panic(err)
	}
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	<-done
}
