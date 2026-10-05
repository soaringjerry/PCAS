package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func TestTierRepairSkipsCompletedCellsRegardlessOfScore(t *testing.T) {
	s := doing.Suite{Tasks: []doing.Task{
		{ID: "A", Category: "cross_group", Must: []doing.Check{{ID: "M1"}}, Forbidden: []doing.Check{{ID: "F1"}}},
		{ID: "B", Category: "outgoing", Must: []doing.Check{{ID: "M1"}}, Forbidden: []doing.Check{{ID: "F1"}}},
	}}
	completed := []doing.Row{}
	for i, task := range s.Tasks {
		j := doing.Judgment{Checks: []doing.Decision{{ID: "M1", Value: i == 0}, {ID: "F1", Value: false}}, Handling: true}
		r := doing.Score(task, [2]doing.Judgment{j, j})
		r.Run, r.Task, r.Category, r.Method = 1, task.ID, task.Category, []string{"light", "medium"}[i]
		completed = append(completed, r)
	}
	var mu sync.Mutex
	calls := map[string]int{}
	providers := []doing.Provider{}
	for _, method := range []string{"light", "medium"} {
		providers = append(providers, doing.Provider{Name: method, Get: func(_ context.Context, _ doing.Suite, t doing.Task) (doing.Evidence, error) {
			mu.Lock()
			calls[method+"/"+t.ID]++
			mu.Unlock()
			answer := "假模型流程草稿"
			return doing.Evidence{Answer: &answer, ModelCalls: 1}, nil
		}})
	}
	r, err := doing.Execute(context.Background(), s, doing.FakeModel{}, missingTierProviders(providers, completed), doing.Report{Repeats: 3, Rows: append([]doing.Row{}, completed...)}, "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 12 || !reflect.DeepEqual(r.Rows[:2], completed) || !reflect.DeepEqual(calls, map[string]int{"light/A": 2, "light/B": 3, "medium/A": 3, "medium/B": 2}) {
		t.Fatal("repair reran or changed a completed result", calls)
	}
}

func TestTierResumeFingerprintsAndScores(t *testing.T) {
	const suitePath = "../../testdata/phase2_5/doing/suite.json"
	s, err := doing.Load(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(suitePath)
	task := s.Tasks[0]
	j := doing.Judgment{Handling: true}
	for _, group := range [][]doing.Check{task.Must, task.Bonus, task.Forbidden} {
		for _, c := range group {
			j.Checks = append(j.Checks, doing.Decision{ID: c.ID})
		}
	}
	row := doing.Score(task, [2]doing.Judgment{j, j})
	row.Run, row.Task, row.Category, row.Method, row.ModelCalls = 1, task.ID, task.Category, "light", 3
	row.TierUsage = &doing.TierUsage{Requested: "light", Calls: map[string]int{"secretary": 1}}
	r := doing.Report{Version: 1, Repeats: 3, Workers: 4, Model: "test", Channel: "test", HostDate: time.Now().UTC().Format("2006-01-02"), AsOf: s.AsOf, SuiteSHA: doing.SHA(string(raw)), AnswerPromptSHA: doing.SHA(doing.AnswerSystem), JudgePromptSHA: doing.SHA(doing.JudgeSystem), AnswerLimit: doing.AnswerLimit, Preparation: &doing.Preparation{Complete: true, Stages: make([]doing.PreparationStage, 3)}, Rows: []doing.Row{row}}
	dir, err := os.MkdirTemp("/var/tmp", "pcas-v2c-resume-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "numeric.json")
	if err := doing.WriteJSON(path, r); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadTierResume(path, suitePath, s, false, "test", "test", 4); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*doing.Report){
		func(r *doing.Report) { r.HostDate = "2000-01-01" },
		func(r *doing.Report) { r.SuiteSHA = "changed" },
		func(r *doing.Report) { r.Rows[0].Usable = true },
		func(r *doing.Report) { r.Rows[0].ModelCalls++ },
		func(r *doing.Report) { r.Rows = append(r.Rows, r.Rows[0]) },
	} {
		copy := r
		copy.Rows = append([]doing.Row{}, r.Rows...)
		change(&copy)
		if err := doing.WriteJSON(path, copy); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadTierResume(path, suitePath, s, false, "test", "test", 4); err == nil {
			t.Fatal("accepted changed resume data")
		}
	}
	if err := runDoingTiers([]string{"-fake", "-validate", "-methods=light,medium,heavy", "-heavy-categories=cross_group,outgoing", "-tasks=CROSS-01", "-resume-report=" + path}); err == nil {
		t.Fatal("accepted resume with task filtering")
	}
}
