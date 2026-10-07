package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
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
	// Execute the actual frontend function bodies and their source dependency. Only erase TypeScript syntax;
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
	const declarationMarker = "\nconst summaryInto = "
	pageSource := string(page)
	if strings.Count(pageSource, declarationMarker) != 1 {
		t.Fatal("frontend summaryInto declaration missing or ambiguous")
	}
	declaration := pageSource[strings.Index(pageSource, declarationMarker)+1:]
	end := strings.Index(declaration, "\n")
	if end < 0 || !strings.HasSuffix(declaration[:end], " as const") {
		t.Fatal("cannot extract frontend summaryInto declaration")
	}
	script += "\n" + strings.TrimSuffix(declaration[:end], " as const")
	script += "\n" + extract(string(page), "function adoptAs(", "function adoptAs(thing, run) {")
	script += `
const cases = JSON.parse(require('fs').readFileSync(0, 'utf8'));
process.stdout.write(JSON.stringify(cases.map(c => ({lines: parseChecklist(c.output), as: ['task','idea','project'].map(kind => adoptAs({kind}, {kind:c.kind, output:c.output}).as)}))));`
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
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": output}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20}})
	}))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "auto-model", Name: "Auto model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 200, InputPerMillion: 1, OutputPerMillion: 2}}}})
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
			{"project progress", "project", "summary", "项目已完成设计", "progress", "副手结果：记住项目进展"},
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
				wantRevision := revision
				if path == "manual" {
					wantRevision++
				}
				if st.Revision != wantRevision {
					t.Fatalf("revision=%d want %d", st.Revision, wantRevision)
				}
				item := autoAdoptItem(st, id)
				if item.History[len(item.History)-1].By != "assistant" {
					t.Fatalf("auto adoption actor=%+v", item.History)
				}
				if tc.itemKind == "idea" && item.Evolution[len(item.Evolution)-1].By != "assistant" {
					t.Fatalf("idea adoption actor=%+v", item.Evolution)
				}
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
							for _, revision := range task.History {
								if revision.By != "assistant" {
									t.Fatalf("created task actor=%+v", task.History)
								}
							}
							if task.Status != "todo" || tc.itemKind == "project" && task.ProjectID != id || tc.itemKind == "idea" && task.IdeaID != id {
								t.Fatalf("task destination=%+v", task)
							}
						}
					}
				case "progress":
					if tc.itemKind == "project" {
						// Phase 3 H7: a project's progress is remembered as a project
						// memory instead of being written into a field.
						if !autoAdoptHasMemory(st, tc.output) {
							t.Fatalf("missing project memory: %+v", st.Memories)
						}
					} else {
						field := map[string]string{"task": "notes", "idea": "body"}[tc.itemKind]
						if !strings.Contains(fieldText(item, field), tc.output) {
							t.Fatalf("missing progress: %+v", item)
						}
					}
				case "doc":
					if len(st.Docs) != 1 || st.Docs[0].By != "ai" || st.Docs[0].Body != tc.output || st.Docs[0].RunID != runID {
						t.Fatalf("docs=%+v", st.Docs)
					}
				}
				if path == "worker" {
					if err := s.runAgentOnce(ctx); err != nil {
						t.Fatal(err)
					}
					again, err := s.Snapshot(ctx, scope)
					if err != nil || again.Revision != st.Revision || again.Runs[0].Adopted.ActionID != run.Adopted.ActionID || run.Cost <= 0 {
						t.Fatalf("idle worker changed adoption/billing: %+v %v", again.Runs, err)
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
				var sampleID string
				for _, entry := range entries {
					if entry.Table == "training_samples" {
						if sampleID != "" || string(entry.Before) != "null" || entry.AfterHash == nil || !memory.ID(entry.ID).Valid() {
							t.Fatalf("invalid adoption sample snapshot: %+v", entry)
						}
						sampleID = entry.ID
					}
					if entry.Table == "agent_runs" && entry.ID == runID {
						var previous workspace.Run
						if err := json.Unmarshal(entry.Before, &previous); err != nil {
							t.Fatal(err)
						}
						if previous.Status != "done" || previous.Adopted != nil || previous.Output != tc.output || previous.FinishedAt == "" || previous.Cost != run.Cost {
							t.Fatalf("undo snapshot=%+v", previous)
						}
						recordedRun = true
					}
				}
				if sampleID == "" {
					t.Fatal("adoption sample was not recorded by trigger")
				}
				if !recordedRun {
					t.Fatal("completed run was not recorded by trigger")
				}
				var samples int
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM training_samples WHERE owner_id=$1 AND document->'origin'->>'runId'=$2", string(scope.OwnerID), runID).Scan(&samples); err != nil || samples != 1 {
					t.Fatalf("samples=%d err=%v", samples, err)
				}
				st = workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: run.Adopted.ActionID})
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM training_samples WHERE owner_id=$1 AND run_id=$2", string(scope.OwnerID), runID).Scan(&samples); err != nil || samples != 0 {
					t.Fatalf("auto adoption sample survived undo: samples=%d err=%v", samples, err)
				}
				restored := autoAdoptItem(st, id)
				if restored.History[len(restored.History)-1].By != "user" {
					t.Fatalf("undo actor=%+v", restored.History)
				}
				if !reflect.DeepEqual(restored.Checklist, before.Checklist) || restored.Notes != before.Notes || restored.Body != before.Body || restored.Progress != before.Progress || len(st.Docs) != 0 || len(st.Tasks) != map[bool]int{true: 1, false: 0}[tc.itemKind == "task"] {
					t.Fatalf("undo did not restore destination: %+v", st)
				}
				if tc.itemKind == "project" && tc.as == "progress" && autoAdoptHasMemory(st, tc.output) {
					t.Fatalf("undo left the adopted project memory in view: %+v", st.Memories)
				}
				if restored.Version <= item.Version || st.Runs[0].Status != "done" || st.Runs[0].Adopted != nil || st.Runs[0].Output != tc.output || st.Runs[0].Cost != run.Cost {
					t.Fatalf("undo did not retain completed result: %+v", st.Runs)
				}
				var reserved float64
				var status string
				if err := s.pool.QueryRow(ctx, "SELECT reserved_cost,status FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), runID).Scan(&reserved, &status); err != nil || reserved != run.Cost || status != "done" {
					t.Fatalf("undo changed billing: cost=%f status=%s err=%v", reserved, status, err)
				}
				requestID := string(memory.NewID())
				st, err := s.Execute(ctx, scope, workspace.Command{Type: "adoptRun", ID: runID, As: tc.as, Text: tc.output, RequestID: requestID, ExpectedRevision: st.Revision})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM training_samples WHERE owner_id=$1 AND run_id=$2", string(scope.OwnerID), runID).Scan(&samples); err != nil || samples != 1 {
					t.Fatalf("readoption duplicated samples: samples=%d err=%v", samples, err)
				}
				var readoptedSampleID string
				if err := s.pool.QueryRow(ctx, "SELECT id::text FROM training_samples WHERE owner_id=$1 AND run_id=$2 AND document->>'kind'='adopted-result'", string(scope.OwnerID), runID).Scan(&readoptedSampleID); err != nil || readoptedSampleID == sampleID {
					t.Fatalf("readoption reused undone sample: id=%s err=%v", readoptedSampleID, err)
				}
				readopted := autoAdoptItem(st, id)
				if readopted.History[len(readopted.History)-1].By != "user" {
					t.Fatalf("manual adoption actor=%+v", readopted.History)
				}
				if got := st.Runs[0].Adopted; got == nil || got.Auto || got.Edited || got.ActionID != requestID {
					t.Fatalf("manual adoption=%+v", got)
				}
				workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: requestID})
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM training_samples WHERE owner_id=$1 AND run_id=$2", string(scope.OwnerID), runID).Scan(&samples); err != nil || samples != 0 {
					t.Fatalf("manual adoption sample survived undo: samples=%d err=%v", samples, err)
				}
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
	if !errors.Is(err, workspace.ErrNewerAction) {
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

// Preserve legacy tests of manual adoption by undoing the new automatic step.
func undoAutoAdoption(t *testing.T, s *Store, scope memory.Scope, runID string) {
	t.Helper()
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range st.Runs {
		if run.ID == runID {
			if run.Adopted == nil || !run.Adopted.Auto {
				t.Fatal("expected automatic adoption before legacy manual flow")
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: run.Adopted.ActionID})
			return
		}
	}
	t.Fatal("run missing before legacy manual flow")
}

