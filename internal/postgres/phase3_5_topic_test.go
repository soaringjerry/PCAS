package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Published #278: ScheduleStatus / ProcessTopicProject; memory.topic_project,
// entity_versions.disambiguation.work_item_id, topic_project_links/checks.
const phase35TopicStage = "memory.topic_project"

type phase35TopicProcessor interface {
	ProcessTopicProject(context.Context, worker.Job) error
}

func phase35ProcessTopic(t *testing.T, s *Store, ctx context.Context, j worker.Job) error {
	t.Helper()
	p, ok := any(s).(phase35TopicProcessor)
	if !ok {
		t.Fatal("#278 ProcessTopicProject seam not yet integrated")
	}
	return p.ProcessTopicProject(ctx, j)
}
func phase35TopicLoad(t *testing.T) *phase35Fixture {
	t.Helper()
	if _, ok := any(&Store{}).(phase35TopicProcessor); !ok {
		t.Fatal("#278 ProcessTopicProject seam not yet integrated")
	}
	return phase35Load(t)
}
func phase35TopicJob(t *testing.T, f *phase35Fixture, topic int) worker.Job {
	t.Helper()
	var stage string
	id := strings.TrimPrefix(f.Topics[topic], "entity:")
	err := f.Store.pool.QueryRow(f.Context, `SELECT stage FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'memory.topic_project:%' AND state='queued' ORDER BY created_at,stage LIMIT 1`, f.Scope.OwnerID, id).Scan(&stage)
	if err != nil {
		t.Fatal("eligible topic was not scheduled", err)
	}
	return leaseStage(t, f.Store, f.Scope, memory.Ref{ID: memory.ID(id), Version: 1, Kind: memory.EntityKind}, stage)
}
func phase35TopicMemoryDigest(t *testing.T, f *phase35Fixture) string {
	t.Helper()
	var out string
	err := f.Store.pool.QueryRow(f.Context, `SELECT md5(string_agg(to_jsonb(c)::text||c.xmin::text,'|' ORDER BY c.claim_id)) FROM claim_revisions c WHERE owner_id=$1`, f.Scope.OwnerID).Scan(&out)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func phase35TopicLink(t *testing.T, f *phase35Fixture, index int) string {
	t.Helper()
	var id string
	err := f.Store.pool.QueryRow(f.Context, `SELECT coalesce(disambiguation->>'work_item_id','') FROM entity_versions WHERE owner_id=$1 AND entity_id=$2 AND version=1`, f.Scope.OwnerID, strings.TrimPrefix(f.Topics[index], "entity:")).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestPhase35C2C4G1T4EligibleTopicLinksReceiptsUndoAndReplay(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	f := phase35TopicLoad(t)
	s, ctx := f.Store, f.Context
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"decision":"new","projectId":null,"name":"虚构声音档案项目","reason":"虚构持续目标与期限需要多步工作"}`)
	})
	before := phase35TopicMemoryDigest(t, f)
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 3; i++ {
		var n int
		err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'memory.topic_project:%'`, f.Scope.OwnerID, strings.TrimPrefix(f.Topics[i], "entity:")).Scan(&n)
		if err != nil || n != 0 {
			t.Fatalf("topic missing prerequisite %d got jobs=%d err=%v", i, n, err)
		}
	}
	j := phase35TopicJob(t, f, 0)
	if err := phase35ProcessTopic(t, s, ctx, j); err != nil {
		t.Fatal(err)
	}
	project := phase35TopicLink(t, f, 0)
	if project == "" {
		t.Fatal("eligible topic not connected")
	}
	st := phase35Snapshot(t, s, f.Scope)
	if len(st.Projects) != 1 {
		t.Fatalf("automatic projects=%d", len(st.Projects))
	}
	var provenance struct {
		Creation *struct {
			By, GroupKey string
			MemoryIDs    []string
		}
	}
	_ = json.Unmarshal(asJSON(st.Projects[0]), &provenance)
	if provenance.Creation == nil || provenance.Creation.By != "background_topic" || provenance.Creation.GroupKey != f.Topics[0] || len(provenance.Creation.MemoryIDs) == 0 {
		t.Fatal("topic project lost evidence provenance")
	}
	var action string
	if err := s.pool.QueryRow(ctx, `SELECT action_id::text FROM topic_project_links WHERE owner_id=$1 AND topic_id=$2 AND undone_at IS NULL`, f.Scope.OwnerID, strings.TrimPrefix(f.Topics[0], "entity:")).Scan(&action); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range st.Activity {
		if a.ID == action {
			found = true
		}
	}
	if !found {
		t.Fatal("topic project no away receipt")
	}
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var scheduled int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.project_handover:%' AND state='queued'`, f.Scope.OwnerID).Scan(&scheduled); err != nil || scheduled == 0 {
		t.Fatal("C4 topic project not in handover schedule", err)
	}
	if _, err := s.Undo(ctx, f.Scope, action); err != nil {
		t.Fatal(err)
	}
	if phase35TopicLink(t, f, 0) != "" || len(phase35Snapshot(t, s, f.Scope).Projects) != 0 {
		t.Fatal("topic undo did not disconnect/delete empty project")
	}
	if phase35TopicMemoryDigest(t, f) != before {
		t.Fatal("topic creation/undo rewrote memories")
	}
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var replay int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'memory.topic_project:%' AND state='queued'`, f.Scope.OwnerID, strings.TrimPrefix(f.Topics[0], "entity:")).Scan(&replay); err != nil || replay != 0 {
		t.Fatal("same evidence rescheduled after undo", replay, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("one eligible topic calls=%d want=1", calls.Load())
	}
	var excess int
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$1 AND reason='topic_project_evidence_left_in_group'`, f.Scope.OwnerID).Scan(&excess); err != nil || excess < 2 {
		t.Fatalf("12 memories, 10 supplied: overflow=%d err=%v", excess, err)
	}
	t.Logf("T4 eligible topic provider_calls=%d; per-page ceiling=max(1,ceil(projects/20)); memories unchanged", calls.Load())
}
func TestPhase35C2G7T4DailyCapDefersToLocalNextDayReusesPaidOutput(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	f := phase35TopicLoad(t)
	s, ctx := f.Store, f.Context
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE workspace_owners SET settings=settings||'{"timezone":"America/New_York"}'::jsonb WHERE owner_id=$1`, f.Scope.OwnerID)
	for i := 0; i < 4; i++ {
		phase26Exec(t, f.phase26LoadedFixture, `INSERT INTO topic_project_links(owner_id,topic_id,input_hash,evidence_hash,project_id,action_id,created_project) VALUES($1,$2,$3,$3,$4,$5,true)`, f.Scope.OwnerID, memory.NewID(), fmt.Sprint("fictional-prior-", i), memory.NewID(), memory.NewID())
	}
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"decision":"new","projectId":null,"name":"虚构第五项目","reason":"虚构目标已成熟"}`)
	})
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	j := phase35TopicJob(t, f, 0)
	err := phase35ProcessTopic(t, s, ctx, j)
	var deferred *worker.JobError
	if !errors.As(err, &deferred) || deferred.Code != "topic_project_daily_limit" || !deferred.NoAttempt {
		t.Fatalf("daily cap did not defer safely: %v", err)
	}
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Now().In(loc)
	want := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	if !deferred.Until.Equal(want) {
		t.Fatalf("local next day=%s want=%s", deferred.Until, want)
	}
	if phase35TopicLink(t, f, 0) != "" || len(phase35Snapshot(t, s, f.Scope).Projects) != 0 {
		t.Fatal("over-cap topic created partial project")
	}
	var paid int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE job_id=$1`, j.ID).Scan(&paid); err != nil || paid != 1 {
		t.Fatal("daily deferral lost paid result", paid, err)
	}
	// Move only four prior synthetic quota rows into yesterday; do not rerun model.
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE topic_project_links SET created_at=created_at-interval '2 days' WHERE owner_id=$1`, f.Scope.OwnerID)
	if err := phase35ProcessTopic(t, s, ctx, j); err != nil {
		t.Fatal("next-day result-only retry", err)
	}
	if calls.Load() != 1 || phase35TopicLink(t, f, 0) == "" {
		t.Fatalf("next-day calls=%d link=%s", calls.Load(), phase35TopicLink(t, f, 0))
	}
	if backgroundHourlyBudgets[phase35TopicStage] != 2 {
		t.Fatalf("independent topic hourly budget=%d want=2", backgroundHourlyBudgets[phase35TopicStage])
	}
}
func TestPhase35C3G5ExistingProjectBeyondFirstPage(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	f := phase35TopicLoad(t)
	s, ctx := f.Store, f.Context
	target := ""
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for i := 0; i < 21; i++ {
			id := fmt.Sprintf("35000000-0035-4000-8000-%012d", i)
			it := newItem("project", fmt.Sprintf("虚构目录项目%02d", i))
			it.ID = id
			it.Goal = "虚构不同名字的陶瓷声音手册目标"
			if err := saveItem(ctx, tx, f.Scope, it); err != nil {
				return err
			}
			if i == 20 {
				target = id
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		found := false
		for _, m := range req.Messages {
			if m.Role == "user" {
				var p struct{ Projects []struct{ ID string } }
				_ = json.Unmarshal([]byte(m.Content), &p)
				for _, it := range p.Projects {
					if it.ID == target {
						found = true
					}
				}
			}
		}
		decision := map[string]any{"decision": "new", "projectId": nil, "name": "虚构不应重复的项目", "reason": "本页无同一目标"}
		if found {
			decision["decision"] = "match"
			decision["projectId"] = target
			decision["reason"] = "不同名字但相同的目标"
		}
		secretaryModelReply(w, decision)
	})
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := phase35ProcessTopic(t, s, ctx, phase35TopicJob(t, f, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 || phase35TopicLink(t, f, 0) != target || len(phase35Snapshot(t, s, f.Scope).Projects) != 21 {
		t.Fatal("first page miss created duplicate or skipped later match")
	}
}
func TestPhase35C2G2UncertainAndInvalidKeepPending(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	for _, output := range []string{`{"decision":"uncertain","projectId":null,"name":"","reason":"证据不够"}`, `{"decision":"new","projectId":null,"name":"","reason":""}`} {
		t.Run(output, func(t *testing.T) {
			f := phase35TopicLoad(t)
			before := phase35TopicMemoryDigest(t, f)
			phase35Model(t, f.Store, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, output) })
			if _, err := f.Store.ScheduleStatus(f.Context, time.Now()); err != nil {
				t.Fatal(err)
			}
			j := phase35TopicJob(t, f, 0)
			err := phase35ProcessTopic(t, f.Store, f.Context, j)
			var failure *worker.JobError
			if !errors.As(err, &failure) || failure.Code == "" {
				t.Fatalf("unnamed uncertain/invalid failure: %v", err)
			}
			if err := f.Store.Retry(f.Context, j, failure.Code); err != nil {
				t.Fatal(err)
			}
			var state, reason string
			var attempts int
			if err := f.Store.pool.QueryRow(f.Context, `SELECT state,error_code,attempts FROM memory_jobs WHERE id=$1`, j.ID).Scan(&state, &reason, &attempts); err != nil || state != "queued" || reason == "" || attempts < 1 {
				t.Fatalf("not retained pending %s reason=%s count=%d err=%v", state, reason, attempts, err)
			}
			if phase35TopicMemoryDigest(t, f) != before || phase35TopicLink(t, f, 0) != "" || len(phase35Snapshot(t, f.Store, f.Scope).Projects) != 0 {
				t.Fatal("uncertainty changed good state")
			}
		})
	}
}
func TestPhase35C2G4SingleMemoryChangesOnlyAffectedTopic(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	f := phase35TopicLoad(t)
	s, ctx := f.Store, f.Context
	phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"decision":"new","projectId":null,"name":"虚构主题","reason":"虚构理由"}`)
	})
	// Make the goal-missing topic eligible. Its persisted queued job is the
	// independent unaffected witness, not an oracle derived from the edited one.
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE claim_revisions SET category='goal' WHERE owner_id=$1 AND claim_id=$2`, f.Scope.OwnerID, f.Claims[1412])
	phase26Exec(t, f.phase26LoadedFixture, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,title,original_text) VALUES($1,$2,$3,1,'deadline','虚构期限','虚构原话')`, f.Scope.OwnerID, memory.NewID(), f.Claims[1412])
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	digest := func() string {
		var out string
		err := s.pool.QueryRow(ctx, `SELECT md5(string_agg(to_jsonb(j)::text||xmin::text,'|' ORDER BY stage)) FROM memory_jobs j WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'memory.topic_project:%'`, f.Scope.OwnerID, strings.TrimPrefix(f.Topics[1], "entity:")).Scan(&out)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := digest()
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE claim_revisions SET value=to_jsonb('虚构主题0的一处进展改变'::text) WHERE owner_id=$1 AND claim_id=$2`, f.Scope.OwnerID, f.Claims[1401])
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if before != digest() {
		t.Fatal("one memory rescheduled/wrote unrelated topic")
	}
}

