package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b4SecondUsageAmendment(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/phase2/b4-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold struct {
		Amendment struct{ Fixtures map[string]string } `json:"coordinator_amendment_2e2b8f9"`
	}
	b4JSON(t, raw, &gold)
	return gold.Amendment.Fixtures
}

func TestPhase2B4_L8_CorrectedMemoryDuringDeputyGenerationKeepsReturnedUsage(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	gold := b4FixtureFor(t, "usage")
	fixtures := b4SecondUsageAmendment(t)
	source := b1Source(t, s, scope, "青玉罗盘资料", gold.PrivateSource, "manual")
	claim := b1Claim(t, s, scope, gold.PrivateMemory, "fact", "adopted", source)
	f.set(fixtures["deputyReturnedText"], 200)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "整理青玉罗盘"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "model", Kind: "breakdown", Prompt: "根据青玉罗盘5591的记忆列出走路步骤"})
	run := st.Runs[0]
	b1HasRef(t, run.ContextVersions, claim, true)
	g := b4HoldModel(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- s.runAgentOnce(ctx) }()
	finished := false
	t.Cleanup(func() {
		cancel()
		g.unblock()
		if !finished {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("owned deputy runner did not stop")
			}
		}
	})
	select {
	case actual := <-g.started:
		b1Contains(t, actual.Prompt, gold.PrivateMemory)
	case <-time.After(10 * time.Second):
		t.Fatal("deputy did not reach actual fake-model request")
	}
	// Correction occurs while the HTTP model response is genuinely withheld.
	b1Correct(t, s, scope, claim, fixtures["deputyCorrection"])
	g.unblock()
	select {
	case <-g.returned:
	case <-time.After(10 * time.Second):
		t.Fatal("fake model did not return the deputy result")
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal("deputy runner", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("deputy runner did not finish")
	}
	if len(f.all()) != 1 {
		t.Fatalf("rejected deputy result must come from exactly one returned call; got %d", len(f.all()))
	}
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	var failed *workspace.Run
	for i := range st.Runs {
		if st.Runs[i].ID == run.ID {
			failed = &st.Runs[i]
		}
	}
	if failed == nil {
		t.Fatal("rejected deputy run disappeared")
	}
	if failed.Status != "failed" || !strings.Contains(failed.Error, "生成期间记忆或授权已变化") || failed.Adopted != nil {
		t.Errorf("changed-context result was not rejected as contracted: %+v", failed)
	}
	if len(st.Docs) != 0 || len(st.Tasks) != 1 || len(st.Tasks[0].Checklist) != 0 {
		t.Errorf("rejected deputy output was adopted into workspace: tasks=%+v docs=%+v", st.Tasks, st.Docs)
	}
	rows := b4Usage(t, s, scope)
	if len(rows) != 1 {
		t.Fatalf("rejected returned deputy call must record one usage row; got %d", len(rows))
	}
	row := rows[0]
	b4UsageNumbers(t, row)
	if row.Purpose != "deputy" || row.AgentID != "model" || row.RunID == nil || *row.RunID != run.ID {
		t.Errorf("rejected deputy usage correlation: %+v", row)
	}
	b1HasRef(t, row.MemoryRefs, claim, true)
	b4NoProse(t, s, scope, gold.PrivateMemory, gold.PrivateSource, fixtures["deputyReturnedText"], fixtures["deputyCorrection"])
}

