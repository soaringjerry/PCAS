package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestOrganizeSkeletonUpgradePreservesData(t *testing.T) {
	s := b1EmptyStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE TABLE schema_migrations (
 name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "035_memory_organize.sql" {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, string(body)); err != nil {
			t.Fatal(file.Name(), err)
		}
		if _, err := s.pool.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", file.Name(), fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			t.Fatal(err)
		}
	}
	b1Model(t, s, `{}`)
	scope := owner()
	source := b1Source(t, s, scope, "虚构园艺记录", "云杉喜欢在周末照料盆栽。", "manual")
	ref := b1Claim(t, s, scope, "云杉喜欢在周末照料盆栽。", "preference", "adopted", source)
	before := b1DatabaseRows(t, s, false)
	columns := b1DatabaseColumns(t, s, before)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, b1DatabaseRowsAtColumns(t, s, columns)) {
		t.Fatal("035 changed existing business data")
	}
	for range 2 {
		if err := s.Migrate(ctx); err != nil {
			t.Fatal("restart", err)
		}
	}
	var organized, attempts, applied int
	if err := s.pool.QueryRow(ctx, "SELECT organized,organize_attempts FROM claims WHERE owner_id=$1 AND id=$2", scope.OwnerID, ref.ID).Scan(&organized, &attempts); err != nil || organized != 0 || attempts != 0 {
		t.Fatal("unexpected organization defaults", organized, attempts, err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE name='035_memory_organize.sql'").Scan(&applied); err != nil || applied != 1 {
		t.Fatal("035 must be recorded once", applied, err)
	}
	m, err := s.GetMemory(ctx, scope, string(ref.ID))
	if err != nil || m.Category != "unknown" || m.Durable != nil || m.Groups == nil || len(m.Groups) != 0 || m.Version != 1 {
		t.Fatal("unexpected memory defaults", m, err)
	}
	facets, err := s.MemoryFacets(ctx, scope)
	if err != nil || facets.Groups == nil || len(facets.Groups) != 0 {
		t.Fatal("unexpected group facets", facets, err)
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Organize != (workspace.Organize{Done: 0, Total: 1, Version: OrganizeVersion}) {
		t.Fatal("unexpected organize snapshot", state.Organize, err)
	}
}

func TestOrganizeParserFirstWinsAndGroupsAreIndependent(t *testing.T) {
	items, _ := parseOrganizeOutput(`{
 "items": [
  {
   "category": "rule",
   "deadlines": [],
   "durable": true,
   "n": 99,
   "scope": "",
   "unrestricted": true
  },
  {
   "category": "bad",
   "deadlines": [],
   "durable": true,
   "n": 1
  },
  {
   "category": "rule",
   "deadlines": [],
   "durable": true,
   "n": 1,
   "scope": "",
   "unrestricted": true
  },
  {
   "category": "rule",
   "deadlines": [],
   "durable": "true",
   "n": 2,
   "scope": "",
   "unrestricted": true
  },
  {
   "area": "不在词表里",
   "category": "event",
   "deadlines": [],
   "durable": false,
   "n": 3,
   "project": [],
   "topics": [
    42,
    "季度汇报",
    "第三个主题"
   ]
  },
  {
   "category": "rule",
   "deadlines": [],
   "durable": null,
   "n": 4,
   "scope": "",
   "unrestricted": true
  }
 ]
}`, 5)
	if len(items) != 0 {
		t.Fatal("malformed output must leave the batch pending, without partial labels", items)
	}
	valid, _ := parseOrganizeOutput(`{"items":[{"n":3,"category":"event","durable":false,"topics":["季度汇报","第三个主题"],"area":"虚构新增领域","deadlines":[]}],"new":[]}`, 5)
	if len(valid) != 1 || valid[3].Category != "event" || valid[3].Durable == nil || *valid[3].Durable || len(valid[3].Groups) != 3 {
		t.Fatal("valid output lost labels/groups", valid)
	}
	for _, invalid := range []string{"not JSON", `{"items":[`, `null`, `{"items":[]}`} {
		got, _ := parseOrganizeOutput(invalid, 3)
		if len(got) != 0 {
			t.Fatal(invalid, got)
		}
	}
}

func organizeTestMemory(t *testing.T, s *Store, scope memory.Scope, text string) memory.Ref {
	t.Helper()
	src := b1Source(t, s, scope, "虚构整理资料", text, "manual")
	return b1Claim(t, s, scope, text, "fact", "adopted", src)
}

func organizeTestJob(t *testing.T, s *Store, scope memory.Scope) worker.Job {
	t.Helper()
	if _, err := s.ScheduleOrganize(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	// Other stages are covered by their own suites. This fixture isolates the
	// organizer while still leasing its real scheduled job through the queue.
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE 'memory.organize:%'", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(context.Background(), 5*time.Minute)
	if err != nil || j == nil || !strings.HasPrefix(j.Stage, OrganizeStage+":") {
		t.Fatal("lease organizer", j, err)
	}
	return *j
}

func organizeTestReply(t *testing.T, w http.ResponseWriter, r *http.Request, category string) {
	t.Helper()
	var request struct {
		Messages []struct{ Role, Content string }
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Error(err)
		w.WriteHeader(500)
		return
	}
	var prompt struct{ Memories []organizeMemory }
	for _, m := range request.Messages {
		if m.Role == "user" {
			if err := json.Unmarshal([]byte(m.Content), &prompt); err != nil {
				t.Error(err)
			}
		}
	}
	items := []any{}
	for _, m := range prompt.Memories {
		items = append(items, map[string]any{"n": m.N, "category": category, "durable": true, "deadlines": []any{}, "unrestricted": true, "scope": "", "topics": []string{"季度汇报"}, "area": "工作"})
	}
	text := string(asJSON(map[string]any{"items": items, "new": []any{map[string]any{"type": "topic", "name": "季度汇报", "desc": "虚构季度汇报"}}}))
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 100}})
}

