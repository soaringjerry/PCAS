package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

// Reuse the checked-in fictional numbers to exercise the exact comparison
// matrix. These synthetic tier rows test accounting, not product quality.
func inputs(t *testing.T) (doing.Suite, string, doing.Report, doing.Report) {
	t.Helper()
	const root = "../../.."
	s, err := doing.Load(root + "/testdata/phase2_5/doing/suite.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(root + "/testdata/phase2_5/doing/suite.json")
	if err != nil {
		t.Fatal(err)
	}
	var b doing.Report
	if _, err = read(root+"/docs/evaluations/2026-10-04-phase2_5-v2-artifacts/result.json.gz", &b); err != nil {
		t.Fatal(err)
	}
	r := b
	r.Workers = 4
	r.HostDate = "2026-10-05"
	r.Rows = nil
	r.Preparation = &doing.Preparation{Complete: true, Handover: true, HandoverInputs: true, Stages: []doing.PreparationStage{
		{Stage: "organize", JobsDone: 1, WallMS: 1}, {Stage: "compare", JobsDone: 1, WallMS: 1}, {Stage: "status", JobsDone: 1, WallMS: 1},
	}}
	for _, row := range b.Rows {
		if row.Method != "current" {
			continue
		}
		for _, method := range []string{"light", "medium", "heavy"} {
			if method == "heavy" && !heavy(doing.Task{Category: row.Category}) {
				continue
			}
			v := row
			v.Method = method
			v.TierUsage = &doing.TierUsage{Requested: method, Effective: method, Calls: map[string]int{"secretary": 1}}
			r.Rows = append(r.Rows, v)
		}
	}
	return s, doing.SHA(string(raw)), b, r
}

func TestScopeAndRecomputation(t *testing.T) {
	s, sha, b, r := inputs(t)
	c, err := compare(s, sha, b, r)
	if err != nil {
		t.Fatal(err)
	}
	for name, scope := range c.Scopes {
		for _, v := range scope.Summaries {
			if v.Category != "all" {
				continue
			}
			if v.Tasks != scope.Tasks {
				t.Fatalf("%s/%s: %d tasks, want %d", name, v.Method, v.Tasks, scope.Tasks)
			}
			if name == "old120" && v.Method == "heavy" {
				t.Fatal("heavy40 was mixed into old120")
			}
		}
	}
	if c.Usage["heavy"].Rows != 120 || c.Usage["light"].Rows != 360 || c.Usage["medium"].Rows != 360 || len(c.Missing) != 2 {
		t.Fatal("wrong matrix or omitted coverage warning")
	}
}

func TestRejectIncompleteOrChangedInputs(t *testing.T) {
	s, sha, b, clean := inputs(t)
	for _, tc := range []struct {
		name string
		edit func(*doing.Report)
	}{
		{"fake", func(r *doing.Report) { r.Fake = true }},
		{"missing", func(r *doing.Report) { r.Rows = r.Rows[1:] }},
		{"duplicate", func(r *doing.Report) { r.Rows = append(r.Rows, r.Rows[0]) }},
		{"failure", func(r *doing.Report) { r.Failures = []doing.Failure{{Error: "timeout"}} }},
		{"score", func(r *doing.Report) { r.Rows[0].Usable = !r.Rows[0].Usable }},
		{"calls", func(r *doing.Report) { r.Rows[0].ModelCalls++ }},
		{"preparation", func(r *doing.Report) { r.Preparation.Complete = false }},
		{"fingerprint", func(r *doing.Report) { r.AnswerPromptSHA = "changed" }},
		{"scope", func(r *doing.Report) { r.Rows[0].Category = "direct_recall" }},
		{"model", func(r *doing.Report) { r.Model = "different" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := json.Marshal(clean)
			var r doing.Report
			if err := json.Unmarshal(buf, &r); err != nil {
				t.Fatal(err)
			}
			tc.edit(&r)
			if _, err := compare(s, sha, b, r); err == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
}

func TestAnswerLimitRemainsPartOfUsability(t *testing.T) {
	s, sha, b, r := inputs(t)
	for i := range r.Rows {
		if r.Rows[i].Usable {
			r.Rows[i].AnswerChars = doing.AnswerLimit + 1
			r.Rows[i].Usable = false
			if _, err := compare(s, sha, b, r); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("test fixture needs one usable answer")
}

func TestCompletedLegacyFallbackStillReportable(t *testing.T) {
	s, sha, b, r := inputs(t)
	r.Preparation.HandoverInputs, r.Preparation.Handover = false, false
	for i := range r.Rows {
		r.Rows[i].TierUsage.Effective = "legacy-fallback"
	}
	c, err := compare(s, sha, b, r)
	if err != nil {
		t.Fatal(err)
	}
	if c.Usage["heavy"].Effective["legacy-fallback"] != 120 {
		t.Fatal("lost effective fallback tier")
	}
}