func TestPhase2B4_L8_UsageIsVisibleBeforeResultTransactionAndSurvivesRollback(t *testing.T) {
	for _, mode := range []string{"rollback", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := b4Store(t), owner()
			f := b4Model(t, s)
			reply := b4SecondUsageAmendment(t)["rollbackReply"]
			f.set(string(asJSON(map[string]any{"reply": reply, "used": []string{}, "actions": []any{}})), 200)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			lockKey := "b4-owned-result-gate-" + string(memory.NewID())
			conn, err := s.pool.Acquire(ctx)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			var pid int
			if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				conn.Release()
				cancel()
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lockKey); err != nil {
				conn.Release()
				cancel()
				t.Fatal(err)
			}
			var releaseOnce sync.Once
			release := func() {
				releaseOnce.Do(func() {
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cleanupCancel()
					if _, err := conn.Exec(cleanupCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lockKey); err != nil {
						t.Error("owned advisory lock cleanup", err)
					}
					conn.Release()
				})
			}
			// Cancel the owned result writer before releasing the gate on failure.
			t.Cleanup(func() { cancel(); release() })
			// This test-owned trigger waits only in the result-writing transaction,
			// then raises a real SQL error. model_usage has no trigger or test stub.
			sql := fmt.Sprintf(`CREATE FUNCTION b4_gate_and_reject_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(hashtextextended('%s',0)); RAISE EXCEPTION 'b4 injected result rollback'; END $$; CREATE TRIGGER b4_gate_and_reject_result AFTER INSERT OR UPDATE ON desk_turns FOR EACH ROW WHEN (NEW.answer='%s') EXECUTE FUNCTION b4_gate_and_reject_result()`, strings.ReplaceAll(lockKey, "'", "''"), strings.ReplaceAll(reply, "'", "''"))
			if _, err := s.pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
			req := turnRequest("合成L8独立事务样例 " + mode)
			done := make(chan error, 1)
			go func() { _, err := s.DeskTurn(ctx, scope, req); done <- err }()
			finished := false
			t.Cleanup(func() {
				cancel()
				release()
				if !finished {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("owned result writer did not stop")
					}
				}
			})
			// Locate a waiter on OUR pinned connection's advisory lock. A waiter
			// proves content returned and the actual result transaction is blocked.
			b4Wait(t, "owned result transaction waiting on SQL gate", func() bool {
				select {
				case err := <-done:
					finished = true
					t.Fatalf("result writer stopped before SQL gate: %v", err)
				default:
				}
				var waiting bool
				err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_locks waiting JOIN pg_locks held USING(locktype,database,classid,objid,objsubid) WHERE held.pid=$1 AND held.locktype='advisory' AND held.granted AND NOT waiting.granted)`, pid).Scan(&waiting)
				if err != nil {
					t.Fatal(err)
				}
				return waiting
			})
			if len(f.all()) != 1 {
				t.Fatalf("result gate expected one actual returned model call; got %d", len(f.all()))
			}
			before := b4Usage(t, s, scope)
			if len(before) != 1 {
				t.Fatalf("usage must be committed and visible on another connection before result commit; got %d", len(before))
			}
			b4UsageNumbers(t, before[0])
			if before[0].Purpose != "secretary" {
				t.Errorf("usage purpose=%q", before[0].Purpose)
			}
			if mode == "cancel" {
				cancel()
			} else {
				release()
			}
			select {
			case err := <-done:
				finished = true
				if err == nil {
					t.Error("test gate did not reject or cancel the result transaction")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("owned result transaction did not roll back")
			}
			release()
			if !reflect.DeepEqual(before, b4Usage(t, s, scope)) {
				t.Error("result failure/cancellation removed or changed independently committed usage")
			}
			var accepted bool
			if err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM desk_turns WHERE owner_id=$1 AND request_id=$2 AND answer=$3)`, string(scope.OwnerID), req.RequestID, reply).Scan(&accepted); err != nil {
				t.Fatal(err)
			}
			if accepted {
				t.Error("rolled-back model answer persisted")
			}
		})
	}
}

// Read the independently frozen invalid content, never a product serializer.
func b4UsageAmendment(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/phase2/b4-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold struct {
		Amendment struct{ Fixtures map[string]string } `json:"coordinator_amendment_b9d26f0"`
	}
	b4JSON(t, raw, &gold)
	return gold.Amendment.Fixtures
}

func TestPhase2B4_L8_ReturnedButInvalidExtractionStillRecordsUsage(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	content := b4UsageAmendment(t)["invalidExtractionContent"]
	if content == "" || json.Valid([]byte(content)) {
		t.Fatal("fixture must return nonempty malformed JSON")
	}
	f.set(content, 200)
	text := "合成L8抽取原话：我喜欢在清晨整理青玉罗盘。"
	source := b1Source(t, s, scope, "L8抽取样例", text, "manual")
	job := leaseStage(t, s, scope, source, "source.extract")
	err := s.ProcessExtraction(context.Background(), job)
	var failure *worker.JobError
	if !errors.As(err, &failure) || failure.Code != "model_output_invalid" {
		t.Errorf("fixture did not reach format rejection: %v", err)
	}
	if len(f.all()) != 1 {
		t.Fatalf("invalid output must come from one returned model call; got %d", len(f.all()))
	}
	b1Contains(t, f.last(t).Prompt, text)
	rows := b4Usage(t, s, scope)
	if len(rows) != 1 {
		t.Fatalf("returned extraction content must record one usage row despite rejection; got %d", len(rows))
	}
	row := rows[0]
	b4UsageNumbers(t, row)
	if row.Purpose != "extraction" || row.JobID == nil || *row.JobID != string(job.ID) {
		t.Errorf("invalid extraction usage correlation: %+v", row)
	}
	b1HasRef(t, row.MemoryRefs, source, true)
	b4NoProse(t, s, scope, text)
}