func TestOrganizeBatchesReadFiltersAndCorrection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); organizeTestReply(t, w, r, "rule") })
	refs := []memory.Ref{}
	for i := 0; i < 45; i++ {
		refs = append(refs, organizeTestMemory(t, s, scope, fmt.Sprintf("虚构用户云杉的季度汇报第%d项材料已准备。", i)))
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "流萤项目"})
	for range 2 {
		j := organizeTestJob(t, s, scope)
		if err := s.ProcessOrganize(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("expected 40+5", calls.Load())
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Organize != (workspace.Organize{Done: 45, Total: 45, Version: OrganizeVersion}) {
		t.Fatal(state.Organize, err)
	}
	facets, err := s.MemoryFacets(ctx, scope)
	if err != nil || len(facets.Groups) != 2 {
		t.Fatal(facets, err)
	}
	var topic string
	for _, f := range facets.Groups {
		if f.Count != 45 {
			t.Fatal(f)
		}
		if f.Type == "topic" {
			topic = f.EntityID
		}
	}
	page, err := s.ListMemories(ctx, scope, workspace.MemoryQuery{Group: topic, Category: "rule", Limit: 2})
	if err != nil || page.Total != 45 || len(page.Items) != 2 || page.Next == "" {
		t.Fatal(page, err)
	}
	page, err = s.ListMemories(ctx, scope, workspace.MemoryQuery{Group: topic, Category: "event"})
	if err != nil || page.Total != 0 {
		t.Fatal(page, err)
	}
	var domains, projects int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE entity_type='area'),count(*) FILTER(WHERE entity_type='project') FROM entity_versions WHERE owner_id=$1", string(scope.OwnerID)).Scan(&domains, &projects); err != nil || domains != 11 || projects != 1 {
		t.Fatal(domains, projects, err)
	}
	before, err := s.GetMemory(ctx, scope, string(refs[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: before.ID, Text: "虚构用户云杉的季度汇报材料还差最后一项。"})
	after, err := s.GetMemory(ctx, scope, before.ID)
	if err != nil || after.Version != before.Version+1 || after.Category != "rule" || after.Durable == nil || !*after.Durable || !reflect.DeepEqual(before.Groups, after.Groups) {
		t.Fatal(after, err)
	}
	state, err = s.Snapshot(ctx, scope)
	if err != nil || state.Organize.Done != 44 {
		t.Fatal(state.Organize, err)
	}
	prior := OrganizeVersion
	OrganizeVersion++
	t.Cleanup(func() { OrganizeVersion = prior })
	state, err = s.Snapshot(ctx, scope)
	if err != nil || state.Organize.Done != 0 || state.Memories[0].Category != "rule" || len(state.Memories[0].Groups) != 2 {
		t.Fatal(state.Organize, err)
	}
}

