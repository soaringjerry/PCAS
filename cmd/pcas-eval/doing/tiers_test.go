package doing

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

type tierJudgeOnly struct{ calls atomic.Int32 }

func (m *tierJudgeOnly) Generate(_ context.Context, system, prompt string) (string, error) {
	if system != JudgeSystem {
		return "", fmt.Errorf("unexpected second answer call")
	}
	m.calls.Add(1)
	return FakeModel{}.Generate(context.Background(), system, prompt)
}
func TestPreparedReplyIsJudgedOnceAndHeavySubsetIsPreserved(t *testing.T) {
	s := Suite{AsOf: "2026-10-04T00:00:00Z", Tasks: []Task{{ID: "fictional-cross", Category: "cross_group", Must: []Check{{ID: "M1"}}}, {ID: "fictional-recall", Category: "direct_recall", Must: []Check{{ID: "M1"}}}}}
	reply := "虚构的现状答复"
	get := func(context.Context, Suite, Task) (Evidence, error) {
		return Evidence{Answer: &reply, ModelCalls: 7, InputChars: 123, ContextChars: 98, Usage: &TierUsage{Requested: "heavy"}}, nil
	}
	model := &tierJudgeOnly{}
	r, err := Execute(context.Background(), s, model, []Provider{{Name: "light", Get: get}, {Name: "heavy", Get: get, Accept: func(t Task) bool { return t.Category == "cross_group" }}}, Report{Repeats: 3}, "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 9 || model.calls.Load() != 18 {
		t.Fatal("wrong answer/judge/subset count", len(r.Rows), model.calls.Load())
	}
	for _, row := range r.Rows {
		if row.ModelCalls != 9 || row.InputChars != 123 || row.ContextChars != 98 || row.TierUsage == nil {
			t.Fatal("lost product metrics", row)
		}
		if row.Method == "heavy" && row.Category != "cross_group" {
			t.Fatal("invented heavy coverage")
		}
	}
}
func TestProductFailureKeepsAttemptedCalls(t *testing.T) {
	row, err := executeRow(context.Background(), Suite{}, Task{ID: "fictional"}, Provider{Name: "heavy", Get: func(context.Context, Suite, Task) (Evidence, error) {
		return Evidence{ModelCalls: 5}, fmt.Errorf("product_failed")
	}}, FakeModel{}, 1)
	if err == nil || row.ModelCalls != 5 {
		t.Fatal("lost attempted reader/main calls", row, err)
	}
}
