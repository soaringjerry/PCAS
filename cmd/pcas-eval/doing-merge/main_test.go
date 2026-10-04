package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

// Reconstruct a transport gap and a changed gold item; reject conditions that
// would silently mix prompts, omit methods, or mislabel a result's category.
func TestMergeCompletenessAndConditions(t *testing.T) {
	for _, bad := range []string{"", "prompt", "missing_method", "category"} {
		t.Run(bad, func(t *testing.T) {
			s, err := doing.Load("../../../testdata/phase2_5/doing/suite.json")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			write := func(name string, v any) (string, string) {
				t.Helper()
				b, _ := json.Marshal(v)
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, b, 0600); err != nil {
					t.Fatal(err)
				}
				return p, doing.SHA(string(b))
			}
			oldPath, oldSHA := write("old.json", s)
			s.Tasks[0].Must[0].Evidence = append(s.Tasks[0].Must[0].Evidence, "P003")
			newPath, newSHA := write("new.json", s)
			r := doing.Report{Version: 1, Revision: "test-evaluation-only", SuiteSHA: newSHA, AnswerPromptSHA: doing.SHA(doing.AnswerSystem), JudgePromptSHA: doing.SHA(doing.JudgeSystem), Model: "synthetic-unit-test", Channel: "test", AsOf: s.AsOf, HostDate: "2026-10-04", Repeats: 3, Workers: 8, AnswerLimit: doing.AnswerLimit}
			base, completion, replacement := r, r, r
			base.SuiteSHA = oldSHA
			completion.Repeats = 1
			for _, task := range s.Tasks {
				for run := 1; run <= 3; run++ {
					for _, method := range []string{"none", "current", "ideal"} {
						checks := []doing.Decision{}
						for _, group := range [][]doing.Check{task.Must, task.Bonus, task.Forbidden} {
							for _, check := range group {
								checks = append(checks, doing.Decision{ID: check.ID, Value: false})
							}
						}
						row := doing.Score(task, [2]doing.Judgment{{Checks: checks, Handling: true}, {Checks: checks, Handling: true}})
						row.Run = run
						row.Task = task.ID
						row.Category = task.Category
						row.Method = method
						row.ModelCalls = 3
						if method == "current" {
							row.CaptureCalls = 1
						}
						if task.ID == s.Tasks[0].ID {
							replacement.Rows = append(replacement.Rows, row)
						}
						if task.ID == s.Tasks[1].ID && run == 3 {
							if method != "none" {
								base.Rows = append(base.Rows, row)
							}
							row.Run = 1
							completion.Rows = append(completion.Rows, row)
						} else {
							base.Rows = append(base.Rows, row)
						}
					}
				}
			}
			switch bad {
			case "prompt":
				completion.AnswerPromptSHA = "wrong"
			case "missing_method":
				completion.Rows = completion.Rows[:2]
			case "category":
				completion.Rows[0].Category = "irrelevant"
			}
			basePath, _ := write("base.json", base)
			completionPath, _ := write("completion.json", completion)
			replacementPath, _ := write("replacement.json", replacement)
			priorFlags, priorArgs := flag.CommandLine, os.Args
			defer func() { flag.CommandLine = priorFlags; os.Args = priorArgs }()
			flag.CommandLine = flag.NewFlagSet("merge-test", flag.ContinueOnError)
			out := filepath.Join(dir, "merged")
			os.Args = []string{"merge-test", "-original-suite", oldPath, "-suite", newPath, "-original", basePath, "-completion", completionPath, "-replacement", replacementPath, "-output", out}
			err = run()
			if bad != "" {
				if err == nil {
					t.Fatal("invalid reconstruction accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result doing.Report
			if _, err := read(out+".json", &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Rows) != 1080 || len(result.Summaries) != 63 || len(result.Ranges) != 21 {
				t.Fatalf("incomplete reconstruction: %d rows", len(result.Rows))
			}
		})
	}
}
