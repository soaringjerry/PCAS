package postgres_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase25B1Items(n int, category string, durable bool) []phase25B1ModelItem {
	items := make([]phase25B1ModelItem, n)
	for i := range items {
		items[i] = phase25B1ModelItem{Number: i + 1, Category: category, Durable: durable}
	}
	return items
}

func (f *phase25B1Fixture) usages(t *testing.T) [][]memory.Ref {
	t.Helper()
	rows, err := f.db.Query(f.ctx, `SELECT memory_refs FROM model_usage WHERE owner_id=$1 AND purpose='organize' ORDER BY at,id`, f.scope.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var batches [][]memory.Ref
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var refs []memory.Ref
		if err := json.Unmarshal(raw, &refs); err != nil {
			t.Fatal(err)
		}
		batches = append(batches, refs)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return batches
}

func TestPhase25B1_X03_FortyThenFive(t *testing.T) {
	f := phase25B1NewFixture(t)
	refs := make([]memory.Ref, 45)
	for i := range refs {
		refs[i] = f.claim(t, fmt.Sprintf("虚构批量便签第%d条。", i))
	}
	fake := f.model(t, phase25B1ModelJSON(t, phase25B1Items(40, "identity", true)))
	f.batch(t)
	first := f.usages(t)
	if len(first) != 1 || len(first[0]) != 40 {
		t.Fatalf("first usage batch = %+v", first)
	}
	// The second job must already exist: do not schedule between these batches.
	j := f.claimOrganize(t)
	if j == nil {
		t.Fatal("continuation was not enqueued")
	}
	if err := f.store.ProcessOrganize(f.ctx, *j); err != nil {
		t.Fatal(err)
	}
	batches := f.usages(t)
	if len(batches) != 2 || len(batches[1]) != 5 || fake.calls() != 2 {
		t.Errorf("batches/calls = %+v/%d", batches, fake.calls())
	}
	for _, ref := range refs {
		f.checkClaim(t, ref, "identity", 1, 0)
	}
}

func TestPhase25B1_X04_CreateGroundedTopic(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构季度汇报需要准备三张插图。")
	f.model(t, phase25B1ModelJSON(t, []phase25B1ModelItem{{Number: 1, Category: "progress", Durable: false, Topics: []string{"季度汇报"}}}, phase25B1NewGroup{Type: "topic", Name: "季度汇报", Description: "季度汇报的准备和提交"}))
	f.batch(t)
	f.checkClaim(t, ref, "progress", 1, 0)
	var facets workspace.MemoryFacets
	f.get(t, "/v1/workspace/memory-facets", &facets)
	if len(facets.Groups) != 1 || facets.Groups[0].Name != "季度汇报" || facets.Groups[0].Type != "topic" || facets.Groups[0].Count != 1 {
		t.Errorf("facets = %+v", facets.Groups)
	}
	var description string
	if err := f.db.QueryRow(f.ctx, `SELECT disambiguation->>'description' FROM entity_versions WHERE owner_id=$1 AND name='季度汇报' AND entity_type='topic'`, f.scope.OwnerID).Scan(&description); err != nil {
		t.Fatal(err)
	}
	if description != "季度汇报的准备和提交" {
		t.Errorf("description = %s", description)
	}
}

func TestPhase25B1_X05_ModelCanDeclareSemanticTopic(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构人物陆青今天买了青色纸张。")
	f.model(t, phase25B1ModelJSON(t, []phase25B1ModelItem{{Number: 1, Category: "event", Durable: false, Topics: []string{"效率提升"}}}, phase25B1NewGroup{Type: "topic", Name: "效率提升", Description: "虚构说明"}))
	f.batch(t)
	f.checkClaim(t, ref, "event", 1, 0)
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND name='效率提升'`, f.scope.OwnerID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("ungrounded groups = %d", count)
	}
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) != 1 || m.Groups[0].Name != "效率提升" {
		t.Fatal(m.Groups)
	}
}

func TestPhase25B1_X06_ModelCanAddArea(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构人物许澄本周读宇宙探索小说。")
	f.model(t, phase25B1ModelJSON(t, []phase25B1ModelItem{{Number: 1, Category: "taste", Durable: true, Area: "宇宙探索"}}, phase25B1NewGroup{Type: "area", Name: "宇宙探索", Description: "虚构宇宙探索兴趣"}))
	f.batch(t)
	f.checkClaim(t, ref, "taste", 1, 0)
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) != 1 || m.Groups[0].Name != "宇宙探索" {
		t.Fatal(m.Groups)
	}
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND entity_type='area' AND name='宇宙探索'`, f.scope.OwnerID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("invalid area entities = %d", count)
	}
}

