package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestP3ExecutionTimingRecordsSecretaryAndDeputy(t *testing.T) {
	for _, failCheck := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed selfcheck"}[failCheck], func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			ctx := context.Background()
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				system, _ := b4RequestBody(t, r)
				if strings.Contains(system, "这是自查") {
					time.Sleep(12 * time.Millisecond)
					if failCheck {
						http.Error(w, "fictitious failure", 503)
						return
					}
					if strings.Contains(system, "前台秘书") {
						secretaryModelReply(w, `{"reply":"修订稿","actions":[]}`)
					} else {
						secretaryModelReply(w, "副手修订稿")
					}
					return
				}
				time.Sleep(45 * time.Millisecond)
				if strings.Contains(system, "前台秘书") {
					secretaryModelReply(w, `{"reply":"已保留的好草稿","memoryPlan":{"depth":"medium","groups":[],"mentioned":[],"adopted":[]},"actions":[]}`)
				} else {
					secretaryModelReply(w, "副手已保留的好草稿")
				}
			})
			m := b4Memory(t, s, scope, "虚构测量背景")
			b4Card(t, s, scope, "self:goal", "self", "虚构目标", m)
			req := turnRequest("仔细拟一段虚构活动简介")
			out := mustTurn(t, s, scope, req)
			if failCheck && out.Turn.Reply != "已保留的好草稿" {
				t.Fatal("failed measurement/check replaced good reply", out.Turn)
			}
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构活动简介"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "起草虚构活动简介"})
			if err := s.runAgentOnce(ctx); err != nil {
				t.Fatal(err)
			}
			st, err := s.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			if st.Runs[0].Status != "done" || failCheck && st.Runs[0].Output != "副手已保留的好草稿" {
				t.Fatal(st.Runs[0])
			}
			raw, err := s.UsageCalls(ctx, scope, 100, "")
			if err != nil {
				t.Fatal(err)
			}
			var page struct {
				Items []struct {
					Purpose         string
					DurationMS      *int64
					TurnID, RunID   *string
					ExecutionTiming *executionTiming
				}
			}
			if err := json.Unmarshal(raw, &page); err != nil {
				t.Fatal(err)
			}
			seen := map[string]int{}
			for _, call := range page.Items {
				if call.TurnID == nil && call.RunID == nil {
					continue
				}
				if call.DurationMS == nil || *call.DurationMS < 10 || call.ExecutionTiming == nil {
					t.Fatalf("missing actual timing: %s", raw)
				}
				row := call.ExecutionTiming
				if row.TotalMS < row.AnswerMS+row.SelfcheckMS || row.AnswerMS < 40 || row.SelfcheckMS < 10 || row.ModelMS < row.AnswerMS+row.SelfcheckMS || row.Tier == "" {
					t.Fatalf("invalid segmentation: %+v", row)
				}
				seen[row.Kind]++
			}
			if seen["secretary"] != 2 || seen["deputy"] != 2 {
				t.Fatalf("missing segments/selfcheck: %s", raw)
			}
			var before, after string
			if err := s.pool.QueryRow(ctx, "SELECT row_to_json(e)::text FROM execution_timings e WHERE owner_id=$1 AND id=$2", scope.OwnerID, out.Turn.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			replay := mustTurn(t, s, scope, req)
			if replay.Turn.ID != out.Turn.ID {
				t.Fatal("replay changed turn")
			}
			if err := s.pool.QueryRow(ctx, "SELECT row_to_json(e)::text FROM execution_timings e WHERE owner_id=$1 AND id=$2", scope.OwnerID, out.Turn.ID).Scan(&after); err != nil || before != after {
				t.Fatal("replay replaced good timing", err)
			}
			today := time.Now().UTC().Format(time.DateOnly)
			summary, err := s.UsageSummary(ctx, scope, today, today)
			if err != nil {
				t.Fatal(err)
			}
			var days struct {
				Days []struct {
					Timings []struct {
						Kind       string
						Executions int
					}
				}
			}
			if err := json.Unmarshal(summary, &days); err != nil {
				t.Fatal(err)
			}
			if len(days.Days) != 1 || len(days.Days[0].Timings) != 2 {
				t.Fatalf("duplicate/missing rounds: %s", summary)
			}
			for _, row := range days.Days[0].Timings {
				if row.Executions != 1 {
					t.Fatalf("model calls double counted as rounds: %s", summary)
				}
			}
		})
	}
}

