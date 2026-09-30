package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestAdoptionMatchesFrontend(t *testing.T) {
	cases := []struct {
		Name   string   `json:"name"`
		Kind   string   `json:"kind"`
		Output string   `json:"output"`
		As     string   `json:"as"`
		Lines  []string `json:"lines"`
	}{
		{"checklist", "breakdown", "- [ ] 第一项\n- [ ] 第二项\n- [ ] 第三项", "subtasks", []string{"第一项", "第二项", "第三项"}},
		{"embedded checklist", "draft", "介绍\n\n- [ ] 1. 保留序号\n结语\n- [x] 已完成", "subtasks", []string{"1. 保留序号", "已完成"}},
		{"summary", "summary", "完成了设计，等待审核", "progress", []string{}},
		{"summary with checklist", "summary", "进度\n- [x] 下一步", "subtasks", []string{"下一步"}},
		{"long article", "draft", strings.Repeat("普通正文。", 1000), "doc", []string{}},
		{"empty lines", "plan", "\n\n", "doc", []string{}},
		{"checked", "breakdown", "- [x] 完成\n- [ ] 待办", "subtasks", []string{"完成", "待办"}},
		{"invalid syntax", "plan", "- [X] 大写\n-[ ] 少空格\n* [ ] 星号\n- [] 空框\n- [ ]   ", "doc", []string{}},
		{"CRLF", "plan", "  - [ ] 第一项\r\n\t- [x]\t第二项\r\n", "doc", []string{}},
		{"unicode whitespace", "plan", "\ufeff- [ ]\u00a0第一项\u3000\n\u2003- [x] 第二项", "subtasks", []string{"第一项", "第二项"}},
		{"preserve non JS whitespace", "draft", "- [ ] \u0085text\u0085", "subtasks", []string{"\u0085text\u0085"}},
		{"non JS whitespace", "draft", "\u0085- [ ] 非 JS 空白", "doc", []string{}},
		{"line separators", "draft", "- [ ] a\u2028b\n- [ ] c\u2029", "doc", []string{}},
	}
	for _, tc := range cases {
		for _, itemKind := range []string{"task", "idea", "project"} {
			t.Run(tc.Name+"/"+itemKind, func(t *testing.T) {
				if got := adoptionFor(workspace.Item{Kind: itemKind}, workspace.Run{Kind: tc.Kind}, tc.Output); got != tc.As {
					t.Fatalf("as=%q want %q", got, tc.As)
				}
				if got := parseRunChecklist(tc.Output); !reflect.DeepEqual(got, tc.Lines) {
					t.Fatalf("lines=%q want %q", got, tc.Lines)
				}
			})
		}
	}
	// Execute the actual frontend function bodies. Only erase TypeScript signatures;
	// do not maintain a second copy of the frontend routing or regex in this test.
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the live frontend equivalence check")
	}
	agent, err := os.ReadFile("../../web/src/domain/agent.ts")
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile("../../web/src/pages/ThingPage.tsx")
	if err != nil {
		t.Fatal(err)
	}
	extract := func(source, marker, signature string) string {
		t.Helper()
		start := strings.Index(source, marker)
		if start < 0 {
			t.Fatalf("frontend function %q missing", marker)
		}
		rest := source[start:]
		end := strings.Index(rest, "\n}")
		newline := strings.Index(rest, "\n")
		if end < 0 || newline < 0 {
			t.Fatalf("cannot extract %q", marker)
		}
		return signature + rest[newline:end+2]
	}
	script := extract(string(agent), "export function parseChecklist(", "function parseChecklist(output) {")
	script = strings.ReplaceAll(script, ".filter((l): l is string => Boolean(l))", ".filter((l) => Boolean(l))")
	script += "\n" + extract(string(page), "function adoptAs(", "function adoptAs(thing, run, text) {")
	script += `
const cases = JSON.parse(require('fs').readFileSync(0, 'utf8'));
process.stdout.write(JSON.stringify(cases.map(c => ({lines: parseChecklist(c.output), as: ['task','idea','project'].map(kind => adoptAs({kind}, {kind:c.kind}, c.output).as)}))));`
	cmd := exec.Command(node, "-e", script)
	cmd.Stdin = strings.NewReader(string(asJSON(cases)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("frontend oracle: %v: %s", err, out)
	}
	var results []struct {
		Lines []string `json:"lines"`
		As    []string `json:"as"`
	}
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(cases) {
		t.Fatal("frontend case count differs")
	}
	for i, result := range results {
		if !reflect.DeepEqual(result.Lines, cases[i].Lines) {
			t.Errorf("frontend %s lines=%q want %q", cases[i].Name, result.Lines, cases[i].Lines)
		}
		for _, as := range result.As {
			if as != cases[i].As {
				t.Errorf("frontend %s as=%s want %s", cases[i].Name, as, cases[i].As)
			}
		}
	}
}

