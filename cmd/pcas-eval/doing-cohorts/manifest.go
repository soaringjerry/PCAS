package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

type numericEntry struct {
	Task       string  `json:"task"`
	RequestSHA string  `json:"request_sha256"`
	ContextSHA string  `json:"context_sha256"`
	Chars      int     `json:"context_chars"`
	JSONChars  int     `json:"context_json_chars"`
	MS         float64 `json:"capture_ms"`
	Calls      int     `json:"local_capture_calls"`
}
type captureManifest struct {
	Schema          int            `json:"schema_version"`
	Fake            bool           `json:"fake"`
	SourceSHA       string         `json:"source_uncompressed_sha256"`
	SnapshotSHA     string         `json:"snapshot_sha256"`
	SuiteSHA        string         `json:"suite_sha256"`
	BaseSHA         string         `json:"base_suite_sha256"`
	CaptureRevision string         `json:"capture_product_revision"`
	ModelRevision   string         `json:"model_run_revision"`
	AsOf            string         `json:"as_of"`
	CaptureStart    string         `json:"capture_started_at"`
	CaptureEnd      string         `json:"capture_completed_at"`
	ModelStart      string         `json:"model_run_started_at"`
	Entries         []numericEntry `json:"entries"`
	Exposure        []exposure     `json:"noise_exposure"`
	Note            string         `json:"note"`
}

func recordedSnapshot(m captureManifest, r doing.Report, s doing.Suite, sourceSHA, baseSHA string) (doing.ContextSnapshot, map[string]int, error) {
	var snap doing.ContextSnapshot
	if m.Schema != 1 || m.Fake != r.Fake || m.SourceSHA != sourceSHA || m.BaseSHA != baseSHA || m.SuiteSHA != r.SuiteSHA || m.AsOf != s.AsOf || m.ModelRevision != r.Revision || m.ModelStart != r.StartedAt || len(m.SnapshotSHA) != 64 {
		return snap, nil, fmt.Errorf("numeric manifest lineage mismatch")
	}
	start, e1 := time.Parse(time.RFC3339, m.CaptureStart)
	end, e2 := time.Parse(time.RFC3339, m.CaptureEnd)
	if e1 != nil || e2 != nil || end.Before(start) || start.UTC().Format("2006-01-02") != s.AsOf[:10] || end.UTC().Format("2006-01-02") != s.AsOf[:10] {
		return snap, nil, fmt.Errorf("numeric manifest capture dates invalid")
	}
	snap = doing.ContextSnapshot{Version: 1, Synthetic: true, SuiteSHA: m.SuiteSHA, AsOf: m.AsOf, HostDate: s.AsOf[:10], Revision: m.CaptureRevision, StartedAt: m.CaptureStart, CompletedAt: m.CaptureEnd}
	sizes := map[string]int{}
	for _, v := range m.Entries {
		if _, seen := sizes[v.Task]; seen || v.Chars < 0 || v.JSONChars < v.Chars+2 || v.MS < 0 || v.Calls != 1 || len(v.ContextSHA) != 64 || len(v.RequestSHA) != 64 {
			return snap, nil, fmt.Errorf("numeric capture entry invalid")
		}
		sizes[v.Task] = v.JSONChars
		snap.Entries = append(snap.Entries, doing.ContextEntry{Task: v.Task, RequestSHA: v.RequestSHA, ContextSHA: v.ContextSHA, ContextChars: v.Chars, CaptureMS: v.MS, CaptureCalls: v.Calls})
	}
	// The numeric record proves scores/sizes/lineage, not the semantic content of
	// an unavailable context. Exposures are retained as recorded observations.
	expected := noiseExposure(s, snap)
	if len(expected) != len(m.Exposure) {
		return snap, nil, fmt.Errorf("noise exposure topology mismatch")
	}
	for i, want := range expected {
		got := m.Exposure[i]
		if got.Task != want.Task || len(got.Memories) != len(want.Memories) {
			return snap, nil, fmt.Errorf("noise exposure topology mismatch")
		}
		for j, mem := range want.Memories {
			if got.Memories[j].Memory != mem.Memory {
				return snap, nil, fmt.Errorf("noise exposure reference mismatch")
			}
		}
	}
	return snap, sizes, nil
}
func recalculate(base, suite, source, manifest, out string, allowFake bool) error {
	old, err := doing.Load(base)
	if err != nil {
		return err
	}
	s, err := doing.Load(suite)
	if err != nil {
		return err
	}
	if err = unchanged(old, s); err != nil {
		return err
	}
	var r doing.Report
	sourceSHA, err := read(source, &r)
	if err != nil {
		return err
	}
	if r.Fake != allowFake {
		return fmt.Errorf("fake flag mismatch")
	}
	var m captureManifest
	if _, err = read(manifest, &m); err != nil {
		return err
	}
	baseBytes, err := os.ReadFile(base)
	if err != nil {
		return err
	}
	snap, sizes, err := recordedSnapshot(m, r, s, sourceSHA, doing.SHA(string(baseBytes)))
	if err != nil {
		return err
	}
	suiteBytes, err := os.ReadFile(suite)
	if err != nil {
		return err
	}
	if err = validateMeasured(r, s, doing.SHA(string(suiteBytes)), snap, sizes); err != nil {
		return err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	for name, report := range split(r, old) {
		if err = doing.WriteJSON(filepath.Join(out, name+".json"), report); err != nil {
			return err
		}
		if err = doing.WriteMarkdown(filepath.Join(out, name+".md"), report); err != nil {
			return err
		}
	}
	if err = doing.WriteJSON(filepath.Join(out, "noise-exposure.json"), m.Exposure); err != nil {
		return err
	}
	return doing.WriteJSON(filepath.Join(out, "capture-manifest.json"), m)
}