func TestPhase25B1_X07_AllDeclaredGroupsAccepted(t *testing.T) {
	f := phase25B1NewFixture(t)
	names := []string{"雪峰观察", "海湾漫步", "庭院种植", "纸船竞速", "灯塔绘制"}
	items := phase25B1Items(5, "event", false)
	var newGroups []phase25B1NewGroup
	refs := make([]memory.Ref, 5)
	for i := 4; i >= 0; i-- {
		refs[i] = f.claim(t, "虚构活动："+names[i]+"。")
		items[i].Topics = []string{names[i]}
	}
	for _, name := range names {
		newGroups = append(newGroups, phase25B1NewGroup{Type: "topic", Name: name, Description: "虚构活动说明"})
	}
	f.model(t, phase25B1ModelJSON(t, items, newGroups...))
	f.batch(t)
	for i, ref := range refs {
		f.checkClaim(t, ref, "event", 1, 0)
		m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
		if err != nil {
			t.Fatal(err)
		}
		var entities int
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND entity_type='topic' AND name=$2`, f.scope.OwnerID, names[i]).Scan(&entities); err != nil {
			t.Fatal(err)
		}
		if entities != 1 || len(m.Groups) != 1 || m.Groups[0].Name != names[i] {
			t.Errorf("declared group %d = entities:%d memory:%+v", i, entities, m.Groups)
		}
	}
}

func TestPhase25B1_X08_CorrectedDuringModelCall(t *testing.T) {
	f := phase25B1NewFixture(t)
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构月报")), Name: "虚构月报", Type: "topic"}
	refs := make([]memory.Ref, 3)
	for i := range refs {
		refs[i] = f.claim(t, fmt.Sprintf("虚构月报旧规则%d。", i))
		f.labels(t, refs[i], "rule", false, 0, topic)
	}
	var corrected memory.Ref
	reply := phase25B1ModelJSON(t, phase25B1Items(3, "event", true))
	fake := phase25B1NewFakeModel(t, func(call int, _ phase25B1ModelRequest) (phase25B1ModelResponse, error) {
		if call == 1 {
			var err error
			corrected, err = f.correction(refs[1], "虚构月报纠正后的两项结论。")
			if err != nil {
				return phase25B1ModelResponse{}, err
			}
		}
		return phase25B1ModelResponse{Text: reply}, nil
	})
	f.store.SetModels(fake.registry)
	f.batch(t)
	f.checkClaim(t, refs[0], "event", 1, 0)
	f.checkClaim(t, refs[2], "event", 1, 0)
	f.checkClaim(t, corrected, "rule", 0, 0)
	m, err := f.store.GetMemory(f.ctx, f.scope, string(corrected.ID))
	if err != nil {
		t.Fatal(err)
	}
	phase25B1AssertLabels(t, m, "rule", false, []workspace.MemoryGroup{topic})
	f.batch(t)
	f.checkClaim(t, corrected, "event", 1, 0)
}

func TestPhase25B1_X09_DeletedDuringModelCall(t *testing.T) {
	f := phase25B1NewFixture(t)
	refs := make([]memory.Ref, 3)
	for i := range refs {
		refs[i] = f.claim(t, fmt.Sprintf("虚构临时便签%d。", i))
	}
	reply := phase25B1ModelJSON(t, phase25B1Items(3, "event", false))
	fake := phase25B1NewFakeModel(t, func(_ int, _ phase25B1ModelRequest) (phase25B1ModelResponse, error) {
		err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{refs[1]}})
		return phase25B1ModelResponse{Text: reply}, err
	})
	f.store.SetModels(fake.registry)
	f.batch(t)
	f.checkClaim(t, refs[0], "event", 1, 0)
	f.checkClaim(t, refs[2], "event", 1, 0)
	var count int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, refs[1].ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("deleted claim reappeared")
	}
}

func TestPhase25B1_X10_BadJSONKeepsGoodLabelsAndRetries(t *testing.T) {
	f := phase25B1NewFixture(t)
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构旧分组")), Name: "虚构旧分组", Type: "topic"}
	ref := f.claim(t, "虚构正文标记，失败不得丢失")
	f.labels(t, ref, "rule", true, 0, topic)
	fake := f.model(t, "deliberately invalid JSON")
	var previous time.Duration
	for attempt := 1; attempt <= 4; attempt++ {
		f.exec(t, "UPDATE claims SET organize_after=now() WHERE owner_id=$1", f.scope.OwnerID)
		f.batch(t)
		f.checkClaim(t, ref, "rule", 0, attempt)
		var after time.Time
		if err := f.db.QueryRow(f.ctx, "SELECT organize_after FROM claims WHERE owner_id=$1 AND id=$2", f.scope.OwnerID, ref.ID).Scan(&after); err != nil {
			t.Fatal(err)
		}
		delay := time.Until(after)
		if delay <= previous {
			t.Fatal("backoff must increase", delay, previous)
		}
		previous = delay
		m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
		if err != nil {
			t.Fatal(err)
		}
		phase25B1AssertLabels(t, m, "rule", true, []workspace.MemoryGroup{topic})
	}
	if fake.calls() != 4 {
		t.Fatal("invalid result must remain retryable", fake.calls())
	}
}

func TestPhase25B1_X11_UnknownNumberInvalidCategoryAndMissingItem(t *testing.T) {
	f := phase25B1NewFixture(t)
	refs := make([]memory.Ref, 4)
	for i := 3; i >= 0; i-- {
		refs[i] = f.claim(t, fmt.Sprintf("虚构记忆%c。", 'A'+i))
	}
	f.model(t, phase25B1ModelJSON(t, []phase25B1ModelItem{
		{Number: 1, Category: "rule", Durable: true}, {Number: 2, Category: "invalid-type", Durable: true},
		{Number: 4, Category: "rule", Durable: true}, {Number: 999, Category: "event", Durable: true},
		{Number: 1, Category: "goal", Durable: false},
	}))
	f.batch(t)
	for i, ref := range refs {
		if i == 0 || i == 3 {
			f.checkClaim(t, ref, "rule", 1, 0)
		} else {
			f.checkClaim(t, ref, "unknown", 0, 1)
		}
	}
}

func TestPhase25B1_R09_RejectNonBooleanDurable(t *testing.T) {
	for _, durable := range []any{nil, "true", 1} {
		t.Run(fmt.Sprintf("%v", durable), func(t *testing.T) {
			f := phase25B1NewFixture(t)
			ref := f.claim(t, "虚构记忆要求使用紫色标题。")
			f.model(t, phase25B1ModelJSON(t, []phase25B1ModelItem{{Number: 1, Category: "rule", Durable: durable}, {Number: 1, Category: "rule", Durable: true}}))
			f.batch(t)
			f.checkClaim(t, ref, "unknown", 0, 1)
		})
	}
}

func TestPhase25B1_X12_TransactionRollsBackAndRetries(t *testing.T) {
	f := phase25B1NewFixture(t)
	refs := make([]memory.Ref, 3)
	for i := range refs {
		refs[i] = f.claim(t, "虚构事务需要整理三条便签。")
	}
	reply := phase25B1ModelJSON(t, []phase25B1ModelItem{
		{Number: 1, Category: "rule", Durable: true, Topics: []string{"虚构事务"}},
		{Number: 2, Category: "rule", Durable: true, Topics: []string{"虚构事务"}},
		{Number: 3, Category: "rule", Durable: true, Topics: []string{"虚构事务"}},
	}, phase25B1NewGroup{Type: "topic", Name: "虚构事务", Description: "虚构事务说明"})
	fake := phase25B1NewFakeModel(t, func(call int, _ phase25B1ModelRequest) (phase25B1ModelResponse, error) {
		if call == 1 {
			_, err := f.db.Exec(f.ctx, `CREATE SEQUENCE phase25_b1_write_order; CREATE FUNCTION phase25_b1_fail_second() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.category IS DISTINCT FROM OLD.category AND nextval('phase25_b1_write_order')=2 THEN RAISE EXCEPTION 'fictitious transaction failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER phase25_b1_fail_second BEFORE UPDATE ON claim_revisions FOR EACH ROW EXECUTE FUNCTION phase25_b1_fail_second()`)
			if err != nil {
				return phase25B1ModelResponse{}, err
			}
		}
		return phase25B1ModelResponse{Text: reply}, nil
	})
	f.store.SetModels(fake.registry)
	f.schedule(t)
	j := f.claimOrganize(t)
	if j == nil {
		t.Fatal("missing job")
	}
	if err := f.store.ProcessOrganize(f.ctx, *j); err == nil {
		t.Fatal("injected write failure was swallowed")
	}
	var ordinal int
	if err := f.db.QueryRow(f.ctx, `SELECT last_value FROM phase25_b1_write_order`).Scan(&ordinal); err != nil {
		t.Fatal(err)
	}
	if ordinal != 2 {
		t.Errorf("injection ordinal = %d", ordinal)
	}
	for _, ref := range refs {
		f.checkClaim(t, ref, "unknown", 0, 0)
	}
	var partial int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND role IN ('project','topic','area')`, f.scope.OwnerID).Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if partial != 0 {
		t.Errorf("partial group mentions = %d", partial)
	}
	var newEntities int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND entity_type='topic' AND name='虚构事务'`, f.scope.OwnerID).Scan(&newEntities); err != nil {
		t.Fatal(err)
	}
	if newEntities != 0 {
		t.Errorf("failed transaction left %d new group entities", newEntities)
	}
	f.exec(t, `DROP TRIGGER phase25_b1_fail_second ON claim_revisions; DROP FUNCTION phase25_b1_fail_second(); DROP SEQUENCE phase25_b1_write_order`)
	if err := f.store.Retry(f.ctx, *j, "fictitious_injected_failure"); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE id=$1`, j.ID)
	next := f.claimOrganize(t)
	if next == nil {
		t.Fatal("retry not claimable")
	}
	if err := f.store.ProcessOrganize(f.ctx, *next); err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		f.checkClaim(t, ref, "rule", 1, 0)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claim_mentions WHERE owner_id=$1 AND role='topic'`, f.scope.OwnerID).Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if partial != 3 {
		t.Errorf("retried mentions = %d", partial)
	}
	if fake.calls() != 1 {
		t.Fatal("write retry repeated a paid call", fake.calls())
	}

}

