package doing

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type countedFake struct{ calls atomic.Int64 }

func (m *countedFake) Generate(ctx context.Context, sys, prompt string) (string, error) {
	m.calls.Add(1)
	return (FakeModel{}).Generate(ctx, sys, prompt)
}
func TestConcurrentPipelineAndMetricsOnly(t *testing.T) {
	s, err := Load("../../../testdata/phase2_5/doing/suite.json")
	if err != nil {
		t.Fatal(err)
	}
	s.Tasks = s.Tasks[:2]
	m := &countedFake{}
	p := []Provider{{Name: "none", Get: func(context.Context, Suite, Task) (Evidence, error) { return Evidence{}, nil }}, {Name: "ideal", Get: func(_ context.Context, s Suite, t Task) (Evidence, error) {
		return Evidence{Text: IdealEvidence(s, t)}, nil
	}}}
	r, err := Execute(context.Background(), s, m, p, Report{Repeats: 3}, "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 12 || m.calls.Load() != 36 || len(r.Ranges) != 4 {
		t.Fatalf("bad pipeline counts rows=%d calls=%d ranges=%d", len(r.Rows), m.calls.Load(), len(r.Ranges))
	}
	for _, row := range r.Rows {
		if row.ModelCalls != 3 || row.MustBoth != 0 || row.Usable {
			t.Fatal("fake became quality oracle")
		}
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err = WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte(s.Tasks[0].Request)) || bytes.Contains(raw, []byte(s.Memories[0].Text)) {
		t.Fatal("raw text leaked to numeric report")
	}
}
func TestAtomicReportReplacesSymlinkWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "frozen.txt")
	os.WriteFile(target, []byte("frozen"), 0600)
	path := filepath.Join(dir, "report.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(path, map[string]int{"count": 1}); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(target)
	if string(unchanged) != "frozen" {
		t.Fatal("followed result symlink")
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal("report not private")
	}
}