func TestPhase2B4_L8_ReturnedButInvalidSecretaryStillRecordsUsage(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	content := b4UsageAmendment(t)["invalidSecretaryContent"]
	if content == "" || json.Valid([]byte(content)) {
		t.Fatal("fixture must return nonempty malformed JSON")
	}
	f.set(content, 200)
	req := turnRequest("合成L8秘书格式失败样例")
	if _, err := s.DeskTurn(context.Background(), scope, req); err == nil {
		t.Error("malformed secretary content did not reach format rejection")
	}
	if len(f.all()) != 1 {
		t.Fatalf("invalid output must come from one returned model call; got %d", len(f.all()))
	}
	b1Contains(t, f.last(t).Prompt, req.Text)
	rows := b4Usage(t, s, scope)
	if len(rows) != 1 {
		t.Fatalf("returned secretary content must record one usage row despite rejection; got %d", len(rows))
	}
	row := rows[0]
	b4UsageNumbers(t, row)
	if row.Purpose != "secretary" || row.AgentID != "model" {
		t.Errorf("invalid secretary usage purpose/agent: %+v", row)
	}
}

func TestPhase2B4_L9_IdenticalLegacyRequestsMakeTwoCallsAndTwoUsageRows(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	f.set(`{"answer":"合成回答：今天整理青玉罗盘。","used":[],"links":[]}`, 200)
	question := b4UsageAmendment(t)["legacyQuestion"]
	if question == "" {
		t.Fatal("missing frozen legacy question")
	}
	first, err := s.AnswerDesk(context.Background(), scope, "model", question, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstRows := b4Usage(t, s, scope)
	if len(firstRows) != 1 {
		t.Fatalf("first legacy request rows=%d", len(firstRows))
	}
	second, err := s.AnswerDesk(context.Background(), scope, "model", question, nil)
	if err != nil {
		t.Fatal(err)
	}
	requests := f.all()
	if len(requests) != 2 {
		t.Fatalf("identical legacy requests must invoke model twice; got %d", len(requests))
	}
	for _, request := range requests {
		b1Contains(t, request.Prompt, question)
	}
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Errorf("legacy requests must create independent answers: %q/%q", first.ID, second.ID)
	}
	rows := b4Usage(t, s, scope)
	if len(rows) != 2 {
		t.Fatalf("identical legacy requests must record two returned calls; got %d", len(rows))
	}
	if !reflect.DeepEqual(firstRows[0], rows[0]) {
		t.Error("second legacy request changed the first usage row")
	}
	if rows[0].ID == rows[1].ID {
		t.Error("two returned legacy calls share a usage identity")
	}
	turns := map[string]bool{first.ID: false, second.ID: false}
	for _, row := range rows {
		b4UsageNumbers(t, row)
		if row.Purpose != "answer" || row.AgentID != "model" || row.TurnID == nil {
			t.Errorf("legacy usage correlation: %+v", row)
			continue
		}
		if seen, exists := turns[*row.TurnID]; !exists || seen {
			t.Errorf("duplicate or wrong legacy turnId: %s", *row.TurnID)
		}
		turns[*row.TurnID] = true
	}
	for turn, seen := range turns {
		if !seen {
			t.Errorf("legacy answer %s has no usage row", turn)
		}
	}
}

