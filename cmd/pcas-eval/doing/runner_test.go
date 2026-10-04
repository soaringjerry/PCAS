package doing

import (
	"encoding/json"
	"strings"
	"testing"
)

func scoreTask() Task {
	return Task{Must: []Check{{ID: "M1"}, {ID: "M2"}, {ID: "M3"}}, Bonus: []Check{{ID: "B1"}}, Forbidden: []Check{{ID: "F1"}}}
}
func judgment(v ...bool) Judgment {
	ids := []string{"M1", "M2", "M3", "B1", "F1"}
	j := Judgment{Handling: true}
	for i, id := range ids {
		j.Checks = append(j.Checks, Decision{id, v[i]})
	}
	return j
}
func TestConservativeDoubleJudge(t *testing.T) {
	task := scoreTask()
	a := judgment(true, true, true, true, false)
	b := judgment(true, true, false, true, true)
	r := Score(task, [2]Judgment{a, b})
	if r.MustBoth != 2 || r.BonusBoth != 1 || r.ForbiddenEither != 1 || r.Usable || strings.Join(r.Disagreements, ",") != "M3,F1" {
		t.Fatalf("bad disagreement score %+v", r)
	}
	good := Score(task, [2]Judgment{a, a})
	if !good.Usable || good.MustBoth != 3 {
		t.Fatal("unanimous pass not usable")
	}
}
func TestJudgmentRejectsMalformedOrIncomplete(t *testing.T) {
	task := scoreTask()
	valid := jsonText(judgment(true, true, true, false, false))
	for _, raw := range []string{valid + `{}`, strings.Replace(valid, `"handling":true`, `"handling":null`, 1), strings.Replace(valid, `"handling":true`, `"unexpected":true`, 1), strings.Replace(valid, `"id":"M2"`, `"id":"M1"`, 1), strings.Replace(valid, `"value":true`, `"missing":true`, 1)} {
		if _, err := ParseJudgment(raw, task); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := ParseJudgment(valid, task); err != nil {
		t.Fatal(err)
	}
}
func TestMicroAverageAndThreeRunFloor(t *testing.T) {
	rows := []Row{}
	for run := 1; run <= 3; run++ {
		rows = append(rows, Row{Run: run, Method: "x", Category: "direct_recall", MustTotal: 3, MustBoth: run, Usable: run == 3, ForbiddenEither: 4 - run}, Row{Run: run, Method: "x", Category: "irrelevant", MustTotal: 6, MustBoth: 6})
	}
	s, r := Aggregate(rows, 3)
	if len(s) != 9 || len(r) != 3 {
		t.Fatal("wrong category aggregate")
	}
	for _, v := range r {
		if v.Category == "all" {
			if v.MustMin != 7.0/9 || v.MustMax != 1 || v.UsableMax != 0.5 || v.ForbiddenMin != 1 || v.ForbiddenMax != 3 || v.IndifferencePP != 50 {
				t.Fatalf("wrong floor %+v", v)
			}
		}
	}
	_, few := Aggregate(rows[:2], 1)
	if len(few) != 0 {
		t.Fatal("made variability from one run")
	}
}
func TestJudgeGetsOnlyCheckEvidenceAndNoMethod(t *testing.T) {
	s, err := Load("../../../testdata/phase2_5/doing/suite.json")
	if err != nil {
		t.Fatal(err)
	}
	prompt := JudgePrompt(s, s.Tasks[0], "answer")
	if strings.Contains(prompt, "NOISE04002") || strings.Contains(prompt, "superseded_by") {
		t.Fatal("entire memory corpus or metadata leaked to judge")
	}
	var in map[string]json.RawMessage
	if json.Unmarshal([]byte(prompt), &in) != nil || in["method"] != nil {
		t.Fatal("judge knows answer method")
	}
}

func TestPrivateApprovalGate(t *testing.T) {
	s, err := Load("../../../testdata/phase2_5/doing/suite.json")
	if err != nil {
		t.Fatal(err)
	}
	s.Synthetic = false
	s.Tasks = s.Tasks[:1]
	if s.Validate() == nil {
		t.Fatal("unreviewed private standard accepted")
	}
	s.Tasks[0].Reviewed = true
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
}