func TestPhase25B1_X14_RuleUpgradePreservesLabels(t *testing.T) {
	oldVersion := postgres.OrganizeVersion
	defer func() { postgres.OrganizeVersion = oldVersion }()
	f := phase25B1NewFixture(t)
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构月报")), Name: "虚构月报", Type: "topic"}
	refs := []memory.Ref{f.claim(t, "虚构月报先列结论。"), f.claim(t, "虚构月报用青色标题。")}
	first := phase25B1Items(2, "rule", true)
	for i := range first {
		first[i].Topics = []string{topic.Name}
	}
	f.model(t, phase25B1ModelJSON(t, first))
	f.batch(t)
	postgres.OrganizeVersion = oldVersion + 1
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if state.Organize != (workspace.Organize{Done: 0, Total: 2, Version: postgres.OrganizeVersion}) {
		t.Errorf("upgraded progress = %+v", state.Organize)
	}
	for _, ref := range refs {
		m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
		if err != nil {
			t.Fatal(err)
		}
		phase25B1AssertLabels(t, m, "rule", true, []workspace.MemoryGroup{topic})
	}
	f.model(t, phase25B1ModelJSON(t, phase25B1Items(2, "goal", false)))
	f.batch(t)
	for _, ref := range refs {
		f.checkClaim(t, ref, "goal", postgres.OrganizeVersion, 0)
	}
	f.get(t, "/v1/workspace", &state)
	if state.Organize != (workspace.Organize{Done: 2, Total: 2, Version: postgres.OrganizeVersion}) {
		t.Errorf("redone progress = %+v", state.Organize)
	}
}