func TestPhase2B4_L1_SecretaryRecordsExactNumbersAndDependencies(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	gold := b4FixtureFor(t, "usage")
	source := b1Source(t, s, scope, "青玉罗盘与蓝色雨伞", gold.PrivateSource, "manual")
	claim := b1Claim(t, s, scope, gold.PrivateMemory, "fact", "adopted", source)
	f.set(`{"reply":"查到了。","used":["M1","S1"],"actions":[]}`, 200)
	req := turnRequest("青玉罗盘蓝色雨伞地下室怎么走")
	out := mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, gold.PrivateMemory, gold.PrivateSource)
	rows := b4Usage(t, s, scope)
	if len(rows) != 1 {
		t.Fatalf("successful secretary call recorded %d rows", len(rows))
	}
	row := rows[0]
	b4UsageNumbers(t, row)
	if row.Purpose != "secretary" || row.AgentID != "model" || row.TurnID == nil || *row.TurnID != out.Turn.ID || row.RunID != nil || row.JobID != nil {
		t.Errorf("secretary correlation: %+v", row)
	}
	refs := b1Refs(t, s, scope, req.RequestID)
	b1HasRef(t, refs, source, true)
	b1HasRef(t, refs, claim, true)
	b4SameRefs(t, row.MemoryRefs, refs)
}

func TestPhase2B4_L2_DeputyLegacyAnswerExtractionEachRecordTheirCall(t *testing.T) {
	t.Run("deputy", func(t *testing.T) {
		s, scope := b4Store(t), owner()
		f := b4Model(t, s)
		f.set(`{"summary":"完成。","output":"完成。","used":[],"actions":[]}`, 200)
		run := b1Run(t, s, scope, "model", "整理合成验收资料")
		rows := b4Usage(t, s, scope)
		if len(rows) != 1 {
			t.Fatalf("deputy usage rows=%d", len(rows))
		}
		row := rows[0]
		b4UsageNumbers(t, row)
		if row.Purpose != "deputy" || row.RunID == nil || *row.RunID != run.ID || row.AgentID != "model" {
			t.Errorf("deputy correlation: %+v", row)
		}
	})
	t.Run("legacy answer", func(t *testing.T) {
		s, scope := b4Store(t), owner()
		f := b4Model(t, s)
		f.set(`{"answer":"没有安排。","used":[],"links":[]}`, 200)
		out, err := s.AnswerDesk(context.Background(), scope, "model", "今天有哪些安排", nil)
		if err != nil {
			t.Fatal(err)
		}
		rows := b4Usage(t, s, scope)
		if len(rows) != 1 {
			t.Fatalf("legacy answer usage rows=%d", len(rows))
		}
		row := rows[0]
		b4UsageNumbers(t, row)
		if row.Purpose != "answer" || row.AgentID != "model" {
			t.Errorf("legacy answer purpose/agent: %+v", row)
		}
		if row.TurnID == nil || *row.TurnID != out.ID {
			t.Errorf("legacy answer turn correlation: %+v, turn=%s", row, out.ID)
		}
	})
	t.Run("extraction", func(t *testing.T) {
		s, scope := b4Store(t), owner()
		f := b4Model(t, s)
		f.set(`{"items":[]}`, 200)
		source := b1Source(t, s, scope, "合成笔记", "我喜欢在清晨整理蓝塔笔记。", "manual")
		job := leaseStage(t, s, scope, source, "source.extract")
		if err := s.ProcessExtraction(context.Background(), job); err != nil {
			t.Fatal(err)
		}
		rows := b4Usage(t, s, scope)
		if len(rows) != 1 {
			t.Fatalf("extraction usage rows=%d", len(rows))
		}
		row := rows[0]
		b4UsageNumbers(t, row)
		if row.Purpose != "extraction" || row.JobID == nil || *row.JobID != string(job.ID) {
			t.Errorf("extraction correlation: %+v", row)
		}
		b1HasRef(t, row.MemoryRefs, source, true)
	})
}

func TestPhase2B4_L3_FailureAndSuccessfulRequestReplayDoNotAddRows(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	f.set(`{"reply":"unused","actions":[]}`, 503)
	failed := b4HTTP(t, s, scope, "POST", "/v1/desk/turn", turnRequest("合成失败请求"))
	if failed.Code < 400 {
		t.Fatalf("model failure returned success: %d", failed.Code)
	}
	if rows := b4Usage(t, s, scope); len(rows) != 0 {
		t.Fatalf("failed model call recorded usage: %+v", rows)
	}
	f.set(`{"reply":"成功。","used":[],"actions":[]}`, 200)
	req := turnRequest("合成成功请求")
	first := mustTurn(t, s, scope, req)
	before := b4Usage(t, s, scope)
	requests := len(f.all())
	if len(before) != 1 {
		t.Fatalf("successful call rows=%d", len(before))
	}
	replay := mustTurn(t, s, scope, req)
	if replay.Turn.ID != first.Turn.ID || len(f.all()) != requests {
		t.Error("request replay performed another model call or created a different turn")
	}
	if !reflect.DeepEqual(before, b4Usage(t, s, scope)) {
		t.Error("request replay changed usage rows")
	}
}

