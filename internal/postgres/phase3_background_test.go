package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Independent content, translated at the model boundary. This does not certify
// real prompts/schema compliance; that remains the implementation PR's real-model
// check. The record contains the exact prompt actually sent by the real processor.
type phase3Model struct {
	mu       sync.Mutex
	Calls    map[string][]phase26Call
	Before   func(context.Context, string)
	Override func(string, string) string
}

func phase3NewModel(t *testing.T, f *phase3LoadedFixture) *phase3Model {
	t.Helper()
	m := &phase3Model{Calls: map[string][]phase26Call{}}
	baseline := &phase26Model{f: f.phase26LoadedFixture}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages       []struct{ Role, Content string }
			System, Prompt string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad test request", 400)
			return
		}
		sys, prompt := req.System, req.Prompt
		for _, msg := range req.Messages {
			if msg.Role == "system" {
				sys = msg.Content
			}
			if msg.Role == "user" {
				prompt = msg.Content
			}
		}
		logical := phase26ModelStage(sys)
		switch {
		case strings.Contains(sys, "前台秘书"):
			logical = "secretary"
		case strings.Contains(sys, "个人工作副手"):
			logical = "deputy"
		case strings.Contains(sys, "工作量"):
			logical = "effort"
		case strings.Contains(sys, "项目") && strings.Contains(sys, "交接"):
			logical = "project_handover"
		}
		m.mu.Lock()
		before, override := m.Before, m.Override
		m.mu.Unlock()
		if before != nil {
			before(r.Context(), logical)
		}
		out := baseline.goldReply(phase26Call{Stage: logical, System: sys, Prompt: prompt})
		p := f.Gold.Projects[0]
		for _, candidate := range f.Gold.Projects {
			if phase3PromptHas(prompt, candidate.ID) || phase3PromptHas(prompt, candidate.Name) || phase3PromptHas(prompt, candidate.Documents[0].ID) {
				p = candidate
				break
			}
		}
		if logical == "project_handover" {
			doc := p.Documents[0]
			v := doc.Versions[len(doc.Versions)-1]
			evidence := func(kind, id string, version int) []map[string]any {
				return []map[string]any{{"kind": kind, "id": id, "version": version}}
			}
			out = string(asJSON(map[string]any{"conclusion": []any{map[string]any{"text": fmt.Sprintf("虚构项目当前方案为第%d版，预算%d虚构单位。", v.Number, v.BudgetUnits), "evidence": evidence("documentVersion", doc.ID, v.Number)}}, "blockers": []any{map[string]any{"text": "虚构卡点已经解决，该事项状态为done。", "evidence": evidence("item", p.Items[0].ID, 0)}}, "nextSteps": []any{map[string]any{"text": "根据虚构最新更正和最新预算续做。", "evidence": evidence("memory", string(f.Claims[p.CurrentMemory]), 1)}}}))
		}
		if logical == "effort" {
			items := []any{}
			for _, it := range p.Items {
				if it.Due != "" {
					items = append(items, map[string]any{"id": it.ID, "estimatedHours": 8, "hours": 8, "reason": "虚构步骤需要两个半天，每天可投入四小时。"})
				}
			}
			out = string(asJSON(map[string]any{"items": items}))
		}
		if override != nil {
			out = override(logical, out)
		}
		m.mu.Lock()
		m.Calls[logical] = append(m.Calls[logical], phase26Call{Stage: logical, System: sys, Prompt: prompt, Output: out})
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"text": out, "choices": []any{map[string]any{"message": map[string]string{"content": out}}}})
	}))
	t.Cleanup(server.Close)
	f.Store.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "phase3", Providers: []ai.Provider{{ID: "phase3", Name: "Fictitious phase3 acceptance model", Protocol: "openai", BaseURL: server.URL, Model: "fictitious", CostMode: "free", MaxOutput: 8192}}}})
	return m
}
func (m *phase3Model) count(stage string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls[stage])
}
func phase3ProcessOne(t *testing.T, f *phase3LoadedFixture, logical string, now time.Time) worker.Job {
	t.Helper()
	stage := phase3StageName(t, logical)
	phase26Isolate(t, f.phase26LoadedFixture, stage)
	if err := phase3Schedule(f.Store, f.Context, logical, now); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f.phase26LoadedFixture, stage)
	job := phase26ClaimStage(t, f.phase26LoadedFixture, stage)
	if err := phase3Process(f.Store, f.Context, logical, job); err != nil {
		t.Fatal(err)
	}
	return job
}
func phase3ValidateEvidence(t *testing.T, f *phase3LoadedFixture, h phase3Handover) {
	t.Helper()
	sections := [][]phase3Sentence{h.Conclusion, h.Blockers, h.NextSteps}
	sentences := 0
	for _, section := range sections {
		for _, s := range section {
			sentences++
			if s.Text == "" || len(s.Evidence) == 0 {
				t.Fatal("empty/unattributed sentence", s)
			}
			for _, e := range s.Evidence {
				var exists bool
				query := ""
				args := []any{f.Scope.OwnerID, e.ID}
				switch e.Kind {
				case "memory":
					query = `SELECT EXISTS(SELECT 1 FROM claims c JOIN claim_revisions v ON (v.owner_id,v.claim_id)=(c.owner_id,c.id) WHERE c.owner_id=$1 AND c.id=$2 AND c.retired='' AND v.version=$3)`
					args = append(args, e.Version)
				case "item":
					query = `SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2)`
				case "run":
					query = `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND id=$2)`
				case "documentVersion":
					var body phase3Version
					phase3NewHTTP(t, f).get(t, f.Context, phase3DocumentPath(e.ID, fmt.Sprintf("versions/%d", e.Version)), &body)
					exists = body.Version == e.Version
				default:
					t.Fatal("unknown evidence kind", e.Kind)
				}
				if query != "" {
					if err := f.Store.pool.QueryRow(f.Context, query, args...).Scan(&exists); err != nil {
						t.Fatal(err)
					}
				}
				if !exists {
					t.Fatal("sentence has dangling/retired evidence", e)
				}
			}
		}
	}
	if sentences == 0 {
		t.Fatal("empty generated handover")
	}
}
func TestPhase3H1H2H8T3ContentAndEveryEvidenceExists(t *testing.T) {
	phase3Finding(t, "S-P3-001")
	f := phase3LoadFixture(t)
	gold := f.Gold.Projects[5]
	phase3OnlyProject(t, f, gold.ID)
	m := phase3NewModel(t, f)
	phase3ProcessOne(t, f, "project_handover", time.Now())
	h := phase3NewHTTP(t, f)
	var got phase3Handover
	h.get(t, f.Context, phase3ProjectPath(gold.ID, "handover"), &got)
	phase3ValidateEvidence(t, f, got)
	doc := gold.Documents[0]
	latest := doc.Versions[len(doc.Versions)-1]
	if len(got.Conclusion) == 0 || len(got.Blockers) == 0 || !strings.Contains(got.Conclusion[0].Text, fmt.Sprintf("%d虚构单位", latest.BudgetUnits)) || !strings.Contains(got.Blockers[0].Text, "done") {
		t.Fatal("handover lost planted newest content", got)
	}
	validNewestReference := false
	for _, sentence := range got.Conclusion {
		for _, ref := range sentence.Evidence {
			if ref.Kind == "documentVersion" && ref.ID == doc.ID && ref.Version == latest.Number {
				validNewestReference = true
			}
		}
	}
	if !validNewestReference {
		t.Fatal("T3 conclusion is not grounded in planted newest document version")
	}
	m.mu.Lock()
	calls := append([]phase26Call{}, m.Calls["project_handover"]...)
	m.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("calls=%d", len(calls))
	}
	for _, call := range calls {
		for _, text := range []string{latest.Body, gold.Items[0].ID, "done", string(f.Claims[gold.ScopedMemory])} {
			if !phase3PromptHas(call.Prompt, text) {
				t.Errorf("H1 missing original/latest input %q", text)
			}
		}
		for _, d := range gold.Documents {
			latest := d.Versions[len(d.Versions)-1]
			if !phase3PromptHas(call.Prompt, latest.Body) {
				t.Error("H1 omitted a document's latest original body", d.ID)
			}
		}
		for _, r := range gold.Runs {
			shouldInclude := r.Adopted != nil || r.ID == gold.Runs[3].ID
			if phase3PromptHas(call.Prompt, r.Output) != shouldInclude {
				t.Error("H1 adopted/latest-unadopted selection", r.ID, shouldInclude)
			}
		}
		if !phase3PromptHas(call.Prompt, "Fictitious project proposal deadline") || !phase3PromptHas(call.Prompt, string(f.Claims[f.Gold.Base.Groups[gold.Entity].Members[0]])) {
			t.Error("H1 deadline/entity-bound project memories omitted")
		}
		if strings.Contains(call.Prompt, string(f.Claims[gold.OldMemory])) {
			t.Error("retired memory was given to handover")
		}
		if !strings.Contains(call.System+call.Prompt, "资料") || !strings.Contains(call.System+call.Prompt, "指令") {
			t.Error("H8 data/instruction boundary missing")
		}
	}
}
func TestPhase3H3T2CoalescesEditsAndDoneStopsRewrite(t *testing.T) {
	phase3Finding(t, "S-P3-001")
	f := phase3LoadFixture(t)
	p := f.Gold.Projects[0]
	phase3OnlyProject(t, f, p.ID)
	m := phase3NewModel(t, f)
	phase3ProcessOne(t, f, "project_handover", time.Now())
	api := phase3NewHTTP(t, f)
	var before phase3Handover
	api.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &before)
	if before.WrittenAt == nil {
		t.Fatal("missing persisted write time")
	}
	written, err := time.Parse(time.RFC3339Nano, *before.WrittenAt)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		api.command(t, f.Context, map[string]any{"type": "updateTask", "id": p.Items[0].ID, "patch": map[string]string{"notes": fmt.Sprintf("虚构第%d次连续修订", i)}})
		if err := phase3Schedule(f.Store, f.Context, "project_handover", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var stale phase3Handover
	api.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &stale)
	if !stale.Stale || !reflect.DeepEqual(stale.WrittenAt, before.WrittenAt) || !reflect.DeepEqual(stale.Conclusion, before.Conclusion) {
		t.Fatal("stale read did not retain last good time/content")
	}
	stage := phase3StageName(t, "project_handover")
	// PostgreSQL timestamps resolve microseconds. Inspect ready work at each
	// exact persisted-time boundary, allowing an early queue entry if deferred.
	for _, offset := range []time.Duration{time.Hour - time.Microsecond, time.Hour, time.Hour + time.Microsecond} {
		at := written.Add(offset)
		if err := phase3Schedule(f.Store, f.Context, "project_handover", at); err != nil {
			t.Fatal(err)
		}
		var ready int
		if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND dedupe_key LIKE $2 AND state='queued' AND available_at<=$3`, f.Scope.OwnerID, stage+":%", at).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if offset < time.Hour && ready != 0 {
			t.Fatal("rewrite ready before one hour", ready)
		}
		if offset >= time.Hour && ready != 1 {
			t.Fatal("edits did not coalesce into one ready rewrite", ready)
		}
	}
	if m.count("project_handover") != 1 {
		t.Fatal("rewrote within hour")
	}
	// Advance only owned fixture clocks; now execute the queued rewrite. Testing
	// scheduling alone would fail to detect a processor that repeatedly calls.
	phase3AgeHandoverClock(t, f)
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE memory_jobs SET available_at=now()-interval '1 second' WHERE owner_id=$1 AND dedupe_key LIKE $2 AND state='queued'`, f.Scope.OwnerID, stage+":%")
	job := phase26ClaimStage(t, f.phase26LoadedFixture, stage)
	if err := phase3Process(f.Store, f.Context, "project_handover", job); err != nil {
		t.Fatal(err)
	}
	if m.count("project_handover") != 2 {
		t.Fatal("coalesced rewrite did not make exactly one actual call")
	}
	api.command(t, f.Context, map[string]any{"type": "updateProject", "id": p.ID, "patch": map[string]string{"status": "done"}})
	phase3AgeHandoverClock(t, f)
	api.command(t, f.Context, map[string]any{"type": "updateTask", "id": p.Items[0].ID, "patch": map[string]string{"notes": "虚构完成项目又有输入变动"}})
	if err := phase3Schedule(f.Store, f.Context, "project_handover", time.Now()); err != nil {
		t.Fatal(err)
	}
	var ready int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND dedupe_key LIKE $2 AND state='queued' AND available_at<=now()`, f.Scope.OwnerID, stage+":%").Scan(&ready); err != nil || ready != 0 {
		t.Fatal("done project has ready rewrite", ready, err)
	}
	t.Logf("T4 single project edits=5 handover_calls=%d (initial=1, coalesced=1)", m.count("project_handover"))
}
func TestPhase3G7T4NewStageIndependentBudgets(t *testing.T) {
	for _, logical := range []string{"project_handover", "effort"} {
		t.Run(logical, func(t *testing.T) {
			phase3Finding(t, map[string]string{"project_handover": "S-P3-001", "effort": "S-P3-005"}[logical])
			f := phase3LoadFixture(t)
			stage := phase3StageName(t, logical)
			if backgroundHourlyBudgets[stage] != 10 {
				t.Fatalf("%s budget=%d want10", stage, backgroundHourlyBudgets[stage])
			}
			phase26Exec(t, f.phase26LoadedFixture, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) SELECT gen_random_uuid(),$1,0,$2 FROM generate_series(1,10)`, f.Scope.OwnerID, stage)
			err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error { return backgroundHourlyTx(f.Context, tx, stage, "phase3_limit") })
			var deferred *worker.JobError
			if !errors.As(err, &deferred) || !deferred.NoAttempt {
				t.Fatal("exhausted independent stage borrowed capacity", err)
			}
			for other, limit := range backgroundHourlyBudgets {
				if other == stage {
					continue
				}
				if limit <= 0 {
					t.Fatal("invalid stage limit")
				}
				if err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error { return backgroundHourlyTx(f.Context, tx, other, "phase3_limit") }); err != nil {
					t.Errorf("%s consumed %s budget: %v", stage, other, err)
				}
			}
		})
	}
}

