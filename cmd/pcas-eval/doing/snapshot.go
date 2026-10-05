package doing

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"
)

type ContextEntry struct {
	Task         string  `json:"task"`
	RequestSHA   string  `json:"request_sha256"`
	Text         string  `json:"text"`
	ContextSHA   string  `json:"context_sha256"`
	ContextChars int     `json:"context_chars"`
	CaptureMS    float64 `json:"capture_ms"`
	CaptureCalls int     `json:"local_capture_calls"`
}
type ContextSnapshot struct {
	Version     int            `json:"schema_version"`
	Synthetic   bool           `json:"synthetic"`
	SuiteSHA    string         `json:"suite_sha256"`
	AsOf        string         `json:"as_of"`
	HostDate    string         `json:"host_date"`
	Revision    string         `json:"revision"`
	StartedAt   string         `json:"started_at"`
	CompletedAt string         `json:"completed_at"`
	Entries     []ContextEntry `json:"entries"`
}

func LoadSnapshot(path string) (ContextSnapshot, error) {
	var s ContextSnapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Version != 1 || !s.Synthetic || s.SuiteSHA == "" || len(s.AsOf) < 10 || len(s.StartedAt) < 10 || len(s.CompletedAt) < 10 || s.HostDate != s.AsOf[:10] || s.StartedAt[:10] != s.HostDate || s.CompletedAt[:10] != s.HostDate || len(s.Entries) == 0 {
		return s, fmt.Errorf("invalid snapshot conditions")
	}
	seen := map[string]bool{}
	for _, e := range s.Entries {
		if e.Task == "" || seen[e.Task] || e.RequestSHA == "" || e.ContextSHA != SHA(e.Text) || e.ContextChars != utf8.RuneCountInString(e.Text) || e.CaptureMS < 0 || e.CaptureCalls != 1 {
			return s, fmt.Errorf("invalid snapshot entry")
		}
		seen[e.Task] = true
	}
	return s, nil
}