func TestPhase2B4_L6_UsageEndpointsEnforceOwnerAndIsolation(t *testing.T) {
	s, scope := b4Store(t), owner()
	b4Model(t, s)
	mustTurn(t, s, scope, turnRequest("花费访问边界合成对话"))
	rows := b4Usage(t, s, scope)
	if len(rows) != 1 {
		t.Fatal("missing owner usage fixture")
	}
	other := owner()
	for _, path := range []string{"/v1/workspace/usage", "/v1/workspace/usage/calls?limit=20"} {
		for _, tc := range []struct {
			name          string
			scope         memory.Scope
			authenticated bool
		}{
			{"unauthenticated", scope, false},
			{"scoped nonowner", memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "external", IsOwner: false}, true},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				b4API(s, tc.scope, tc.authenticated).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != 401 && w.Code != 403 {
					t.Errorf("nonowner read status=%d; body=%s", w.Code, w.Body.String())
				}
				b1Absent(t, w.Body.String(), rows[0].ID)
			})
		}
		w := b4HTTP(t, s, other, "GET", path, nil)
		b4OK(t, w)
		b1Absent(t, w.Body.String(), rows[0].ID)
		if own := b4Usage(t, s, other); len(own) != 0 {
			t.Error("other owner's fixture is not empty")
		}
	}
}

func TestPhase2B4_L7_UndoClearAndDeleteKeepExactUsageRows(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	gold := b4FixtureFor(t, "usage")
	source := b1Source(t, s, scope, "青玉罗盘笔记", gold.PrivateSource, "manual")
	claim := b1Claim(t, s, scope, gold.PrivateMemory, "fact", "adopted", source)
	f.set(`{"reply":"建好了。","used":["M1","S1"],"actions":[{"op":"create_task","title":"整理青玉罗盘笔记"}]}`, 200)
	req := turnRequest("整理青玉罗盘蓝色雨伞笔记")
	out := mustTurn(t, s, scope, req)
	before := b4Usage(t, s, scope)
	if len(before) != 1 {
		t.Fatalf("usage fixture rows=%d", len(before))
	}
	undo := b4HTTP(t, s, scope, "POST", "/v1/workspace/commands", workspace.Command{Type: "undoAction", ID: b1ReceiptAction(t, out.Turn, 0), RequestID: string(memory.NewID()), ExpectedRevision: out.State.Revision})
	b4OK(t, undo)
	if !reflect.DeepEqual(before, b4Usage(t, s, scope)) {
		t.Error("undo removed or altered usage")
	}
	turnSource := b1TurnSource(t, s, scope, req.RequestID)
	clear := b4HTTP(t, s, scope, "POST", "/v1/memory/delete", memory.DeleteRequest{Targets: []memory.Ref{turnSource}})
	b4OK(t, clear)
	history := b1History(t, s, scope, out.ConversationID)
	if history.Text != "" || history.Reply != "（内容已删除）" {
		t.Errorf("fixture did not clear the turn: %+v", history)
	}
	if !reflect.DeepEqual(before, b4Usage(t, s, scope)) {
		t.Error("clearing turn removed or altered usage")
	}
	deleted := b4HTTP(t, s, scope, "POST", "/v1/memory/delete", memory.DeleteRequest{Targets: []memory.Ref{claim}})
	b4OK(t, deleted)
	if !reflect.DeepEqual(before, b4Usage(t, s, scope)) {
		t.Error("deleting memory removed or altered usage")
	}
	b4NoProse(t, s, scope, gold.PrivateMemory, gold.PrivateSource)
}

type b4Call struct {
	ID, At, Purpose, AgentID, Model string
	InputTokens, OutputTokens       int
	Cost                            float64
	TurnID, RunID, JobID            *string
	Refs                            []struct {
		ID      string
		Version int
		Kind    string
		Text    string
		Deleted bool
	}
}
type b4CallPage struct {
	Items []b4Call
	Next  string
}

