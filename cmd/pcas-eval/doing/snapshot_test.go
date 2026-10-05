package doing

import (
	"path/filepath"
	"testing"
)

func TestSnapshotIntegrity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	good := ContextSnapshot{Version: 1, Synthetic: true, SuiteSHA: SHA("suite"), AsOf: "2026-10-04T09:00:00Z", HostDate: "2026-10-04", StartedAt: "2026-10-04T23:00:00Z", CompletedAt: "2026-10-04T23:01:00Z", Entries: []ContextEntry{{Task: "T1", RequestSHA: SHA("request"), Text: "虚构", ContextSHA: SHA("虚构"), ContextChars: 2, CaptureCalls: 1}}}
	if e := WriteJSON(path, good); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadSnapshot(path); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*ContextSnapshot){
		"private":            func(s *ContextSnapshot) { s.Synthetic = false },
		"cross midnight":     func(s *ContextSnapshot) { s.CompletedAt = "2026-10-05T00:00:01Z" },
		"wrong anchor":       func(s *ContextSnapshot) { s.AsOf = "2026-10-05T09:00:00Z" },
		"changed context":    func(s *ContextSnapshot) { s.Entries[0].Text = "another" },
		"wrong chars":        func(s *ContextSnapshot) { s.Entries[0].ContextChars = 3 },
		"fake extra capture": func(s *ContextSnapshot) { s.Entries[0].CaptureCalls = 2 },
		"duplicate":          func(s *ContextSnapshot) { s.Entries = append(s.Entries, s.Entries[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			v := good
			v.Entries = append([]ContextEntry{}, good.Entries...)
			mutate(&v)
			if e := WriteJSON(path, v); e != nil {
				t.Fatal(e)
			}
			if _, e := LoadSnapshot(path); e == nil {
				t.Fatal("bad snapshot accepted")
			}
		})
	}
}