func TestPhase25B1_X16_NoModelThenConfigured(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构尚无模型的便签。")
	for i := 0; i < 3; i++ {
		if f.schedule(t) != 0 || f.claimOrganize(t) != nil {
			t.Fatal("job queued without a model")
		}
	}
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if state.Organize != (workspace.Organize{Done: 0, Total: 1, Version: postgres.OrganizeVersion}) {
		t.Errorf("unconfigured progress = %+v", state.Organize)
	}
	fake := f.model(t, phase25B1ModelJSON(t, phase25B1Items(1, "identity", true)))
	f.batch(t)
	f.checkClaim(t, ref, "identity", 1, 0)
	if fake.calls() != 1 {
		t.Errorf("calls = %d", fake.calls())
	}
}

func TestPhase25B1_X17_ChannelUnavailableTwiceThenRecovers(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构模型暂时不可用的便签。")
	fake := f.model(t, phase25B1ModelJSON(t, phase25B1Items(1, "identity", true)))
	if f.schedule(t) != 1 {
		t.Fatal("configured model did not queue initial job")
	}
	// Keep the configured extraction provider, temporarily remove only this
	// fixture's credential. This exercises provider availability, not a paid
	// HTTP failure followed by the generic worker's exponential retry policy.
	t.Setenv("PCAS_P25_B1_FAKE_MODEL_KEY", "")
	if fake.registry.Available("phase25-b1-fake") {
		t.Fatal("fixture provider did not become unavailable")
	}
	for attempt := 0; attempt < 2; attempt++ {
		var stage string
		if err := f.db.QueryRow(f.ctx, `SELECT stage FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID).Scan(&stage); err != nil {
			t.Fatal(err)
		}
		runner := worker.New(f.store, map[string]worker.Handler{stage: f.store.ProcessOrganize, "memory.organize": f.store.ProcessOrganize}, slog.Default())
		worked, err := runner.RunOnce(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			t.Fatal("worker did not claim unavailable task")
		}
		f.checkClaim(t, ref, "unknown", 0, 0)
		if fake.calls() != 0 {
			t.Error("unavailable provider was called")
		}
		if f.claimOrganize(t) != nil {
			t.Fatal("deferred job immediately claimable")
		}
		var delay float64
		if err := f.db.QueryRow(f.ctx, `SELECT extract(epoch FROM available_at-now()) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID).Scan(&delay); err != nil {
			t.Fatal(err)
		}
		if delay < 50 || delay > 65 {
			t.Errorf("deferral seconds = %.2f", delay)
		}
		f.exec(t, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued'`, f.scope.OwnerID)
	}
	t.Setenv("PCAS_P25_B1_FAKE_MODEL_KEY", "fictitious-test-key")
	f.batch(t)
	f.checkClaim(t, ref, "identity", 1, 0)
	if fake.calls() != 1 {
		t.Errorf("recovered model calls = %d", fake.calls())
	}
}

func TestPhase25B1_X18_RecordUsageForValidAndInvalidJSON(t *testing.T) {
	f := phase25B1NewFixture(t)
	first := f.claim(t, "虚构付费调用正文标记甲。")
	f.model(t, phase25B1ModelJSON(t, phase25B1Items(1, "event", false)))
	f.batch(t)
	second := f.claim(t, "虚构付费调用正文标记乙。")
	f.model(t, "deliberately invalid JSON")
	f.batch(t)
	batches := f.usages(t)
	if len(batches) != 2 {
		t.Fatalf("usage calls = %d", len(batches))
	}
	for i, ref := range []memory.Ref{first, second} {
		if len(batches[i]) != 1 || batches[i][0] != ref {
			t.Errorf("usage %d refs = %+v, want %+v", i, batches[i], ref)
		}
	}
	var raw string
	if err := f.db.QueryRow(f.ctx, `SELECT jsonb_agg(memory_refs)::text FROM model_usage WHERE owner_id=$1 AND purpose='organize'`, f.scope.OwnerID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "正文标记") || strings.Contains(raw, "text") {
		t.Errorf("usage leaks body: %s", raw)
	}
}

func (f *phase25B1Fixture) model(t *testing.T, text string) *phase25B1FakeModel {
	t.Helper()
	fake := phase25B1NewFakeModel(t, func(int, phase25B1ModelRequest) (phase25B1ModelResponse, error) {
		return phase25B1ModelResponse{Text: text}, nil
	})
	f.store.SetModels(fake.registry)
	return fake
}

func (f *phase25B1Fixture) schedule(t *testing.T) int {
	t.Helper()
	// Fixture ingestion/Commit also queues unrelated work. This suite exercises
	// organizing only; remove that auxiliary fixture work, never organize jobs.
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE 'memory.organize:%'`, f.scope.OwnerID)
	n, err := f.store.ScheduleOrganize(f.ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *phase25B1Fixture) claimOrganize(t *testing.T) *worker.Job {
	t.Helper()
	j, err := f.store.Claim(f.ctx, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if j != nil {
		if !strings.HasPrefix(j.Stage, fmt.Sprintf("memory.organize:%d:", postgres.OrganizeVersion)) {
			t.Fatalf("unexpected queue stage %s", j.Stage)
		}
		var priority int
		if err := f.db.QueryRow(f.ctx, `SELECT priority FROM memory_jobs WHERE id=$1`, j.ID).Scan(&priority); err != nil {
			t.Fatal(err)
		}
		if priority != 4 {
			t.Fatalf("organize priority = %d", priority)
		}
	}
	return j
}

func (f *phase25B1Fixture) batch(t *testing.T) {
	t.Helper()
	f.schedule(t)
	j := f.claimOrganize(t)
	if j == nil {
		t.Fatal("no organize job claimed")
	}
	if err := f.store.ProcessOrganize(f.ctx, *j); err != nil {
		t.Fatal(err)
	}
}

func (f *phase25B1Fixture) checkClaim(t *testing.T, ref memory.Ref, category string, organized, attempts int) {
	t.Helper()
	if organized == 1 {
		organized = postgres.OrganizeVersion
	}
	var gotCategory string
	var gotOrganized, gotAttempts, version, revisions int
	if err := f.db.QueryRow(f.ctx, `SELECT cr.category,c.organized,c.organize_attempts,r.version,(SELECT count(*) FROM claim_revisions old WHERE old.owner_id=c.owner_id AND old.claim_id=c.id) FROM claims c JOIN memory_records r ON r.owner_id=c.owner_id AND r.id=c.id JOIN claim_revisions cr ON cr.owner_id=c.owner_id AND cr.claim_id=c.id AND cr.version=r.version WHERE c.owner_id=$1 AND c.id=$2`, f.scope.OwnerID, ref.ID).Scan(&gotCategory, &gotOrganized, &gotAttempts, &version, &revisions); err != nil {
		t.Fatal(err)
	}
	if gotCategory != category || gotOrganized != organized || gotAttempts != attempts || version != ref.Version || revisions != ref.Version {
		t.Errorf("%s category/organized/attempts/version/revisions = %s/%d/%d/%d/%d, want %s/%d/%d/%d/%d", ref.ID, gotCategory, gotOrganized, gotAttempts, version, revisions, category, organized, attempts, ref.Version, ref.Version)
	}
}

func (f *phase25B1Fixture) correction(ref memory.Ref, text string) (memory.Ref, error) {
	var replacement memory.Claim
	replacement.Revision = memory.Revision{Ref: ref, State: "active"}
	if err := f.db.QueryRow(f.ctx, `SELECT subject_id,predicate,nature,acquisition,confirmation FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version).Scan(&replacement.SubjectID, &replacement.Predicate, &replacement.Nature, &replacement.Acquisition, &replacement.Confirmation); err != nil {
		return memory.Ref{}, err
	}
	value, err := json.Marshal(text)
	if err != nil {
		return memory.Ref{}, err
	}
	replacement.Value = value
	return f.store.Correct(f.ctx, f.scope, memory.CorrectRequest{Target: ref, Replacement: replacement, Reason: "虚构验收纠正"})
}

func TestPhase25B1_X01_OrganizeInPlace(t *testing.T) {
	f := phase25B1NewFixture(t)
	topic := workspace.MemoryGroup{EntityID: string(f.entity(t, "topic", "虚构月报")), Name: "虚构月报", Type: "topic"}
	items := phase25B1Items(3, "rule", true)
	refs := make([]memory.Ref, 3)
	for i := range refs {
		refs[i] = f.claim(t, fmt.Sprintf("虚构月报要求 %d：使用青色标题。", i))
		items[i].Topics = []string{topic.Name}
	}
	fake := f.model(t, phase25B1ModelJSON(t, items))
	f.batch(t)
	for _, ref := range refs {
		f.checkClaim(t, ref, "rule", 1, 0)
		m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
		if err != nil {
			t.Fatal(err)
		}
		phase25B1AssertLabels(t, m, "rule", true, []workspace.MemoryGroup{topic})
	}
	if fake.calls() != 1 {
		t.Errorf("model calls = %d", fake.calls())
	}
}
