package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func replayRequest(body string) *http.Request {
	r, _ := http.NewRequest("POST", "https://fictional.invalid/v1/embeddings", bytes.NewBufferString(body))
	return r
}
func saveHTTP(t *testing.T, dir string, r httpRecording) string {
	t.Helper()
	path := filepath.Join(dir, "http.jsonl")
	if err := writePrivate(path, r); err != nil {
		t.Fatal(err)
	}
	return path
}
func sampleHTTP() httpRecording {
	r := httpRecording{Method: "POST", URL: "https://fictional.invalid/v1/embeddings", Input: []byte(`{"input":"fictional"}`), Status: 200, Output: []byte(`{"data":[],"usage":{"total_tokens":7}}`)}
	r.InputSHA256 = sum(r.Input)
	r.OutputSHA256 = sum(r.Output)
	return r
}
func TestHTTPReplayReturnsRecordedUsageWithoutNetwork(t *testing.T) {
	r := sampleHTTP()
	path := saveHTTP(t, t.TempDir(), r)
	tr, err := newRecordedHTTP("replay", path, transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("live transport called"); return nil, nil }))
	if err != nil {
		t.Fatal(err)
	}
	response, err := tr.RoundTrip(replayRequest(string(r.Input)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	if !bytes.Equal(body, r.Output) || tr.live != 0 || tr.cursor != 1 {
		t.Fatal("reply or consumption changed")
	}
	if _, err = tr.RoundTrip(replayRequest(string(r.Input))); err == nil || tr.cursor != 1 {
		t.Fatal("duplicate request reused reply")
	}
}
func TestHTTPReplayRejectsChangedInputAndCorruptRecording(t *testing.T) {
	r := sampleHTTP()
	path := saveHTTP(t, t.TempDir(), r)
	tr, err := newRecordedHTTP("replay", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tr.RoundTrip(replayRequest(`{"input":"different"}`)); err == nil || tr.cursor != 0 {
		t.Fatal("changed input accepted")
	}
	r.Output = []byte("different")
	path = saveHTTP(t, t.TempDir(), r)
	if _, err = newRecordedHTTP("replay", path, nil); err == nil {
		t.Fatal("corrupt output accepted")
	}
	if _, err = newRecordedHTTP("replay", filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Fatal("missing cassette accepted")
	}
}
func TestHTTPCaptureBoundsCallsAndKeepsFailureSemantics(t *testing.T) {
	calls := 0
	path := filepath.Join(t.TempDir(), "record.jsonl")
	tr, err := newRecordedHTTP("record", path, transportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, context.DeadlineExceeded }))
	if err != nil {
		t.Fatal(err)
	}
	tr.limit = 1
	if _, err = tr.RoundTrip(replayRequest("fictional")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err = tr.RoundTrip(replayRequest("fictional")); err == nil || calls != 1 || tr.suppressed != 1 {
		t.Fatal("call allowance did not stop network")
	}
	offline, err := newRecordedHTTP("replay", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = offline.RoundTrip(replayRequest("fictional")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("failure category changed", err)
	}
}
func TestHTTPCaptureStopsAfterEvidenceCannotBeSaved(t *testing.T) {
	calls := 0
	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "record.jsonl")
	tr, err := newRecordedHTTP("record", path, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString("fictional"))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	tr.limit = 2
	if _, err = tr.RoundTrip(replayRequest("fictional")); err == nil {
		t.Fatal("write error hidden")
	}
	if _, err = tr.RoundTrip(replayRequest("fictional")); err == nil || calls != 1 {
		t.Fatal("provider called after evidence failure")
	}
}
func TestHTTPReplayRejectsPublicPermissions(t *testing.T) {
	path := saveHTTP(t, t.TempDir(), sampleHTTP())
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := newRecordedHTTP("replay", path, nil); err == nil {
		t.Fatal("public cassette accepted")
	}
}
func TestHTTPRecordingRetainsExactRequestBytes(t *testing.T) {
	r := sampleHTTP()
	path := filepath.Join(t.TempDir(), "record.jsonl")
	tr, _ := newRecordedHTTP("record", path, transportFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if !bytes.Equal(body, r.Input) {
			t.Fatal("provider input changed")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(r.Output))}, nil
	}))
	tr.limit = 1
	if _, err := tr.RoundTrip(replayRequest(string(r.Input))); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var stored httpRecording
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored.Input, r.Input) || !bytes.Equal(stored.Output, r.Output) {
		t.Fatal("recorded bytes changed")
	}
}

func TestHTTPReplayBindsOperationAndProviderVersionWithoutCredentials(t *testing.T) {
	r := sampleHTTP()
	r.OperationID = "first"
	r.InputHeader = http.Header{"Content-Type": {"application/json"}, "Anthropic-Version": {"fictional-version"}}
	path := saveHTTP(t, t.TempDir(), r)
	for _, changed := range []string{"operation", "version", "authentication"} {
		t.Run(changed, func(t *testing.T) {
			tr, err := newRecordedHTTP("replay", path, nil)
			if err != nil {
				t.Fatal(err)
			}
			tr.selectOperation("first")
			request := replayRequest(string(r.Input))
			request.Header = r.InputHeader.Clone()
			request.Header.Set("Authorization", "offline-no-key")
			switch changed {
			case "operation":
				tr.selectOperation("different")
			case "version":
				request.Header.Set("Anthropic-Version", "different")
			}
			_, err = tr.RoundTrip(request)
			if changed == "authentication" {
				if err != nil {
					t.Fatal("offline identity rejected", err)
				}
			} else if err == nil || tr.cursor != 0 {
				t.Fatal("different call accepted")
			}
		})
	}
}