func TestOrganizeBadOutputAndExhaustion(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	b1Model(t, s, "not JSON")
	ref := organizeTestMemory(t, s, scope, "虚构用户霜叶在周末散步。")
	for attempt := 1; attempt <= 4; attempt++ {
		if _, err := s.pool.Exec(ctx, "UPDATE claims SET organize_after=now() WHERE owner_id=$1", scope.OwnerID); err != nil {
			t.Fatal(err)
		}
		j := organizeTestJob(t, s, scope)
		if err := s.ProcessOrganize(ctx, j); err != nil {
			t.Fatal(err)
		}
		var got, version int
		if err := s.pool.QueryRow(ctx, "SELECT organize_attempts,organized FROM claims WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(ref.ID)).Scan(&got, &version); err != nil || got != attempt || (version != 0) {
			t.Fatal(got, version, err)
		}
	}
	var usages int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose='organize' AND memory_refs=$2::jsonb", string(scope.OwnerID), asJSON([]memory.Ref{ref})).Scan(&usages); err != nil || usages != 4 {
		t.Fatal(usages, err)
	}
	queued, err := s.ScheduleOrganize(ctx, time.Now())
	if err != nil || queued != 0 {
		t.Fatal(queued, err)
	}
}

func TestOrganizeConcurrentInvocationAndDeletion(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int32
	var erased memory.Ref
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{erased}}); err != nil {
			t.Error(err)
		}
		organizeTestReply(t, w, r, "event")
	})
	erased = organizeTestMemory(t, s, scope, "虚构云杉的季度汇报第一项记录。")
	kept := organizeTestMemory(t, s, scope, "虚构云杉的季度汇报第二项记录。")
	j := organizeTestJob(t, s, scope)
	go func() { done <- s.ProcessOrganize(ctx, j) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	err := s.ProcessOrganize(ctx, j)
	var failure *worker.JobError
	if !errors.As(err, &failure) || failure.Code != "organize_busy" || !failure.NoAttempt {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	m, err := s.GetMemory(ctx, scope, string(kept.ID))
	if err != nil || m.Category != "event" || m.Version != 1 {
		t.Fatal(m, err)
	}
	if _, err := s.GetMemory(ctx, scope, string(erased.ID)); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestOrganizeTransactionRollbackAndRecovery(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { organizeTestReply(t, w, r, "rule") })
	first := organizeTestMemory(t, s, scope, "虚构云杉的季度汇报第一项。")
	second := organizeTestMemory(t, s, scope, "虚构云杉的季度汇报第二项。")
	j := organizeTestJob(t, s, scope)
	_, err := s.pool.Exec(ctx, `CREATE FUNCTION organize_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.claim_id='`+string(first.ID)+`'::uuid THEN RAISE EXCEPTION 'synthetic transaction failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER organize_test_fail BEFORE UPDATE OF category ON claim_revisions FOR EACH ROW EXECUTE FUNCTION organize_test_fail()`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessOrganize(ctx, j); err == nil {
		t.Fatal("expected failure")
	}
	for _, ref := range []memory.Ref{first, second} {
		m, err := s.GetMemory(ctx, scope, string(ref.ID))
		if err != nil || m.Category != "unknown" || len(m.Groups) != 0 {
			t.Fatal(m, err)
		}
	}
	if _, err := s.pool.Exec(ctx, "DROP TRIGGER organize_test_fail ON claim_revisions"); err != nil {
		t.Fatal(err)
	}
	// Reprocessing the same fenced task reuses its paid output and recovers
	// all still-outdated claims after the result transaction rolls back.
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []memory.Ref{first, second} {
		m, err := s.GetMemory(ctx, scope, string(ref.ID))
		if err != nil || m.Category != "rule" || len(m.Groups) != 2 {
			t.Fatal(m, err)
		}
	}
}

func TestOrganizeConfiguredChannelAndHourlyLimit(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	b1Model(t, s, `{
 "items": [
  {
   "category": "taste",
   "deadlines": [],
   "durable": true,
   "n": 1
  }
 ]
}`)
	ref := organizeTestMemory(t, s, scope, "虚构霜叶喜欢蓝色的水杯。")
	models := s.models
	s.SetModels(nil)
	if n, err := s.ScheduleOrganize(ctx, time.Now()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	s.SetModels(models)
	j := organizeTestJob(t, s, scope)
	// Forty synthetic reservations fill classification's own rolling hour,
	// including zero-cost subscription requests.
	for range 40 {
		if _, err := s.pool.Exec(ctx, "INSERT INTO background_usage(owner_id,job_id,reserved_cost,stage) VALUES($1,$2,0,'memory.organize')", string(scope.OwnerID), string(j.ID)); err != nil {
			t.Fatal(err)
		}
	}
	err := s.ProcessOrganize(ctx, j)
	var failure *worker.JobError
	if !errors.As(err, &failure) || failure.Code != "organize_hourly_limit" || failure.Until.IsZero() || !failure.NoAttempt {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE background_usage SET created_at=now()-interval '2 hours'"); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	if m, err := s.GetMemory(ctx, scope, string(ref.ID)); err != nil || m.Category != "taste" {
		t.Fatal(m, err)
	}
}

func TestOrganizeMissingInvalidAndUnknownNumbers(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	b1Model(t, s, `{
 "items": [
  {
   "area": "宇宙探索",
   "category": "rule",
   "deadlines": [],
   "durable": true,
   "n": 1,
   "scope": "",
   "unrestricted": true
  },
  {
   "category": "bad",
   "deadlines": [],
   "durable": true,
   "n": 2
  },
  {
   "category": "rule",
   "deadlines": [],
   "durable": true,
   "n": 2,
   "scope": "",
   "unrestricted": true
  },
  {
   "category": "taste",
   "deadlines": [],
   "durable": false,
   "n": 4
  },
  {
   "category": "rule",
   "deadlines": [],
   "durable": true,
   "n": 99,
   "scope": "",
   "unrestricted": true
  }
 ]
}`)
	for i := 0; i < 4; i++ {
		organizeTestMemory(t, s, scope, fmt.Sprintf("虚构霜叶的第%d条盆栽观察。", i))
	}
	j := organizeTestJob(t, s, scope)
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT cl.organized,cl.organize_attempts,c.category FROM claims cl
 JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 WHERE cl.owner_id=$1 ORDER BY r.created_at DESC,r.id`, string(scope.OwnerID))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		i++
		var version, attempts int
		var category string
		if err := rows.Scan(&version, &attempts, &category); err != nil {
			t.Fatal(err)
		}
		if i == 1 || i == 4 {
			if version != OrganizeVersion || attempts != 0 {
				t.Fatal(i, version, attempts)
			}
		} else if version != 0 || attempts != 1 || category != "unknown" {
			t.Fatal(i, version, attempts, category)
		}
	}
	if err := rows.Err(); err != nil || i != 4 {
		t.Fatal(i, err)
	}
}

func TestOrganizeModelDeclaredGroupsHaveNoLexicalOrThreeGroupLimit(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	b1Model(t, s, `{
 "items": [
  {
   "category": "progress",
   "deadlines": [],
   "durable": false,
   "n": 1,
   "topics": [
    "季度汇报",
    "盆栽观察"
   ]
  },
  {
   "category": "progress",
   "deadlines": [],
   "durable": false,
   "n": 2,
   "topics": [
    "书店走访",
    "蓝杯计划"
   ]
  },
  {
   "area": "宇宙探索",
   "category": "progress",
   "deadlines": [],
   "durable": false,
   "n": 3,
   "topics": [
    "阅读记录",
    "效率提升"
   ]
  }
 ],
 "new": [
  {
   "name": "季度汇报",
   "type": "topic"
  },
  {
   "name": "盆栽观察",
   "type": "topic"
  },
  {
   "name": "书店走访",
   "type": "topic"
  },
  {
   "name": "蓝杯计划",
   "type": "topic"
  },
  {
   "name": "阅读记录",
   "type": "topic"
  },
  {
   "name": "效率提升",
   "type": "topic"
  }
 ]
}`)
	for range 3 {
		organizeTestMemory(t, s, scope, "虚构霜叶的季度汇报、盆栽观察、书店走访、蓝杯计划和阅读记录都在推进。")
	}
	j := organizeTestJob(t, s, scope)
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := s.pool.QueryRow(ctx, "SELECT array_agg(name ORDER BY name) FROM entity_versions WHERE owner_id=$1 AND entity_type='topic'", string(scope.OwnerID)).Scan(&names); err != nil || len(names) != 6 {
		t.Fatal(names, err)
	}
	for _, name := range []string{"季度汇报", "盆栽观察", "书店走访", "蓝杯计划", "阅读记录", "效率提升"} {
		if !oneOf(name, names...) {
			t.Fatal(names)
		}
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Organize.Done != 3 {
		t.Fatal(state.Organize, err)
	}
}

func TestOrganizeUnavailableDoesNotConsumeAttemptsAndBudgetDefers(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	b1Model(t, s, `{
 "items": [
  {
   "category": "taste",
   "deadlines": [],
   "durable": true,
   "n": 1
  }
 ]
}`)
	ref := organizeTestMemory(t, s, scope, "虚构霜叶喜欢蓝色水杯。")
	channelKey := "PCAS_TEST_O1_CHANNEL_AVAILABLE"
	t.Setenv(channelKey, "")
	s.models.Config.Providers[0].KeyEnv = channelKey
	j := organizeTestJob(t, s, scope)
	for range 2 {
		err := s.ProcessOrganize(ctx, j)
		var failure *worker.JobError
		if !errors.As(err, &failure) || failure.Code != "provider_unavailable" || !failure.NoAttempt {
			t.Fatal(err)
		}
	}
	var attempts, calls int
	if err := s.pool.QueryRow(ctx, "SELECT organize_attempts FROM claims WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(ref.ID)).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal(attempts, err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&calls); err != nil || calls != 0 {
		t.Fatal(calls, err)
	}
	t.Setenv(channelKey, "synthetic-channel-key")
	s.models.Config.Providers[0].InputPerMillion = 1
	if _, err := s.pool.Exec(ctx, `UPDATE workspace_owners SET settings=jsonb_set(settings,'{dailyBudget}','0') WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	err := s.ProcessOrganize(ctx, j)
	var failure *worker.JobError
	if !errors.As(err, &failure) || failure.Code != "budget_deferred" || failure.Until.Before(time.Now()) || !failure.NoAttempt {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE workspace_owners SET settings=jsonb_set(settings,'{dailyBudget}','10') WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	if m, err := s.GetMemory(ctx, scope, string(ref.ID)); err != nil || m.Category != "taste" {
		t.Fatal(m, err)
	}
}

func TestOrganizeCorrectionDuringCallSkipsOnlyCorrectedVersion(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var corrected memory.Ref
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: string(corrected.ID), Text: "虚构云杉的季度汇报材料又修订了一次。"})
		organizeTestReply(t, w, r, "progress")
	})
	corrected = organizeTestMemory(t, s, scope, "虚构云杉的季度汇报第一项材料。")
	kept := organizeTestMemory(t, s, scope, "虚构云杉的季度汇报第二项材料。")
	if _, err := s.pool.Exec(ctx, "UPDATE claim_revisions SET category='rule',durable=true WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(corrected.ID)); err != nil {
		t.Fatal(err)
	}
	j := organizeTestJob(t, s, scope)
	if err := s.ProcessOrganize(ctx, j); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMemory(ctx, scope, string(corrected.ID))
	if err != nil || m.Version != 2 || m.Category != "rule" || m.Durable == nil || !*m.Durable {
		t.Fatal(m, err)
	}
	var version int
	if err := s.pool.QueryRow(ctx, "SELECT organized FROM claims WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(corrected.ID)).Scan(&version); err != nil || version != 0 {
		t.Fatal(version, err)
	}
	m, err = s.GetMemory(ctx, scope, string(kept.ID))
	if err != nil || m.Version != 1 || m.Category != "progress" {
		t.Fatal(m, err)
	}
}

