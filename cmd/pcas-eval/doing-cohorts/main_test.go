package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func fixture(t *testing.T) (doing.Suite, doing.Suite, doing.Report, doing.ContextSnapshot) {
	t.Helper()
	old, e := doing.Load("../../../testdata/phase2_5/doing/suite.json")
	if e != nil {
		t.Fatal(e)
	}
	path := "../../../testdata/phase2_5/doing-independent/suite.json"
	s, e := doing.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	r := doing.Report{Version: 1, SuiteSHA: doing.SHA(string(b)), AsOf: s.AsOf, HostDate: "2026-10-04", StartedAt: "2026-10-04T23:01:00Z", Repeats: 3, AnswerLimit: doing.AnswerLimit, AnswerPromptSHA: doing.SHA(doing.AnswerSystem), JudgePromptSHA: doing.SHA(doing.JudgeSystem)}
	snap := doing.ContextSnapshot{SuiteSHA: r.SuiteSHA, AsOf: s.AsOf, HostDate: r.HostDate}
	for _, task := range s.Tasks {
		snap.Entries = append(snap.Entries, doing.ContextEntry{Task: task.ID, RequestSHA: doing.SHA(task.Request)})
		j := doing.Judgment{Checks: []doing.Decision{}, Handling: true}
		for _, g := range [][]doing.Check{task.Must, task.Bonus, task.Forbidden} {
			for _, c := range g {
				j.Checks = append(j.Checks, doing.Decision{ID: c.ID})
			}
		}
		for run := 1; run <= 3; run++ {
			for _, m := range []string{"none", "frozen-current", "ideal"} {
				row := doing.Score(task, [2]doing.Judgment{j, j})
				row.Run = run
				row.Task = task.ID
				row.Category = task.Category
				row.Method = m
				row.ModelCalls = 3
				contextText := ""
				if m == "ideal" {
					contextText = doing.IdealEvidence(s, task)
				}
				row.ContextChars = utf8.RuneCountInString(contextText)
				row.InputChars = utf8.RuneCountInString(doing.AnswerSystem + doing.AnswerPrompt(s, task, contextText))
				r.Rows = append(r.Rows, row)
			}
		}
	}
	return old, s, r, snap
}
func cloneReport(r doing.Report) doing.Report {
	b, _ := json.Marshal(r)
	var out doing.Report
	json.Unmarshal(b, &out)
	return out
}
func TestMatrixAndLineage(t *testing.T) {
	old, s, r, snap := fixture(t)
	if e := unchanged(old, s); e != nil {
		t.Fatal(e)
	}
	if e := validate(r, s, r.SuiteSHA, snap); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*doing.Report){
		"missing":               func(v *doing.Report) { v.Rows = v.Rows[1:] },
		"duplicate":             func(v *doing.Report) { v.Rows[0] = v.Rows[1] },
		"score":                 func(v *doing.Report) { v.Rows[0].MustBoth++ },
		"context":               func(v *doing.Report) { v.Rows[0].InputChars++ },
		"live capture invented": func(v *doing.Report) { v.Rows[0].CaptureCalls = 1 },
		"wrong suite":           func(v *doing.Report) { v.SuiteSHA = "changed" },
		"one judge missing":     func(v *doing.Report) { v.Rows[0].Judgments[1].Checks = nil },
	} {
		t.Run(name, func(t *testing.T) {
			v := cloneReport(r)
			mutate(&v)
			if validate(v, s, r.SuiteSHA, snap) == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	s.Tasks[0].Request = "changed old request"
	if unchanged(old, s) == nil {
		t.Fatal("modified old prefix accepted")
	}
}
func TestCohortsUseRowsAndWeightedChecks(t *testing.T) {
	old, _, r, _ := fixture(t)
	// A direct score-level perturbation demonstrates micro weighting; validate
	// above separately checks that real scores match both judgments.
	r.Rows[0].MustBoth = 1
	out := split(r, old)
	for name, want := range map[string]int{"old120": 1080, "new48": 432, "all168": 1512} {
		if len(out[name].Rows) != want {
			t.Fatalf("%s rows", name)
		}
	}
	mt := 0
	for _, row := range out["all168"].Rows {
		if row.Run == 1 && row.Method == "none" {
			mt += row.MustTotal
		}
	}
	found := false
	for _, summary := range out["all168"].Summaries {
		if summary.Run == 1 && summary.Method == "none" && summary.Category == "all" {
			found = true
			if summary.MustRate != 1/float64(mt) {
				t.Fatal("must rate not micro weighted")
			}
		}
	}
	if !found {
		t.Fatal("missing all summary")
	}
	if !reflect.DeepEqual(out["all168"].Rows[0].Judgments, r.Rows[0].Judgments) {
		t.Fatal("judgments altered")
	}
}

func TestNoiseExposureContainsIdentifiersOnly(t *testing.T) {
	_, s, _, snap := fixture(t)
	for i, e := range snap.Entries {
		if e.Task == "I-NOISE-01" {
			snap.Entries[i].Text = s.ByID()["I-N01a"].Text
		}
	}
	exposed := noiseExposure(s, snap)
	if len(exposed) != 10 {
		t.Fatal("wrong noise cohort")
	}
	for _, e := range exposed {
		if e.Task == "I-NOISE-01" {
			if len(e.Memories) != 2 || e.Memories[0].Memory != "I-N01a" || !e.Memories[0].FullText || e.Memories[1].FullText {
				t.Fatal("wrong exposure measurement")
			}
		}
	}
	b, _ := json.Marshal(exposed)
	if strings.Contains(string(b), s.ByID()["I-N01a"].Text) {
		t.Fatal("memory body leaked")
	}
}
