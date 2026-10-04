// doing-merge reconstructs this interrupted synthetic three-run experiment.
// Selection depends only on missing transport results and changed gold, never scores.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func read(path string, v any) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return doing.SHA(string(b)), json.Unmarshal(b, v)
}

type source struct {
	SHA      string `json:"sha256"`
	Revision string `json:"revision"`
	Rows     int    `json:"completed_rows"`
	Retained int    `json:"retained_rows"`
	Calls    int    `json:"completed_row_model_calls"`
}

func run() error {
	oldPath := flag.String("original-suite", "", "frozen suite used for original partial run")
	newPath := flag.String("suite", "testdata/phase2_5/doing/suite.json", "final suite")
	basePath := flag.String("original", "", "interrupted three-run numeric report")
	completionPath := flag.String("completion", "", "one-run report: every unchanged task missing any third-run method")
	repairPath := flag.String("replacement", "", "three-run report: all tasks whose gold changed")
	out := flag.String("output", "", "synthetic report prefix; never use for private data")
	flag.Parse()
	if flag.NArg() != 0 || *out == "" {
		return fmt.Errorf("output required; no positional arguments")
	}
	oldSuite, err := doing.Load(*oldPath)
	if err != nil {
		return err
	}
	newSuite, err := doing.Load(*newPath)
	if err != nil {
		return err
	}
	if !oldSuite.Synthetic || !newSuite.Synthetic {
		return fmt.Errorf("merge accepts synthetic suites only")
	}
	var base, completion, repair doing.Report
	oldSHA, err := read(*oldPath, &oldSuite)
	if err != nil {
		return err
	}
	newSHA, err := read(*newPath, &newSuite)
	if err != nil {
		return err
	}
	paths := []string{*basePath, *completionPath, *repairPath}
	reports := []*doing.Report{&base, &completion, &repair}
	sources := make([]source, 3)
	for i, p := range paths {
		sha, err := read(p, reports[i])
		if err != nil {
			return err
		}
		sources[i] = source{SHA: sha, Revision: reports[i].Revision, Rows: len(reports[i].Rows)}
		for _, row := range reports[i].Rows {
			sources[i].Calls += row.ModelCalls
		}
	}
	if base.Repeats != 3 || completion.Repeats != 1 || repair.Repeats != 3 || base.SuiteSHA != oldSHA || completion.SuiteSHA != newSHA || repair.SuiteSHA != newSHA {
		return fmt.Errorf("source suite hashes or repetitions mismatch")
	}
	if len(completion.Failures) > 0 || len(repair.Failures) > 0 {
		return fmt.Errorf("replacement runs must be complete")
	}
	for _, r := range reports {
		if r.Fake || r.Version != 1 || r.AsOf != newSuite.AsOf || r.AnswerPromptSHA != doing.SHA(doing.AnswerSystem) || r.JudgePromptSHA != doing.SHA(doing.JudgeSystem) || r.AnswerLimit != doing.AnswerLimit || r.Model != base.Model || r.Channel != base.Channel || r.HostDate != base.HostDate || r.Workers != base.Workers {
			return fmt.Errorf("source execution conditions mismatch")
		}
	}
	// No corpus or task topology change can be repaired by retaining old rows.
	oldTasks, newTasks := oldSuite.Tasks, newSuite.Tasks
	oldSuite.Tasks = nil
	newSuite.Tasks = nil
	if !reflect.DeepEqual(oldSuite, newSuite) || len(oldTasks) != len(newTasks) {
		return fmt.Errorf("corpus or topology changed")
	}
	changed := map[string]bool{}
	tasks := map[string]doing.Task{}
	for i, t := range newTasks {
		if t.ID != oldTasks[i].ID {
			return fmt.Errorf("task order/topology changed")
		}
		tasks[t.ID] = t
		if !reflect.DeepEqual(t, oldTasks[i]) {
			changed[t.ID] = true
		}
	}
	methods := []string{"none", "current", "ideal"}
	key := func(row doing.Row) string { return fmt.Sprintf("%d/%s/%s", row.Run, row.Task, row.Method) }
	seen := map[string]bool{}
	for _, row := range base.Rows {
		if seen[key(row)] {
			return fmt.Errorf("duplicate original row")
		}
		seen[key(row)] = true
	}
	missing := map[string]bool{}
	for id := range tasks {
		for _, method := range methods {
			for run := 1; run <= 2; run++ {
				if !seen[key(doing.Row{Run: run, Task: id, Method: method})] {
					return fmt.Errorf("original first two repetitions incomplete")
				}
			}
			if !changed[id] && !seen[key(doing.Row{Run: 3, Task: id, Method: method})] {
				missing[id] = true
			}
		}
	}
	rows := []doing.Row{}
	for i, r := range reports {
		for _, row := range r.Rows {
			if _, ok := tasks[row.Task]; !ok {
				return fmt.Errorf("unknown source task")
			}
			keep := false
			switch i {
			case 0:
				keep = !changed[row.Task] && !(row.Run == 3 && missing[row.Task])
			case 1:
				if !missing[row.Task] || row.Run != 1 {
					return fmt.Errorf("completion selection mismatch")
				}
				row.Run = 3
				keep = true
			case 2:
				if !changed[row.Task] {
					return fmt.Errorf("replacement selection mismatch")
				}
				keep = true
			}
			if keep {
				rows = append(rows, row)
				sources[i].Retained++
			}
		}
	}
	seen = map[string]bool{}
	for _, row := range rows {
		if row.Run < 1 || row.Run > 3 || seen[key(row)] {
			return fmt.Errorf("duplicate or invalid merged row")
		}
		seen[key(row)] = true
		if row.Method != "none" && row.Method != "current" && row.Method != "ideal" {
			return fmt.Errorf("unknown method")
		}
		t := tasks[row.Task]
		for _, j := range row.Judgments {
			b, _ := json.Marshal(j)
			if _, err := doing.ParseJudgment(string(b), t); err != nil {
				return err
			}
		}
		s := doing.Score(t, row.Judgments)
		if row.Category != t.Category || row.MustTotal != s.MustTotal || row.MustBoth != s.MustBoth || row.BonusTotal != s.BonusTotal || row.BonusBoth != s.BonusBoth || row.ForbiddenTotal != s.ForbiddenTotal || row.ForbiddenEither != s.ForbiddenEither || row.Usable != (s.Usable && row.AnswerChars <= doing.AnswerLimit) || !reflect.DeepEqual(row.Disagreements, s.Disagreements) || row.ModelCalls != 3 || (row.Method == "current" && row.CaptureCalls != 1) || (row.Method != "current" && row.CaptureCalls != 0) {
			return fmt.Errorf("row score or measurement invalid: %s", key(row))
		}
	}
	if len(rows) != len(tasks)*len(methods)*3 {
		return fmt.Errorf("merged run is incomplete")
	}
	sort.Slice(rows, func(i, j int) bool { return key(rows[i]) < key(rows[j]) })
	merged := base
	merged.Rows = rows
	merged.Failures = nil
	merged.SuiteSHA = newSHA
	merged.Notes = append(merged.Notes, "Reconstructed from an interrupted run, one completion run (mapped to repetition 3), and all-three-repetition gold repair. Selection used transport completeness and changed task definitions only; see provenance.json. Row times include each row's actual measurements, not the wall time of discarded work.")
	merged.Summaries, merged.Ranges = doing.Aggregate(rows, 3)
	if err := doing.WriteJSON(*out+".json", merged); err != nil {
		return err
	}
	if err := doing.WriteMarkdown(*out+".md", merged); err != nil {
		return err
	}
	ids := func(m map[string]bool) []string {
		v := []string{}
		for id := range m {
			v = append(v, id)
		}
		sort.Strings(v)
		return v
	}
	return doing.WriteJSON(*out+".provenance.json", struct {
		Sources       []source `json:"sources_original_completion_replacement"`
		OriginalSuite string   `json:"original_suite_sha256"`
		FinalSuite    string   `json:"final_suite_sha256"`
		Changed       []string `json:"changed_task_ids"`
		Missing       []string `json:"completion_task_ids"`
		Note          string   `json:"note"`
	}{sources, oldSHA, newSHA, ids(changed), ids(missing), "Every source file is numeric only. Source revision identifies evaluation code; product implementation stayed at original revision. Completed source-row calls exclude preflights and failed/canceled attempts; the original driver did not persist those attempt counts."})
}
