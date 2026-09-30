package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterAsksJevOneChoice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model     string `json:"model"`
			State     string `json:"state"`
			Questions map[string]struct {
				Type     string            `json:"type"`
				Criteria map[string]string `json:"criteria"`
			} `json:"questions"`
		}
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer k" || json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		q := in.Questions["intent"]
		if in.State != "帮我写封请假邮件" || q.Type != "choice" || len(q.Criteria) != 3 {
			t.Errorf("unexpected body %+v", in)
		}
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"intent":{"type":"choice","choice":"delegate","confidence":0.91}}}`))
	}))
	defer server.Close()
	r := NewRouter("k")
	r.BaseURL = server.URL
	got, err := r.Route(context.Background(), "帮我写封请假邮件")
	if err != nil || got != (Route{Intent: IntentDelegate, Confidence: 0.91}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestRouterRejectsUnknownChoicesAndErrors(t *testing.T) {
	if NewRouter("") != nil {
		t.Fatal("router without a key")
	}
	for _, reply := range []string{`{"answers":{"intent":{"choice":"shop","confidence":0.9}}}`, `not json`, ``} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reply == "" {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(reply))
		}))
		r := NewRouter("k")
		r.BaseURL = server.URL
		if _, err := r.Route(context.Background(), "x"); err == nil {
			t.Fatalf("accepted %q", reply)
		}
		server.Close()
	}
}
