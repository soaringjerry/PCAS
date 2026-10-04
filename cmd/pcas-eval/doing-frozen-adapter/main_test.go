package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func TestReplayBindingsAndNoAdditionalCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	s := doing.ContextSnapshot{Version: 1, Synthetic: true, SuiteSHA: doing.SHA("suite"), AsOf: "2026-10-04T09:00:00Z", HostDate: "2026-10-04", StartedAt: "2026-10-04T23:00:00Z", CompletedAt: "2026-10-04T23:01:00Z", Entries: []doing.ContextEntry{{Task: "T", RequestSHA: doing.SHA("请求"), Text: "虚构", ContextSHA: doing.SHA("虚构"), ContextChars: 2, CaptureCalls: 1}}}
	if e := doing.WriteJSON(path, s); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := replay(strings.NewReader(`{"task_id":"T","request":"请求","as_of":"2026-10-04T09:00:00Z","database_url":"never-used"}`), &out, path); e != nil {
		t.Fatal(e)
	}
	var got struct {
		Context string `json:"context"`
		Calls   int    `json:"model_calls"`
	}
	if e := json.Unmarshal(out.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	if got.Context != "虚构" || got.Calls != 0 {
		t.Fatal("replay changed context or invented cost")
	}
	for _, bad := range []string{
		`{"task_id":"wrong","request":"请求","as_of":"2026-10-04T09:00:00Z"}`,
		`{"task_id":"T","request":"changed","as_of":"2026-10-04T09:00:00Z"}`,
		`{"task_id":"T","request":"请求","as_of":"2026-10-05T09:00:00Z"}`,
	} {
		out.Reset()
		if replay(strings.NewReader(bad), &out, path) == nil || out.Len() != 0 {
			t.Fatal("bad binding produced output")
		}
	}
}