func autoAdoptModel(t *testing.T, s *Store, output string, during func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if during != nil {
			during()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": output}}}})
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "auto-model", Name: "Auto model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 200, CostMode: "free"}}}})
}

func autoAdoptItem(st workspace.State, id string) workspace.Item {
	for _, items := range [][]workspace.Item{st.Tasks, st.Ideas, st.Projects} {
		for _, item := range items {
			if item.ID == id {
				return item
			}
		}
	}
	return workspace.Item{}
}

func TestAutoAdoptDestinationsAndUndo(t *testing.T) {
	for _, path := range []string{"worker", "manual"} {
		for _, tc := range []struct{ name, itemKind, runKind, output, as, summary string }{
			{"task checks", "task", "breakdown", "介绍\n- [ ] 第一项\n- [x] 第二项\n- [ ] 3. 第三项\n结语", "subtasks", "副手结果：加了 3 个子任务"},
			{"idea tasks", "idea", "breakdown", "- [ ] 第一项\n- [x] 第二项\n- [ ] 第三项", "subtasks", "副手结果：加了 3 个子任务"},
			{"project tasks", "project", "summary", "- [ ] 第一项\n- [ ] 第二项\n- [ ] 第三项", "subtasks", "副手结果：加了 3 个子任务"},
			{"project progress", "project", "summary", "项目已完成设计", "progress", "副手结果：写进进度"},
			{"task progress", "task", "summary", "任务已完成设计", "progress", "副手结果：写进进度"},
			{"idea progress", "idea", "summary", "想法已完成评估", "progress", "副手结果：写进进度"},
			{"document", "task", "draft", "这是一篇普通文档。", "doc", "副手结果：存成文档"},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				s := testStore(t)
				scope := owner()
				ctx := context.Background()
				autoAdoptModel(t, s, tc.output, nil)
				id := string(memory.NewID())
				st := workspaceCommand(t, s, scope, workspace.Command{Type: map[string]string{"task": "addTask", "idea": "addIdea", "project": "addProject"}[tc.itemKind], ID: id, Title: "事项", Name: "项目", Text: "原有说明"})
				if tc.itemKind == "project" {
					st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateProject", ID: id, Patch: asJSON(map[string]any{"progress": "原有进度"})})
				}
				before := autoAdoptItem(st, id)
				agent := "auto-model"
				if path == "manual" {
					agent = "manual"
				}
				st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: agent, Kind: tc.runKind, Prompt: "处理事项"})
				runID := st.Runs[0].ID
				revision := st.Revision
				if path == "manual" {
					st = workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: runID, Output: tc.output})
				} else {
					if err := s.runAgentOnce(ctx); err != nil {
						t.Fatal(err)
					}
					var err error
					st, err = s.Snapshot(ctx, scope)
					if err != nil {
						t.Fatal(err)
					}
				}
				run := st.Runs[0]
				if run.Status != "done" || run.Adopted == nil || !run.Adopted.Auto || run.Adopted.Edited || run.Adopted.As != tc.as || !memory.ID(run.Adopted.ActionID).Valid() {
					t.Fatalf("run not auto-adopted: %+v", run)
				}
				if st.Revision != revision+1 {
					t.Fatalf("revision=%d want %d", st.Revision, revision+1)
				}
				item := autoAdoptItem(st, id)
				switch tc.as {
				case "subtasks":
					if tc.itemKind == "task" {
						want := parseRunChecklist(tc.output)
						if len(item.Checklist) != 3 {
							t.Fatalf("checks=%+v", item.Checklist)
						}
						for i, check := range item.Checklist {
							if check.Text != want[i] || check.Done {
								t.Fatalf("check=%+v want %s", check, want[i])
							}
						}
					} else {
						if len(st.Tasks) != 3 {
							t.Fatalf("tasks=%+v", st.Tasks)
						}
						for _, task := range st.Tasks {
							if task.Status != "todo" || tc.itemKind == "project" && task.ProjectID != id || tc.itemKind == "idea" && task.IdeaID != id {
								t.Fatalf("task destination=%+v", task)
							}
						}
					}
				case "progress":
					field := map[string]string{"task": "notes", "idea": "body", "project": "progress"}[tc.itemKind]
					if !strings.Contains(fieldText(item, field), tc.output) {
						t.Fatalf("missing progress: %+v", item)
					}
				case "doc":
					if len(st.Docs) != 1 || st.Docs[0].By != "ai" || st.Docs[0].Body != tc.output || st.Docs[0].RunID != runID {
						t.Fatalf("docs=%+v", st.Docs)
					}
				}
				var source, summary string
				var changes []byte
				if err := s.pool.QueryRow(ctx, "SELECT source,summary,changes FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.Adopted.ActionID).Scan(&source, &summary, &changes); err != nil {
					t.Fatal(err)
				}
				if source != "worker" || summary != tc.summary {
					t.Fatalf("action=%s %s", source, summary)
				}
				var entries []actionChange
				if err := json.Unmarshal(changes, &entries); err != nil {
					t.Fatal(err)
				}
				recordedRun := false
				for _, entry := range entries {
					if entry.Table == "agent_runs" && entry.ID == runID {
						var previous workspace.Run
						if err := json.Unmarshal(entry.Before, &previous); err != nil {
							t.Fatal(err)
						}
						if previous.Status != "done" || previous.Adopted != nil || previous.Output != tc.output || previous.FinishedAt == "" {
							t.Fatalf("undo snapshot=%+v", previous)
						}
						recordedRun = true
					}
				}
				if !recordedRun {
					t.Fatal("completed run was not recorded by trigger")
				}
				var samples int
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM training_samples WHERE owner_id=$1 AND document->'origin'->>'runId'=$2", string(scope.OwnerID), runID).Scan(&samples); err != nil || samples != 1 {
					t.Fatalf("samples=%d err=%v", samples, err)
				}
				st = workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: run.Adopted.ActionID})
				restored := autoAdoptItem(st, id)
				if !reflect.DeepEqual(restored.Checklist, before.Checklist) || restored.Notes != before.Notes || restored.Body != before.Body || restored.Progress != before.Progress || len(st.Docs) != 0 || len(st.Tasks) != map[bool]int{true: 1, false: 0}[tc.itemKind == "task"] {
					t.Fatalf("undo did not restore destination: %+v", st)
				}
				if restored.Version <= item.Version || st.Runs[0].Status != "done" || st.Runs[0].Adopted != nil || st.Runs[0].Output != tc.output {
					t.Fatalf("undo did not retain completed result: %+v", st.Runs)
				}
				requestID := string(memory.NewID())
				st, err := s.Execute(ctx, scope, workspace.Command{Type: "adoptRun", ID: runID, As: tc.as, Text: tc.output, RequestID: requestID, ExpectedRevision: st.Revision})
				if err != nil {
					t.Fatal(err)
				}
				if got := st.Runs[0].Adopted; got == nil || got.Auto || got.Edited || got.ActionID != requestID {
					t.Fatalf("manual adoption=%+v", got)
				}
				workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: requestID})
			})
		}
	}
}