func b4Calls(t *testing.T, s *Store, scope memory.Scope, path string) b4CallPage {
	t.Helper()
	w := b4HTTP(t, s, scope, "GET", path, nil)
	b4OK(t, w)
	var page b4CallPage
	b4JSON(t, w.Body.Bytes(), &page)
	if page.Items == nil {
		t.Fatal("usage calls must return items array")
	}
	return page
}
func TestPhase2B4_L4_LocalDaysAndSydneyDSTHaveExactSummaries(t *testing.T) {
	var cases []b4DayCase
	b4JSON(t, b4Gold(t).Fixtures["usageDays"], &cases)
	for n, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", n, tc.Zone), func(t *testing.T) {
			s, scope := b4Store(t), owner()
			b4Model(t, s)
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": tc.Zone})})
			want := map[string]int{}
			for i, instant := range tc.Instants {
				out := mustTurn(t, s, scope, turnRequest(fmt.Sprintf("时区验收第%d次调用", i)))
				// Fixture clock: rewrite only the usage timestamp after a REAL call.
				// Summary grouping remains the real HTTP path and real table.
				if _, err := s.pool.Exec(context.Background(), `UPDATE model_usage SET at=$1 WHERE owner_id=$2 AND turn_id=$3`, b4Instant(t, instant), string(scope.OwnerID), out.Turn.ID); err != nil {
					t.Fatal(err)
				}
				want[tc.Dates[i]]++
			}
			w := b4HTTP(t, s, scope, "GET", "/v1/workspace/usage?from="+tc.Dates[0]+"&to="+tc.Dates[len(tc.Dates)-1], nil)
			b4OK(t, w)
			var summary struct {
				Days []struct {
					Date     string
					Purposes []struct {
						Purpose                          string
						Calls, InputTokens, OutputTokens int
						Cost                             float64
					}
				}
			}
			b4JSON(t, w.Body.Bytes(), &summary)
			if len(summary.Days) != len(want) {
				t.Fatalf("days=%s; want=%v", w.Body.String(), want)
			}
			gold := b4FixtureFor(t, "usage")
			previous := ""
			for _, day := range summary.Days {
				if day.Date <= previous {
					t.Error("summary days not strictly ascending")
				}
				previous = day.Date
				count, exists := want[day.Date]
				if !exists || len(day.Purposes) != 1 {
					t.Fatalf("unexpected day/purposes: %s", w.Body.String())
				}
				p := day.Purposes[0]
				if p.Purpose != "secretary" || p.Calls != count || p.InputTokens != count*gold.InputTokens || p.OutputTokens != count*gold.OutputTokens || math.Abs(p.Cost-float64(count)*gold.Cost) > 1e-10 {
					t.Errorf("local-day totals: %+v; calls=%d", p, count)
				}
			}
		})
	}
}
func TestPhase2B4_L5_CurrentUnicodeExcerptDeletedRefAndEveryColumnPrivacy(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	gold := b4FixtureFor(t, "usage")
	text := gold.PrivateMemory + strings.Repeat("🚲合成中文", 30)
	source := b1Source(t, s, scope, "青玉罗盘与蓝色雨伞", gold.PrivateSource, "manual")
	claim := b1Claim(t, s, scope, text, "fact", "adopted", source)
	f.set(`{"reply":"查到了。","used":["M1","S1"],"actions":[]}`, 200)
	req := turnRequest("青玉罗盘蓝色雨伞地下室怎么走")
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, text, gold.PrivateSource)
	b4NoProse(t, s, scope, gold.PrivateMemory, gold.PrivateSource, text)
	page := b4Calls(t, s, scope, "/v1/workspace/usage/calls?limit=1")
	if len(page.Items) != 1 {
		t.Fatalf("calls page=%+v", page)
	}
	matched := false
	for _, ref := range page.Items[0].Refs {
		if ref.ID == string(claim.ID) {
			matched = true
			if ref.Deleted || ref.Text != string([]rune(text)[:80]) {
				t.Errorf("Unicode excerpt=%+v", ref)
			}
		}
	}
	if !matched {
		t.Fatal("calls missing fixture memory reference")
	}
	updated := gold.PrivateMemory + "现在改为向西走。" + strings.Repeat("🌏当前文字", 30)
	b1Correct(t, s, scope, claim, updated)
	page = b4Calls(t, s, scope, "/v1/workspace/usage/calls?limit=1")
	matched = false
	for _, ref := range page.Items[0].Refs {
		if ref.ID == string(claim.ID) {
			matched = true
			if ref.Text != string([]rune(updated)[:80]) {
				t.Errorf("calls did not resolve CURRENT text: %+v", ref)
			}
		}
	}
	if !matched {
		t.Fatal("edited memory reference disappeared from calls")
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	current := b1MemoryRef(t, state.Memories, updated)
	w := b4HTTP(t, s, scope, "POST", "/v1/memory/delete", memory.DeleteRequest{Targets: []memory.Ref{current}})
	b4OK(t, w)
	page = b4Calls(t, s, scope, "/v1/workspace/usage/calls?limit=1")
	matched = false
	for _, ref := range page.Items[0].Refs {
		if ref.ID == string(claim.ID) {
			matched = true
			if !ref.Deleted || ref.Text != "" {
				t.Errorf("deleted reference contains text or no tombstone: %+v", ref)
			}
		}
	}
	if !matched {
		t.Error("deleted reference disappeared instead of retaining tombstone")
	}
	b4NoProse(t, s, scope, gold.PrivateMemory, gold.PrivateSource, text, updated)
}