func TestAutoAdoptFailurePreservesCompletedResult(t *testing.T) {
	for _, path := range []string{"worker", "manual"} {
		for _, failure := range []string{"invalid-checklist", "action-log-sql"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				s := testStore(t)
				scope := owner()
				ctx := context.Background()
				const privateText = "private-result-729413"
				output := "- [ ] first step\n- [ ] " + privateText + strings.Repeat("x", 2001)
				kind := "breakdown"
				if failure == "action-log-sql" {
					output, kind = privateText+" draft", "draft"
					// Fail after doc, item, sample and adopted run writes. A real SQL error
					// aborts the savepoint and must not poison the completed outer tx.
					_, err := s.pool.Exec(ctx, `
CREATE FUNCTION reject_worker_adoption() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'private-result-729413'; END $$;
CREATE TRIGGER reject_worker_adoption BEFORE INSERT ON action_log
FOR EACH ROW WHEN (NEW.source='worker') EXECUTE FUNCTION reject_worker_adoption();`)
					if err != nil {
						t.Fatal(err)
					}
				}
				var calls atomic.Int32
				autoAdoptModel(t, s, output, func() { calls.Add(1) })
				st := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "Project before adoption"})
				project := st.Projects[0]
				agent := "auto-model"
				if path == "manual" {
					agent = "manual"
				}
				st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: project.ID, AgentID: agent, Kind: kind, Prompt: "Generate result"})
				runID, revision := st.Runs[0].ID, st.Revision
				var warnings bytes.Buffer
				previousLogger := slog.Default()
				slog.SetDefault(slog.New(slog.NewJSONHandler(&warnings, nil)))
				defer slog.SetDefault(previousLogger)
				if path == "worker" {
					if err := s.runAgentOnce(ctx); err != nil {
						t.Fatalf("adoption failure rolled back completion: %v", err)
					}
					var err error
					st, err = s.Snapshot(ctx, scope)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					st = workspaceCommand(t, s, scope, workspace.Command{Type: "pasteRunResult", ID: runID, Output: output})
				}
				wantRevision := revision
				if path == "manual" {
					wantRevision++
				}
				run := st.Runs[0]
				if run.Status != "done" || run.Adopted != nil || run.Output != output || run.FinishedAt == "" || st.Revision != wantRevision {
					t.Fatalf("completion lost after adoption failure: run=%+v revision=%d", run, st.Revision)
				}
				if !reflect.DeepEqual(st.Projects[0], project) || len(st.Tasks) != 0 || len(st.Docs) != 0 || len(st.Samples) != 0 {
					t.Fatalf("partial adoption survived rollback: %+v", st)
				}
				for _, table := range []string{"adopted_artifacts", "artifact_fields", "training_samples"} {
					var count int
					if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count); err != nil || count != 0 {
						t.Fatalf("partial %s survived rollback: count=%d err=%v", table, count, err)
					}
				}
				var workerActions int
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM action_log WHERE owner_id=$1 AND source='worker'", string(scope.OwnerID)).Scan(&workerActions); err != nil || workerActions != 0 {
					t.Fatalf("failed adoption was recorded: count=%d err=%v", workerActions, err)
				}
				log := warnings.String()
				adoptionWarnings := 0
				for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
					var record struct {
						Level string `json:"level"`
						Msg   string `json:"msg"`
						RunID string `json:"run_id"`
					}
					if err := json.Unmarshal([]byte(line), &record); err != nil {
						t.Fatalf("invalid warning record: %v", err)
					}
					if record.Level == "WARN" && record.Msg == "assistant result auto-adoption failed" {
						adoptionWarnings++
						if record.RunID != runID {
							t.Fatalf("adoption warning references wrong run: %s", record.RunID)
						}
					}
				}
				if strings.Contains(log, privateText) || strings.Contains(log, "Generate result") || adoptionWarnings != 1 {
					t.Fatalf("warning missing, duplicated or contains private text: %s", log)
				}
				var status string
				var rowCount int
				var cost float64
				var leased bool
				if err := s.pool.QueryRow(ctx, "SELECT status,reserved_cost,lease_token IS NOT NULL OR lease_until IS NOT NULL FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), runID).Scan(&status, &cost, &leased); err != nil || status != "done" || cost != run.Cost || leased {
					t.Fatalf("completion/billing not committed: status=%s cost=%f leased=%t err=%v", status, cost, leased, err)
				}
				expectedCalls := calls.Load()
				if path == "worker" {
					// The default medium turn may also reach selfcheck within its
					// budget. Neither call may repeat once the run is done.
					if expectedCalls < 1 || expectedCalls > 2 {
						t.Fatalf("unexpected model calls: %d", expectedCalls)
					}
					if cost <= 0 {
						t.Fatal("fixture did not bill the model")
					}
				} else if cost != 0 || expectedCalls != 0 {
					t.Fatalf("manual handoff incurred model cost: %f calls=%d", cost, expectedCalls)
				}
				// Processing again must not regenerate or attempt adoption of a done run.
				if err := s.runAgentOnce(ctx); err != nil {
					t.Fatal(err)
				}
				var spent float64
				if err := s.pool.QueryRow(ctx, "SELECT count(*),coalesce(sum(reserved_cost),0) FROM agent_runs WHERE owner_id=$1", string(scope.OwnerID)).Scan(&rowCount, &spent); err != nil || rowCount != 1 || spent != cost || calls.Load() != expectedCalls || warnings.String() != log {
					t.Fatalf("result regenerated or rebilled: rows=%d spent=%f calls=%d err=%v", rowCount, spent, calls.Load(), err)
				}
				again, err := s.Snapshot(ctx, scope)
				if err != nil || !reflect.DeepEqual(again.Runs[0], run) || again.Revision != st.Revision {
					t.Fatalf("idle worker changed completed result: %+v %v", again.Runs, err)
				}
				// The original output can still be adopted manually as a document. Only
				// worker action logs are rejected by the injected SQL failure.
				st = workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: runID, As: "doc", Text: output})
				if got := st.Runs[0].Adopted; got == nil || got.Auto || got.Edited || got.ActionID == "" || len(st.Docs) != 1 || st.Docs[0].Body != output || len(st.Samples) != 1 || st.Runs[0].Cost != cost {
					t.Fatalf("manual adoption unavailable after failure: %+v", st)
				}
			})
		}
	}
}

func autoAdoptHasMemory(st workspace.State, text string) bool {
	for _, m := range st.Memories {
		if m.Text == text {
			return true
		}
	}
	return false
}