// Optional live walk-through uses a dedicated channel home and only a fresh
// disposable schema with sixty explicitly fictional inputs.
func TestOrganizeLiveSynthetic(t *testing.T) {
	home := os.Getenv("PCAS_O1_LIVE_CODEX_HOME")
	if home == "" {
		t.Skip("set dedicated PCAS_O1_LIVE_CODEX_HOME for synthetic live walk-through")
	}
	s := testStore(t)
	scope := owner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// Seed through existing synthetic helpers before replacing their fake model.
	b1Model(t, s, `{}`)
	for i := 0; i < 60; i++ {
		organizeTestMemory(t, s, scope, fmt.Sprintf("虚构人物云杉在流萤项目里准备季度汇报，第%d项是整理虚构的盆栽观察记录。", i))
	}
	codex, err := ai.NewCodex(os.Getenv("PCAS_O1_LIVE_CODEX_BINARY"), home)
	if err != nil {
		t.Fatal(err)
	}
	defer codex.Close()
	models, err := ai.Load(os.Getenv("PCAS_O1_LIVE_MODELS_FILE"), codex)
	if err != nil {
		t.Fatal(err)
	}
	if models.Config.Extraction == "" {
		models.Config.Extraction = "chatgpt"
	}
	s.SetModels(models)
	for step := 0; step < 10; step++ {
		state, err := s.Snapshot(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		if state.Organize.Done == state.Organize.Total {
			break
		}
		j := organizeTestJob(t, s, scope)
		if err := s.ProcessOrganize(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Organize.Done != 60 {
		t.Fatal(state.Organize, err)
	}
	var calls int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose='organize'", string(scope.OwnerID)).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	groups := []string{}
	rows, err := s.pool.Query(ctx, "SELECT entity_type || ':' || name FROM entity_versions WHERE owner_id=$1 AND entity_type IN('project','topic') ORDER BY entity_type,name", string(scope.OwnerID))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		groups = append(groups, name)
	}
	rows.Close()
	t.Logf("synthetic live walk-through: calls=%d done=%d total=%d groups=%v", calls, state.Organize.Done, state.Organize.Total, groups)
}

func TestOrganizeSkeletonMemoryJSON(t *testing.T) {
	for _, durable := range []*bool{nil, new(bool), new(true)} {
		data, err := json.Marshal(workspace.Memory{Durable: durable})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["category"]) != `"unknown"` || string(fields["groups"]) != "[]" {
			t.Fatal("missing defaults", string(data))
		}
		value, present := fields["durable"]
		if durable == nil && present || durable != nil && (!present || string(value) != fmt.Sprint(*durable)) {
			t.Fatal("durable must preserve unset, false and true", string(data))
		}
	}
}