func TestPhase2B4_L4_SummarySeparatesPurposesAndOmitsEmptyDays(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	f.set(`{"reply":"秘书回答。","used":[],"actions":[]}`, 200)
	mustTurn(t, s, scope, turnRequest("自然日用途汇总"))
	f.set(`{"answer":"导办台回答。","used":[],"links":[]}`, 200)
	if _, err := s.AnswerDesk(context.Background(), scope, "model", "自然日用途汇总", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), `UPDATE model_usage SET at='2025-01-02T04:00:00Z' WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	w := b4HTTP(t, s, scope, "GET", "/v1/workspace/usage?from=2025-01-01&to=2025-01-03", nil)
	b4OK(t, w)
	var response struct {
		Days []struct {
			Date     string
			Purposes []struct {
				Purpose                          string
				Calls, InputTokens, OutputTokens int
				Cost                             float64
			}
		}
	}
	b4JSON(t, w.Body.Bytes(), &response)
	if len(response.Days) != 1 || response.Days[0].Date != "2025-01-02" || len(response.Days[0].Purposes) != 2 {
		t.Fatalf("empty days or purposes: %s", w.Body.String())
	}
	gold := b4FixtureFor(t, "usage")
	seen := map[string]bool{}
	for _, purpose := range response.Days[0].Purposes {
		if seen[purpose.Purpose] || purpose.Purpose != "secretary" && purpose.Purpose != "answer" {
			t.Errorf("unexpected purpose %+v", purpose)
		}
		seen[purpose.Purpose] = true
		if purpose.Calls != 1 || purpose.InputTokens != gold.InputTokens || purpose.OutputTokens != gold.OutputTokens || math.Abs(purpose.Cost-gold.Cost) > 1e-10 {
			t.Errorf("purpose totals %+v", purpose)
		}
	}
}
func TestPhase2B4_L1_CallsPaginationHasNoDuplicatesAndPreservesNumbers(t *testing.T) {
	s, scope := b4Store(t), owner()
	b4Model(t, s)
	for n := range 3 {
		mustTurn(t, s, scope, turnRequest(fmt.Sprintf("分页合成对话%d", n)))
	}
	gold := b4FixtureFor(t, "usage")
	seen := map[string]bool{}
	cursor := ""
	previous := ""
	for n := range 3 {
		path := "/v1/workspace/usage/calls?limit=1"
		if cursor != "" {
			path += "&before=" + url.QueryEscape(cursor)
		}
		page := b4Calls(t, s, scope, path)
		if len(page.Items) != 1 {
			t.Fatalf("page %d=%+v", n, page)
		}
		row := page.Items[0]
		if seen[row.ID] {
			t.Error("duplicate paginated call")
		}
		seen[row.ID] = true
		if previous != "" && row.At > previous {
			t.Error("calls are not newest first")
		}
		previous = row.At
		if row.Purpose != "secretary" || row.Model != gold.Model || row.InputTokens != gold.InputTokens || row.OutputTokens != gold.OutputTokens || math.Abs(row.Cost-gold.Cost) > 1e-10 {
			t.Errorf("wire call numbers %+v", row)
		}
		if n < 2 && page.Next == "" {
			t.Fatal("pagination next missing")
		}
		cursor = page.Next
	}
}
