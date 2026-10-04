package siwc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVisionUsesAuthorizedResponsesImageInput(t *testing.T) {
	f, m := newFixture(t)
	signIn(t, f, m, "")
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer access-PRIVATE" {
			t.Error("authorized Responses request")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body) != 5 || body["store"] != false || body["stream"] != true {
			t.Error("unsupported SIWC field")
		}
		items := body["input"].([]any)[0].(map[string]any)["content"].([]any)
		if len(items) != 2 || items[0].(map[string]any)["type"] != "input_text" || items[0].(map[string]any)["text"] != "read notice" || items[1].(map[string]any)["type"] != "input_image" || items[1].(map[string]any)["image_url"] != "data:image/png;base64,eA==" {
			t.Error("image input", items)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"notice\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n")
	}))
	defer api.Close()
	// The existing catalog/authorization fixture stays on its original issuer.
	m.api = api.URL
	if err := m.Select(context.Background(), f.client, "first", true, false); err != nil {
		t.Fatal(err)
	}
	out, err := m.Vision(context.Background(), "first", "read notice", "data:image/png;base64,eA==")
	if err != nil || out.Text != "notice" || out.InputTokens != 2 || out.OutputTokens != 1 {
		t.Fatal(out, err)
	}
}