func TestP3DurationSummaryExactAndHistoricalNull(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	// UTC 16:00 belongs to the next local day; include even-sample median.
	at := time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC)
	for i, ms := range []int64{10, 30, 100} {
		turn := memory.NewID()
		u := modelUsage{OwnerID: scope.OwnerID, ID: memory.NewID(), At: at, Purpose: "secretary", Model: "fictional", TurnID: string(turn), Tier: "medium", DurationMS: &ms}
		if err := s.recordUsage(ctx, u); err != nil {
			t.Fatal(err)
		}
		b4Exec(t, s, `INSERT INTO execution_timings(owner_id,id,kind,tier,at,prepare_ms,answer_ms,selfcheck_ms,writeback_ms,model_ms,total_ms,other_ms) VALUES($1,$2,'secretary','medium',$3,$4::bigint,$4::bigint,0,$4::bigint,$4::bigint,$4::bigint*3,0)`, scope.OwnerID, turn, at, ms)
		if i == 0 { // A second call in the same turn must not duplicate execution stats.
			u.ID = memory.NewID()
			u.DurationMS = nil
			if err := s.recordUsage(ctx, u); err != nil {
				t.Fatal(err)
			}
		}
	}
	historical := modelUsage{OwnerID: scope.OwnerID, ID: memory.NewID(), At: at, Purpose: "deputy", Model: "old", Tier: "light"}
	if err := s.recordUsage(ctx, historical); err != nil {
		t.Fatal(err)
	}
	raw, err := s.UsageSummary(ctx, scope, "2026-01-02", "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	type stat struct {
		Median, Max   *float64
		MeasuredCalls int
	}
	var summary struct {
		Days []struct {
			Date     string
			Purposes []struct {
				Purpose    string
				Calls      int
				DurationMS stat
			}
			Timings []struct {
				Kind, Tier                                                               string
				Executions                                                               int
				PrepareMS, AnswerMS, SelfcheckMS, WritebackMS, ModelMS, TotalMS, OtherMS stat
			}
		}
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Days) != 1 || summary.Days[0].Date != "2026-01-02" {
		t.Fatal(string(raw))
	}
	for _, p := range summary.Days[0].Purposes {
		if p.Purpose == "secretary" {
			if p.Calls != 4 || p.DurationMS.MeasuredCalls != 3 || p.DurationMS.Median == nil || *p.DurationMS.Median != 30 || *p.DurationMS.Max != 100 {
				t.Fatal(string(raw))
			}
		} else if p.DurationMS.Median != nil || p.DurationMS.Max != nil || p.DurationMS.MeasuredCalls != 0 {
			t.Fatal("historical null converted to zero", string(raw))
		}
	}
	rows := summary.Days[0].Timings
	if len(rows) != 1 || rows[0].Executions != 3 || *rows[0].PrepareMS.Median != 30 || *rows[0].AnswerMS.Max != 100 || *rows[0].WritebackMS.Median != 30 || *rows[0].TotalMS.Median != 90 || *rows[0].SelfcheckMS.Max != 0 {
		t.Fatal(string(raw))
	}
	// Two samples have interpolated median 20, not one arbitrarily chosen sample.
	b4Exec(t, s, "DELETE FROM execution_timings WHERE owner_id=$1 AND prepare_ms=100", scope.OwnerID)
	raw, err = s.UsageSummary(ctx, scope, "2026-01-02", "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if *summary.Days[0].Timings[0].PrepareMS.Median != 20 {
		t.Fatal(string(raw))
	}
}

// The own-login walk uses unchanged product instructions/schema, plus a
// recording bridge so the complete model input/output can be pasted in the PR.
func TestP3LiveExecutionTiming(t *testing.T) {
	home := os.Getenv("PCAS_P3_CODEX_HOME")
	if home == "" {
		t.Skip("own login opt-in")
	}
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	codex, err := ai.NewCodex("", home)
	if err != nil {
		t.Fatal(err)
	}
	defer codex.Close()
	real, err := ai.Load("", codex)
	if err != nil {
		t.Fatal(err)
	}
	calls := []map[string]any{}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		system, prompt := b4RequestBody(t, r)
		var result ai.Result
		var err error
		if strings.Contains(system, "这是自查") && strings.Contains(system, "前台秘书") {
			result, err = real.GenerateSchema(r.Context(), "chatgpt", system, prompt, secretaryCheckSchema)
		} else if strings.Contains(system, "这是自查") {
			result, err = real.Generate(r.Context(), "chatgpt", system, prompt)
		} else if system == secretaryInstructions {
			result, err = real.GenerateWithSearchSchema(r.Context(), "chatgpt", system, prompt, secretaryOutputSchema)
		} else {
			result, err = real.GenerateWithSearch(r.Context(), "chatgpt", system, prompt)
		}
		call := map[string]any{"system": system, "input": prompt, "output": result.Text, "durationMs": result.DurationMS}
		if err != nil {
			call["error"] = err.Error()
		}
		calls = append(calls, call)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		secretaryModelReply(w, result.Text)
	})
	req := turnRequest("虚构项目晨露花园准备周末社区活动，先拟一段简短介绍，不创建事项。")
	out, err := s.DeskTurn(ctx, scope, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Turn.Reply == "" {
		t.Fatalf("live call failed: %+v", out.Turn)
	}
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "晨露花园活动介绍"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "拟一段简短的虚构社区活动介绍，不需要联网。"})
	if err := s.runAgentOnce(ctx); err != nil {
		t.Fatal(err)
	}
	st, err = s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := s.UsageCalls(ctx, scope, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format(time.DateOnly)
	summary, err := s.UsageSummary(ctx, scope, today, today)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("PCAS_P3_RAW_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(map[string]any{"input": req.Text, "reply": out.Turn.Reply, "calls": calls, "runs": st.Runs, "usage": json.RawMessage(usage), "summary": json.RawMessage(summary)}, "", "  ")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if st.Runs[0].Status != "done" {
		t.Fatal(st.Runs[0])
	}
}

func TestP3TimingStorageFailureKeepsCommittedAnswer(t *testing.T) {
	s := testStore(t)
	scope := owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"已有的好结果","actions":[{"op":"create_task","title":"虚构已完成派单"}]}`)
	})
	if _, err := s.Snapshot(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	b4Exec(t, s, `CREATE FUNCTION reject_timing() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'fictitious timing write failure'; END$$`)
	b4Exec(t, s, `CREATE TRIGGER reject_timing BEFORE INSERT ON execution_timings FOR EACH ROW EXECUTE FUNCTION reject_timing()`)
	out := mustTurn(t, s, scope, turnRequest("创建虚构已完成派单"))
	if out.Turn.Reply != "已有的好结果" || len(out.State.Tasks) != 1 || out.State.Tasks[0].Title != "虚构已完成派单" {
		t.Fatal("measurement failure invalidated committed result", out)
	}
	var calls int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND duration_ms IS NOT NULL", scope.OwnerID).Scan(&calls); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
}
