package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
)

type httpRecording struct {
	Method       string      `json:"method"`
	OperationID  string      `json:"operation_id,omitempty"`
	InputHeader  http.Header `json:"input_header,omitempty"`
	URL          string      `json:"url"`
	InputSHA256  string      `json:"input_sha256"`
	Input        []byte      `json:"input"`
	Status       int         `json:"status"`
	Header       http.Header `json:"header,omitempty"`
	Output       []byte      `json:"output,omitempty"`
	OutputSHA256 string      `json:"output_sha256"`
	ErrorCode    string      `json:"error_code,omitempty"`
}

type recordedHTTP struct {
	mode, path             string
	operationID            string
	base                   http.RoundTripper
	mutex                  sync.Mutex
	records                []httpRecording
	cursor, requests, live int
	limit, suppressed      int
	failures               []string
}

func sum(body []byte) string { hash := sha256.Sum256(body); return hex.EncodeToString(hash[:]) }

func modelHeaders(header http.Header) http.Header {
	// Authentication is deliberately absent: offline replay uses no live key.
	// Preserve all other explicit provider headers, including format and API
	// version. The application sets them before net/http adds wire metadata.
	bound := header.Clone()
	for _, name := range []string{"Authorization", "X-Api-Key", "Cookie", "Proxy-Authorization"} {
		bound.Del(name)
	}
	return bound
}

func (t *recordedHTTP) selectOperation(id string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.operationID = id
}

func (t *recordedHTTP) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var input []byte
	var err error
	if request.Body != nil {
		input, err = io.ReadAll(request.Body)
		request.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	t.requests++
	if t.mode == "record" && (t.requests > t.limit || len(t.failures) != 0) {
		t.suppressed++
		t.failures = append(t.failures, "http_capture_limit_or_failure")
		return nil, errors.New("http_capture_limit_or_failure")
	}
	if t.mode == "replay" {
		if t.cursor >= len(t.records) {
			t.failures = append(t.failures, "http_reply_missing")
			return nil, errors.New("http_reply_missing")
		}
		record := t.records[t.cursor]
		expected, err := json.Marshal(record.InputHeader)
		if err != nil {
			return nil, err
		}
		actual, err := json.Marshal(modelHeaders(request.Header))
		if err != nil {
			return nil, err
		}
		// nil and empty headers have the same transport meaning.
		headersMatch := bytes.Equal(expected, actual) || (len(record.InputHeader) == 0 && len(modelHeaders(request.Header)) == 0)
		if record.OperationID != t.operationID || !headersMatch || record.Method != request.Method || record.URL != request.URL.String() || record.InputSHA256 != sum(input) {
			t.failures = append(t.failures, "http_input_mismatch")
			return nil, errors.New("http_input_mismatch")
		}
		t.cursor++
		if record.ErrorCode != "" {
			return nil, recordedHTTPError(record.ErrorCode)
		}
		return &http.Response{StatusCode: record.Status, Header: record.Header.Clone(), Body: io.NopCloser(bytes.NewReader(record.Output)), Request: request}, nil
	}
	request.Body = io.NopCloser(bytes.NewReader(input))
	t.live++
	response, callErr := t.base.RoundTrip(request)
	record := httpRecording{Method: request.Method, OperationID: t.operationID, InputHeader: modelHeaders(request.Header), URL: request.URL.String(), InputSHA256: sum(input), Input: input}
	if callErr != nil {
		record.ErrorCode = "transport_error"
		if errors.Is(callErr, context.Canceled) {
			record.ErrorCode = "canceled"
		}
		if errors.Is(callErr, context.DeadlineExceeded) {
			record.ErrorCode = "deadline_exceeded"
		}
	} else {
		record.Status, record.Header = response.StatusCode, response.Header.Clone()
		// The provider decoder already has an 8 MiB response limit. Capture
		// rejects overflow instead of hiding extra bytes or growing without bound.
		const responseLimit = 8 << 20
		record.Output, err = io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
		response.Body.Close()
		if err != nil {
			t.failures = append(t.failures, "http_capture_response_incomplete")
			return nil, err
		}
		if len(record.Output) > responseLimit {
			t.failures = append(t.failures, "http_capture_response_overflow")
			return nil, errors.New("http_capture_response_overflow")
		}
		response.Body = io.NopCloser(bytes.NewReader(record.Output))
	}
	record.OutputSHA256 = sum(record.Output)
	f, err := os.OpenFile(t.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		t.failures = append(t.failures, "http_capture_write_failed")
		return nil, err
	}
	err = json.NewEncoder(f).Encode(record)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		t.failures = append(t.failures, "http_capture_write_failed")
		return nil, err
	}
	if closeErr != nil {
		t.failures = append(t.failures, "http_capture_write_failed")
		return nil, closeErr
	}
	return response, callErr
}

func newRecordedHTTP(mode, path string, base http.RoundTripper) (*recordedHTTP, error) {
	if mode != "record" && mode != "replay" {
		return nil, errors.New("http_replay_mode_invalid")
	}
	path, err := privatePath(path)
	if err != nil {
		return nil, err
	}
	t := &recordedHTTP{mode: mode, path: path, base: base}
	if mode != "replay" {
		return t, nil
	}
	t.base = nil
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("http_recording_permissions")
	}
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	for {
		var record httpRecording
		err := decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if record.InputSHA256 != sum(record.Input) || record.OutputSHA256 != sum(record.Output) {
			return nil, errors.New("http_recording_corrupted")
		}
		t.records = append(t.records, record)
	}
	return t, nil
}

type recordedHTTPError string

func (e recordedHTTPError) Error() string { return "recorded_provider_" + string(e) }
func (e recordedHTTPError) Unwrap() error {
	if e == "canceled" {
		return context.Canceled
	}
	if e == "deadline_exceeded" {
		return context.DeadlineExceeded
	}
	return nil
}