func TestPhase35C2G3TopicPaidRetryAfterWriteFailure(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	f := phase35TopicLoad(t)
	s, ctx := f.Store, f.Context
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"decision":"new","projectId":null,"name":"虚构只写入重试的主题项目","reason":"虚构主题目标成熟"}`)
	})
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	j := phase35TopicJob(t, f, 0)
	phase26Exec(t, f.phase26LoadedFixture, `CREATE FUNCTION phase35_reject_topic() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'phase35 injected topic write failure'; END $$; CREATE TRIGGER phase35_reject_topic BEFORE INSERT ON topic_project_links FOR EACH ROW EXECUTE FUNCTION phase35_reject_topic()`)
	if err := phase35ProcessTopic(t, s, ctx, j); err == nil {
		t.Fatal("topic business fault did not fail")
	}
	var paid int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE job_id=$1`, j.ID).Scan(&paid); err != nil || paid != 1 {
		t.Fatal("paid topic output lost", paid, err)
	}
	phase26Exec(t, f.phase26LoadedFixture, `DROP TRIGGER phase35_reject_topic ON topic_project_links; DROP FUNCTION phase35_reject_topic()`)
	peer, err := Open(ctx, s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peer.SetModels(s.models)
	if err := phase35ProcessTopic(t, peer, ctx, j); err != nil {
		t.Fatal("restart topic persist retry", err)
	}
	if calls.Load() != 1 || phase35TopicLink(t, f, 0) == "" {
		t.Fatalf("topic paid twice or not linked calls=%d", calls.Load())
	}
}
func TestPhase35C2G7IndependentHourlyAndNoBorrowing(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	if backgroundHourlyBudgets[phase35TopicStage] != 2 {
		t.Fatalf("published topic budget=%d want 2", backgroundHourlyBudgets[phase35TopicStage])
	}
	for stage, limit := range backgroundHourlyBudgets {
		if stage == phase35TopicStage {
			continue
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) SELECT gen_random_uuid(),$1,0,$2 FROM generate_series(1,$3)`, scope.OwnerID, stage, limit); err != nil {
			t.Fatal(err)
		}
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return backgroundHourlyTx(ctx, tx, phase35TopicStage, "phase35_topic_hourly_limit")
	}); err != nil {
		t.Fatal("other stages consumed topic capacity", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) SELECT gen_random_uuid(),$1,0,$2 FROM generate_series(1,2)`, scope.OwnerID, phase35TopicStage); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return backgroundHourlyTx(ctx, tx, phase35TopicStage, "phase35_topic_hourly_limit")
	})
	var deferred *worker.JobError
	if !errors.As(err, &deferred) || !deferred.NoAttempt || deferred.Until.IsZero() {
		t.Fatal("topic exceeded its independent two-call budget", err)
	}
}
func TestPhase35C2ConcurrentTopicProcessorSchedulerAndUserHTTP(t *testing.T) {
	phase35Finding(t, "S-P35-004")
	f := phase35TopicLoad(t)
	s, ctx := f.Store, f.Context
	entered := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			secretaryModelReply(w, `{"decision":"new","projectId":null,"name":"虚构并发主题项目","reason":"虚构目标成熟"}`)
		case <-r.Context().Done():
		}
	})
	if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	j := phase35TopicJob(t, f, 0)
	result := make(chan error, 1)
	go func() {
		p, ok := any(s).(phase35TopicProcessor)
		if !ok {
			result <- errors.New("published ProcessTopicProject missing")
			return
		}
		result <- p.ProcessTopicProject(ctx, j)
	}()
	select {
	case <-entered:
	case err := <-result:
		t.Fatal("processor did not overlap model", err)
	case <-time.After(15 * time.Second):
		t.Fatal("no topic provider barrier")
	}
	parallel := make(chan error, 2)
	go func() { _, err := s.ScheduleStatus(ctx, time.Now()); parallel <- err }()
	h := phase35HTTP(t, s, f.Scope)
	go func() {
		code, raw, err := h.call(ctx, "POST", "/v1/workspace/commands", map[string]any{"type": "addTask", "title": "虚构并发用户独立待办", "requestId": string(memory.NewID())})
		if err == nil && code != 200 {
			err = fmt.Errorf("HTTP %d %s", code, raw)
		}
		parallel <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case err := <-parallel:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("topic stage blocked foreground/scheduling")
		}
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("topic processor did not finish")
	}
	if calls.Load() != 1 || phase35TopicLink(t, f, 0) == "" || len(phase35Snapshot(t, s, f.Scope).Tasks) != 1 {
		t.Fatal("concurrent topic/user result lost or duplicated")
	}
}