func TestAutoAdoptChangedSince(t *testing.T) {
	s := testStore(t)
	scope := owner()
	autoAdoptModel(t, s, "- [ ] 第一步", nil)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "事项"})
	id := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: id, AgentID: "auto-model", Kind: "breakdown", Prompt: "列步骤"})
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	actionID := st.Runs[0].Adopted.ActionID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: id, Text: "后来修改"})
	_, err = s.Execute(context.Background(), scope, workspace.Command{Type: "undoAction", ID: actionID, RequestID: string(memory.NewID()), ExpectedRevision: st.Revision})
	if !errors.Is(err, workspace.ErrChangedSince) {
		t.Fatalf("undo error=%v", err)
	}
}

func TestAutoAdoptSkipsStaleAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		stale        bool
	}{{"stale", "有效的结果", true}, {"empty", "", false}, {"blank", "\n \t", false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "事项"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "manual", Kind: "draft", Prompt: "写文档"})
			run := st.Runs[0]
			run.Status, run.Output, run.StaleContext = "done", tc.output, tc.stale
			err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
				ctx := context.Background()
				if _, err := tx.Exec(ctx, "UPDATE agent_runs SET status='done',document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, asJSON(run)); err != nil {
					return err
				}
				return s.autoAdoptRunTx(ctx, tx, scope, &run)
			})
			if err != nil {
				t.Fatal(err)
			}
			st, err = s.Snapshot(context.Background(), scope)
			if err != nil || st.Runs[0].Status != "done" || st.Runs[0].Adopted != nil || len(st.Docs) != 0 || len(st.Tasks[0].Checklist) != 0 {
				t.Fatalf("skip failed: state=%+v err=%v", st, err)
			}
		})
	}
}

func TestAutoAdoptWorkerSkipsStaleFlag(t *testing.T) {
	s := testStore(t)
	scope := owner()
	autoAdoptModel(t, s, "结果仍然存在", func() {
		if _, err := s.pool.Exec(context.Background(), "UPDATE agent_runs SET document=document||'{\"staleContext\":true}'::jsonb WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			t.Error(err)
		}
	})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "事项"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "auto-model", Kind: "draft", Prompt: "写文档"})
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil || st.Runs[0].Status != "done" || !st.Runs[0].StaleContext || st.Runs[0].Adopted != nil || len(st.Docs) != 0 {
		t.Fatalf("stale flag ignored: %+v %v", st.Runs, err)
	}
}