func phase3NewStageFixture(t *testing.T, logical string) (*phase3LoadedFixture, *phase3Model, string, worker.Job) {
	t.Helper()
	phase3Finding(t, map[string]string{"project_handover": "S-P3-001", "effort": "S-P3-005"}[logical])
	f := phase3LoadFixture(t)
	m := phase3NewModel(t, f)
	stage := phase3StageName(t, logical)
	phase26Isolate(t, f.phase26LoadedFixture, stage)
	if err := phase3Schedule(f.Store, f.Context, logical, time.Now()); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f.phase26LoadedFixture, stage)
	job := phase26ClaimStage(t, f.phase26LoadedFixture, stage)
	return f, m, stage, job
}
func TestPhase3T2T4NewStagesSchedulerProcessorUserOverlap(t *testing.T) {
	for _, logical := range []string{"project_handover", "effort"} {
		t.Run(logical, func(t *testing.T) {
			f, m, stage, job := phase3NewStageFixture(t, logical)
			h := phase3NewHTTP(t, f)
			entered, release := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			m.mu.Lock()
			m.Before = func(ctx context.Context, which string) {
				if which == logical {
					once.Do(func() { close(entered) })
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}
			m.mu.Unlock()
			ctx, cancel := context.WithTimeout(f.Context, 45*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- phase3Process(f.Store, ctx, logical, job) }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatal("no actual model overlap", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			schedules := make(chan error, 1)
			go func() {
				for i := 0; i < 4; i++ {
					err := phase3Schedule(f.Store, ctx, logical, time.Now())
					var busy *worker.JobError
					if err != nil && !(errors.As(err, &busy) && busy.Code == "background_write_busy") {
						schedules <- err
						return
					}
				}
				schedules <- nil
			}()
			readings := []time.Duration{}
			for i := 0; i < 3; i++ {
				var state json.RawMessage
				readings = append(readings, h.get(t, ctx, "/v1/workspace", &state))
				h.command(t, ctx, map[string]any{"type": "addTask", "title": "虚构并发用户事项", "projectId": f.Gold.Projects[29].ID})
				code, raw, err := h.call(ctx, "POST", "/v1/desk/turn", map[string]any{"requestId": string(memory.NewID()), "agentId": "phase3", "thingId": f.Gold.Projects[29].ID, "text": "虚构项目现在是什么情况？"})
				if err != nil || code != 200 {
					t.Errorf("studio foreground status=%d body=%s err=%v", code, raw, err)
				}
			}
			if err := <-schedules; err != nil {
				t.Error("scheduler", err)
			}
			unblock()
			err := <-done
			var busy *worker.JobError
			if errors.As(err, &busy) && busy.Code == "background_write_busy" {
				err = phase3Process(f.Store, ctx, logical, job)
			}
			if err != nil {
				t.Fatal("processor", err)
			}
			if m.count(logical) != 1 {
				t.Errorf("actual paid calls=%d want1", m.count(logical))
			}
			var state string
			if err := f.Store.pool.QueryRow(ctx, `SELECT state FROM memory_jobs WHERE id=$1`, job.ID).Scan(&state); err != nil || state != "done" {
				t.Fatal("queue did not complete", state, err)
			}
			phase3Latency(t, stage+"/concurrent_workspace", readings)
		})
	}
}
func TestPhase3G3NewStagesPaidResultsRetryWriteWithoutNewModel(t *testing.T) {
	for _, logical := range []string{"project_handover", "effort"} {
		t.Run(logical, func(t *testing.T) {
			f, m, _, job := phase3NewStageFixture(t, logical)
			table := "work_items"
			if logical == "project_handover" {
				rows, err := f.Store.pool.Query(f.Context, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename LIKE '%project%handover%'`)
				if err != nil {
					t.Fatal(err)
				}
				tables := []string{}
				for rows.Next() {
					var name string
					if err := rows.Scan(&name); err != nil {
						t.Fatal(err)
					}
					tables = append(tables, name)
				}
				rows.Close()
				if len(tables) != 1 {
					t.Fatal("handover persistence adapter needs one table", tables)
				}
				table = tables[0]
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			m.mu.Lock()
			m.Before = func(ctx context.Context, which string) {
				if which == logical {
					once.Do(func() { close(entered) })
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}
			m.mu.Unlock()
			done := make(chan error, 1)
			go func() { done <- phase3Process(f.Store, f.Context, logical, job) }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatal("no actual paid attempt", err)
			case <-time.After(20 * time.Second):
				close(release)
				t.Fatal("provider barrier")
			}
			tx, err := f.Store.pool.Begin(f.Context)
			if err != nil {
				close(release)
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(f.Context, "LOCK TABLE "+pgx.Identifier{table}.Sanitize()+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			err = <-done
			var busy *worker.JobError
			if !errors.As(err, &busy) || busy.Code != "background_write_busy" {
				t.Fatal("write failure was not deferred", err)
			}
			var saved int
			if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM background_model_results WHERE job_id=$1`, job.ID).Scan(&saved); err != nil || saved != 1 {
				t.Fatal("paid output not durable", saved, err)
			}
			if err := tx.Rollback(f.Context); err != nil {
				t.Fatal(err)
			}
			restarted, err := Open(f.Context, f.Store.pool.Config().ConnString())
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			restarted.SetModels(f.Store.models)
			if err := phase3Process(restarted, f.Context, logical, job); err != nil {
				t.Fatal(err)
			}
			if m.count(logical) != 1 {
				t.Fatal("restart called model again", m.count(logical))
			}
		})
	}
}
func TestPhase3G7T2T4ExhaustionRetainsAndEventuallyDrainsThirtyProjects(t *testing.T) {
	for _, logical := range []string{"project_handover", "effort"} {
		t.Run(logical, func(t *testing.T) {
			f, m, stage, job := phase3NewStageFixture(t, logical)
			phase26Exec(t, f.phase26LoadedFixture, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) SELECT gen_random_uuid(),$1,0,$2 FROM generate_series(1,10)`, f.Scope.OwnerID, stage)
			err := phase3Process(f.Store, f.Context, logical, job)
			var limited *worker.JobError
			if !errors.As(err, &limited) || !limited.NoAttempt {
				t.Fatal("budget overrun did not preserve pending work", err)
			}
			if err := f.Store.Defer(f.Context, job, limited.Code, limited.Until, true); err != nil {
				t.Fatal(err)
			}
			if m.count(logical) != 0 {
				t.Fatal("provider called beyond budget")
			}
			var pending int
			if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM memory_jobs WHERE id=$1 AND state='queued'`, job.ID).Scan(&pending); err != nil || pending != 1 {
				t.Fatal("queued overflow disappeared", pending, err)
			}
			for hour := 0; hour < 4; hour++ {
				// Advance ONLY this owned test ledger, never wait an actual hour or delete
				// usage/jobs. Retained old-hour history stays available for audit.
				phase26Exec(t, f.phase26LoadedFixture, `UPDATE background_usage SET created_at=created_at-interval '2 hours' WHERE owner_id=$1 AND stage=$2`, f.Scope.OwnerID, stage)
				before := m.count(logical)
				phase26Isolate(t, f.phase26LoadedFixture, stage)
				if err := phase3Schedule(f.Store, f.Context, logical, time.Now()); err != nil {
					t.Fatal(err)
				}
				phase26Isolate(t, f.phase26LoadedFixture, stage)
				for i := 0; i < 11; i++ {
					next, err := f.Store.Claim(f.Context, 5*time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					if next == nil {
						break
					}
					if !strings.HasPrefix(next.Stage, stage+":") {
						t.Fatal("isolation picked another stage")
					}
					err = phase3Process(f.Store, f.Context, logical, *next)
					if err != nil {
						var e *worker.JobError
						if !errors.As(err, &e) || !e.NoAttempt {
							t.Fatal(err)
						}
						if err := f.Store.Defer(f.Context, *next, e.Code, e.Until, true); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
				calls := m.count(logical) - before
				if calls > 10 {
					t.Fatalf("hour %d calls=%d >10", hour, calls)
				}
				t.Logf("T4 stage=%s simulated_hour=%d actual_calls=%d", stage, hour, calls)
			}
			if m.count(logical) != 30 {
				t.Fatalf("thirty eligible project batches lost/duplicated: actual=%d", m.count(logical))
			}
			h := phase3NewHTTP(t, f)
			for _, p := range f.Gold.Projects {
				if logical == "project_handover" {
					var got phase3Handover
					h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &got)
					if got.WrittenAt == nil {
						t.Error("starved project", p.ID)
					}
				} else {
					var got phase3Timeline
					h.get(t, f.Context, phase3ProjectPath(p.ID, "timeline"), &got)
					for _, it := range got.Items {
						if it.Status == "todo" && it.EstimatedHours == nil {
							t.Error("lost eligible effort estimate", it.ID)
						}
					}
				}
			}
		})
	}
}

// Input envelopes can be structured JSON or plain text. Compare decoded string
// content, never fail a correct JSON prompt just because newlines are escaped.
func phase3PromptHas(prompt, want string) bool {
	if strings.Contains(prompt, want) {
		return true
	}
	var value any
	if json.Unmarshal([]byte(prompt), &value) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(v any) bool {
		switch x := v.(type) {
		case string:
			return strings.Contains(x, want)
		case []any:
			for _, child := range x {
				if visit(child) {
					return true
				}
			}
		case map[string]any:
			for _, child := range x {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}

func TestPhase3G4T4SingleScopedMemoryEditRewritesOnlyRelatedProject(t *testing.T) {
	phase3Finding(t, "S-P3-001")
	f := phase3LoadFixture(t)
	p, other := f.Gold.Projects[0], f.Gold.Projects[1]
	phase3OnlyProject(t, f, p.ID)
	h := phase3NewHTTP(t, f)
	h.command(t, f.Context, map[string]any{"type": "updateProject", "id": other.ID, "patch": map[string]string{"status": "active"}})
	m := phase3NewModel(t, f)
	phase3ProcessOne(t, f, "project_handover", time.Now())
	phase3ProcessOne(t, f, "project_handover", time.Now())
	phase3AgeHandoverClock(t, f)
	var previousOther phase3Handover
	h.get(t, f.Context, phase3ProjectPath(other.ID, "handover"), &previousOther)
	if previousOther.Stale || previousOther.WrittenAt == nil {
		t.Fatal("unrelated project baseline is not fresh")
	}
	beforeCalls := m.count("project_handover")
	start := time.Now()
	h.command(t, f.Context, map[string]any{"type": "editMemory", "id": string(f.Claims[p.ScopedMemory]), "text": "虚构项目00的范围记忆更正：下一步核验新预算。", "reason": "虚构独立增量验收"})
	var current, unchanged phase3Handover
	h.get(t, f.Context, phase3ProjectPath(p.ID, "handover"), &current)
	h.get(t, f.Context, phase3ProjectPath(other.ID, "handover"), &unchanged)
	if !current.Stale || !reflect.DeepEqual(previousOther, unchanged) {
		t.Fatal("single scoped memory edit invalidated unrelated project", current, unchanged)
	}
	phase3ProcessOne(t, f, "project_handover", time.Now())
	delta := m.count("project_handover") - beforeCalls
	if delta != 1 {
		t.Fatal("single project memory edit model delta", delta)
	}
	h.get(t, f.Context, phase3ProjectPath(other.ID, "handover"), &unchanged)
	if !reflect.DeepEqual(previousOther, unchanged) {
		t.Fatal("unrelated project was rewritten")
	}
	t.Logf("T4 single scoped memory edit project_handover_calls=%d elapsed_ms=%.3f ceiling=10/hour; old-stage deltas measured by G4_T4_single_memory_calls", delta, phase3Milliseconds(time.Since(start)))
}
