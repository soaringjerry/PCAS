package postgres_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (f *phase25B234Fixture) scheduleStatus(t *testing.T, now time.Time) int {
	t.Helper()
	f.statusNow = now
	// Remove unrelated fixture ingestion/index jobs; status jobs themselves
	// must be scheduled, leased and fenced through real queue operations.
	f.exec(t, `DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'memory.card:%' AND stage NOT LIKE 'memory.handover:%'`, f.scope.OwnerID)
	n, err := f.store.ScheduleStatus(f.ctx, now)
	if err != nil {
		var quota *worker.JobError
		if errors.As(err, &quota) && (quota.Code == "card_hourly_limit" || quota.Code == "handover_daily_limit" || quota.Code == "handover_interval") {
			return 0
		}
		if err.Error() == "card_hourly_limit" || err.Error() == "handover_daily_limit" || err.Error() == "handover_interval" {
			return 0
		}
		t.Fatal(err)
	}
	return n
}

func (f *phase25B234Fixture) statusJob(t *testing.T) string {
	t.Helper()
	// ScheduleStatus uses the supplied test clock for available_at. Advance
	// only this owner's status queue to make those simulated due jobs leasable
	// by the real wall-clock Claim API; never forge a Job or lease token.
	// Mutation APIs and chained scheduling use wall time, while these tests
	// age the owning inputs to the simulated clock. Align freshly scheduled
	// jobs as well; a worker-deferred job keeps its actual retry boundary.
	f.exec(t, `UPDATE memory_jobs SET available_at=least(available_at,now()) WHERE owner_id=$1 AND state='queued' AND (error_code='' OR available_at<=now() OR (error_code<>'card_hourly_limit' AND available_at<=$2)) AND (stage LIKE 'memory.card:%' OR stage LIKE 'memory.handover:%')`, f.scope.OwnerID, f.statusNow)
	j, err := f.store.Claim(f.ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		return ""
	}
	var priority int
	if err := f.db.QueryRow(f.ctx, `SELECT priority FROM memory_jobs WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, j.ID).Scan(&priority); err != nil {
		t.Fatal(err)
	}
	var processErr error
	if strings.HasPrefix(j.Stage, "memory.card:") {
		if priority != 13 {
			t.Errorf("card priority=%d, want 13", priority)
		}
		processErr = f.store.ProcessCard(f.ctx, *j)
	} else if strings.HasPrefix(j.Stage, "memory.handover:") {
		if priority != 14 {
			t.Errorf("handover priority=%d, want 14", priority)
		}
		processErr = f.store.ProcessHandover(f.ctx, *j)
	} else {
		t.Fatalf("unrelated job claimed: %s", j.Stage)
	}
	if processErr != nil {
		var deferred *worker.JobError
		if errors.As(processErr, &deferred) && deferred.Retry && !deferred.Until.IsZero() {
			if err := f.store.Defer(f.ctx, *j, deferred.Code, deferred.Until, deferred.NoAttempt); err != nil {
				t.Fatal(err)
			}
			return "deferred:" + j.Stage
		}
		// The documented handlers may report quota exhaustion as a plain
		// error. Use the public queue boundary to retain that leased job. Caps
		// are still verified from real successful model calls and ledger rows.
		if processErr.Error() == "card_hourly_limit" || processErr.Error() == "handover_daily_limit" || processErr.Error() == "handover_interval" {
			until := time.Now().Add(time.Hour)
			if processErr.Error() == "handover_daily_limit" {
				until = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			}
			if processErr.Error() == "handover_interval" {
				until = time.Now().Add(6 * time.Hour)
			}
			if err := f.store.Defer(f.ctx, *j, processErr.Error(), until, true); err != nil {
				t.Fatal(err)
			}
			return "deferred:" + j.Stage
		}
		t.Fatal(processErr)
	}
	return j.Stage
}

type phase25B3WireDeadline struct {
	N          int        `json:"n"`
	Kind       string     `json:"kind"`
	At         *time.Time `json:"at"`
	Recurrence string     `json:"recurrence"`
	Title      string     `json:"title"`
	TimeNote   string     `json:"timeNote"`
}
type phase25B3WireRule struct {
	N         int    `json:"n"`
	AppliesTo string `json:"appliesTo"`
}
type phase25B3WireCard struct {
	Fields    map[string][]int        `json:"fields"`
	Rules     []phase25B3WireRule     `json:"rules,omitempty"`
	Deadlines []phase25B3WireDeadline `json:"deadlines,omitempty"`
}

func phase25B3All(input phase25B3Input) phase25B3WireCard {
	out := phase25B3WireCard{Fields: map[string][]int{"status": {}}}
	for _, m := range input.Memories {
		out.Fields["status"] = append(out.Fields["status"], m.N)
	}
	return out
}
func phase25B3N(input phase25B3Input, text string) int {
	for _, m := range input.Memories {
		if m.Text == text {
			return m.N
		}
	}
	return 0
}
func phase25B3JSON(out phase25B3WireCard) phase25B234ModelReply {
	data, err := json.Marshal(out)
	if err != nil {
		return phase25B234ModelReply{status: 500}
	}
	return phase25B234ModelReply{content: string(data)}
}

func (f *phase25B234Fixture) statusModel(t *testing.T, card func(phase25B3Input) phase25B3WireCard) *phase25B234Model {
	t.Helper()
	return f.model(t, func(_ *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		input, err := phase25B3CardInput(request)
		if err == nil {
			if card == nil {
				return phase25B3JSON(phase25B3All(input))
			}
			return phase25B3JSON(card(input))
		}
		text, e := phase25B3Prompt(request)
		if e != nil || !strings.Contains(text, `"cards"`) {
			return phase25B234ModelReply{status: 400}
		}
		out, e := phase25B3HandoverJSON(nil, nil)
		if e != nil {
			return phase25B234ModelReply{status: 500}
		}
		return phase25B234ModelReply{content: out}
	})
}

func (f *phase25B234Fixture) drainStatus(t *testing.T) int {
	t.Helper()
	return f.drainStatusJobs(t, 150)
}

// The budget counts every claimed job, including quota deferrals, rather than
// just model calls. Large-card fixtures must allow each remaining card to defer.
func (f *phase25B234Fixture) drainStatusJobs(t *testing.T, jobBudget int) int {
	t.Helper()
	n := 0
	for i := 0; i < jobBudget; i++ {
		stage := f.statusJob(t)
		if stage == "" {
			return n
		}
		if strings.HasPrefix(stage, "memory.card:") {
			n++
		}
		// Backends may serialize cards per owner. Poll the documented scheduler
		// after each actual completion instead of assuming all groups queue at once.
		f.scheduleStatus(t, f.statusNow)
	}
	t.Fatalf("status queue did not drain after %d jobs (%d completed card jobs)", jobBudget, n)
	return n
}

func (f *phase25B234Fixture) buildStatus(t *testing.T, clock time.Time) int {
	t.Helper()
	f.scheduleStatus(t, clock)
	return f.drainStatus(t)
}

func (f *phase25B234Fixture) usage(t *testing.T, purpose string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose=$2`, f.scope.OwnerID, purpose).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *phase25B234Fixture) groupTexts(t *testing.T, texts ...string) (workspace.MemoryGroup, []memory.Ref) {
	t.Helper()
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构白鹭月报")), Name: "虚构白鹭月报", Type: "project"}
	var refs []memory.Ref
	for _, text := range texts {
		r := f.claim(t, text)
		f.labels(t, r, "progress", true, 1, g)
		refs = append(refs, r)
	}
	return g, refs
}

func (f *phase25B234Fixture) readDeadlines(t *testing.T) []workspace.Deadline {
	t.Helper()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	d, err := f.store.DeadlinesTx(f.ctx, tx, f.scope, time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *phase25B234Fixture) timeZone(t *testing.T, zone string) {
	t.Helper()
	if _, err := f.store.Snapshot(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE workspace_owners SET settings=jsonb_set(settings,'{timezone}',to_jsonb($2::text)) WHERE owner_id=$1`, f.scope.OwnerID, zone)
}

func phase25B3Monday(t *testing.T) (time.Time, time.Time) {
	t.Helper()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(zone)
	days := (int(now.Weekday()) + 6) % 7
	mon := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, zone).AddDate(0, 0, -days)
	fri := mon.AddDate(0, 0, 4).Add(time.Hour)
	if !fri.After(now) {
		mon = mon.AddDate(0, 0, 7)
		fri = fri.AddDate(0, 0, 7)
	}
	return mon, fri
}

func TestPhase25B3_X3_2_OutsideBatchNumbersDiscarded(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 3)
	foreign := f.claim(t, "虚构外组哨兵：不可进入白鹭月报卡。")
	f.statusModel(t, func(input phase25B3Input) phase25B3WireCard {
		for _, m := range input.Memories {
			if strings.Contains(m.Text, "外组哨兵") {
				t.Error("outside-group memory fed to card model")
			}
		}
		return phase25B3WireCard{Fields: map[string][]int{"status": {1, 0, -1, 999}}}
	})
	if n := f.buildStatus(t, time.Now().Add(11*time.Minute)); n != 1 {
		t.Fatalf("built cards=%d", n)
	}
	items := phase25B3Items(f.readCards(t, "entity:"+g.EntityID))
	if len(items) != 1 {
		t.Errorf("valid numbered items=%d, want 1", len(items))
	}
	for _, m := range items {
		if m.ID == string(foreign.ID) {
			t.Error("outside-group item accepted")
		}
	}
	f.assertRevisions(t, append(refs, foreign)...)
}

func TestPhase25B3_X3_3_WeekdayDeadlineUsesUserTimezone(t *testing.T) {
	f := phase25B234NewFixture(t)
	f.timeZone(t, "Asia/Shanghai")
	text := "虚构汇报：周五上午十点前把汇报发给主管。"
	_, refs := f.groupTexts(t, text, "虚构汇报使用青色图表。", "虚构汇报先写结论。")
	mon, fri := phase25B3Monday(t)
	f.exec(t, `UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2 AND version=1`, f.scope.OwnerID, refs[0].ID, mon)
	f.statusModel(t, func(input phase25B3Input) phase25B3WireCard {
		if input.Timezone != "Asia/Shanghai" {
			t.Errorf("model timezone=%s", input.Timezone)
		}
		n := phase25B3N(input, text)
		if n == 0 {
			t.Error("deadline source absent")
		}
		out := phase25B3All(input)
		out.Deadlines = []phase25B3WireDeadline{{N: n, Kind: "deadline", At: &fri, Title: "虚构提交汇报"}}
		return out
	})
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	d := f.readDeadlines(t)
	if len(d) != 1 {
		t.Fatalf("derived deadlines=%+v", d)
	}
	if d[0].MemoryID != string(refs[0].ID) || d[0].At == nil {
		t.Fatalf("deadline source/time=%+v", d[0])
	}
	got, err := time.Parse(time.RFC3339, *d[0].At)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(fri) {
		t.Errorf("deadline=%s, want %s", got, fri)
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_X3_4_RecurringAndAmbiguousHour(t *testing.T) {
	f := phase25B234NewFixture(t)
	a, b := "虚构课表：每周二晚上有课。", "虚构课表：每周二 7 点有课。"
	_, refs := f.groupTexts(t, a, b, "虚构课表使用青色笔记本。")
	f.statusModel(t, func(input phase25B3Input) phase25B3WireCard {
		out := phase25B3All(input)
		out.Deadlines = []phase25B3WireDeadline{{N: phase25B3N(input, a), Kind: "recurring", Recurrence: "每周二晚上", Title: "虚构晚课"}, {N: phase25B3N(input, b), Kind: "recurring", Recurrence: "每周二 7 点", Title: "虚构待明确时刻的课", TimeNote: "没说上午还是下午"}}
		return out
	})
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	d := f.readDeadlines(t)
	if len(d) != 2 {
		t.Fatalf("recurring rows=%+v", d)
	}
	for _, row := range d {
		if row.Kind != "recurring" || row.At != nil || row.Recurrence == "" {
			t.Errorf("recurring shape=%+v", row)
		}
		if row.MemoryID == string(refs[0].ID) && row.TimeNote != "" {
			t.Errorf("evening fabricated ambiguity=%q", row.TimeNote)
		}
		if row.MemoryID == string(refs[1].ID) && !strings.Contains(row.TimeNote, "上午") {
			t.Errorf("missing ambiguity=%q", row.TimeNote)
		}
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_X3_5_UngroundedAndPastDatesDiscarded(t *testing.T) {
	f := phase25B234NewFixture(t)
	a, b := "虚构陆青喜欢青色图表。", "虚构旧事项：昨天上午十点前提交过月报。"
	_, refs := f.groupTexts(t, a, b, "虚构月报先写结论。")
	now := time.Now().UTC()
	fake := now.AddDate(0, 0, 17)
	past := now.Add(-24 * time.Hour)
	f.exec(t, `UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=ANY($2::uuid[])`, f.scope.OwnerID, []string{string(refs[0].ID), string(refs[1].ID)}, now)
	f.statusModel(t, func(input phase25B3Input) phase25B3WireCard {
		out := phase25B3All(input)
		out.Deadlines = []phase25B3WireDeadline{{N: phase25B3N(input, a), Kind: "deadline", At: &fake, Title: "虚构编造日期"}, {N: phase25B3N(input, b), Kind: "appointment", At: &past, Title: "虚构过期事项"}}
		return out
	})
	f.buildStatus(t, now.Add(11*time.Minute))
	if d := f.readDeadlines(t); len(d) != 0 {
		t.Errorf("invalid dates accepted: %+v", d)
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_X3_6_CorrectionDebounceAndActualRebuild(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 5)
	model := f.statusModel(t, nil)
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	key := "entity:" + g.EntityID
	refs[0] = f.correct(t, refs[0], "虚构修订后的月报：先放图表。")
	cards := f.readCards(t, key)
	phase25B234AssertIDs(t, phase25B3Items(cards), refs[1:]...)
	if len(cards) != 1 || !cards[0].Stale {
		t.Fatalf("corrected card not stale: %+v", cards)
	}
	var changed time.Time
	if err := f.db.QueryRow(f.ctx, `SELECT updated_at FROM memory_records WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, refs[0].ID).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	f.scheduleStatus(t, changed.Add(9*time.Minute))
	var due *time.Time
	if err := f.db.QueryRow(f.ctx, `SELECT min(available_at) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.card:%' AND state IN ('queued','leased')`, f.scope.OwnerID).Scan(&due); err != nil {
		t.Fatal(err)
	}
	if due != nil && due.Before(changed.Add(10*time.Minute)) {
		t.Errorf("card eligible before ten-minute debounce: changed=%s due=%s", changed, *due)
	}
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	cards = f.readCards(t, key)
	phase25B234AssertIDs(t, phase25B3Items(cards), refs...)
	if len(cards) != 1 || cards[0].Stale {
		t.Errorf("rebuilt card not fresh: %+v", cards)
	}
	if len(model.calls()) < 2 {
		t.Error("rebuild did not call model")
	}
	f.assertRevisions(t, refs...)
}

func TestPhase25B3_X3_7_RebuildAfterSupersession(t *testing.T) {
	f := phase25B234NewFixture(t)
	f.sharedSubject = true
	g, refs := f.cardGroup(t, 5)
	f.statusModel(t, nil)
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	old := refs[0]
	replacement := f.claim(t, "虚构月报改到周五交。")
	f.labels(t, replacement, "progress", true, 1, g)
	oldText := refsText(t, f, old)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, oldText), New: phase25B2N(in, "虚构月报改到周五交。")}}
		return out
	})
	f.runCompare(t)
	f.statusModel(t, nil)
	key := "entity:" + g.EntityID
	cards := f.readCards(t, key)
	phase25B234AssertIDs(t, phase25B3Items(cards), refs[1:]...)
	if len(cards) != 1 || !cards[0].Stale {
		t.Errorf("superseded card not stale")
	}
	f.buildStatus(t, time.Now().Add(11*time.Minute))
	phase25B234AssertIDs(t, phase25B3Items(f.readCards(t, key)), append(refs[1:], replacement)...)
	f.assertRevisions(t, append(refs, replacement)...)
}

func TestPhase25B3_X3_1_BuildOriginalProjectCard(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 12)
	model := f.model(t, func(_ *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		input, err := phase25B3CardInput(request)
		if err != nil {
			return phase25B234ModelReply{status: 400}
		}
		if len(input.Memories) != 12 {
			t.Errorf("card input=%d, want 12", len(input.Memories))
		}
		return phase25B234ModelReply{content: `{"fields":{"status":[1,2,3,4,5,6,7,8,9,10,11,12]}}`}
	})
	n := f.scheduleStatus(t, time.Now().Add(11*time.Minute))
	t.Logf("scheduled=%d", n)
	if n != 1 {
		t.Fatalf("scheduled=%d, want one project card", n)
	}
	stage := f.statusJob(t)
	if !strings.HasPrefix(stage, "memory.card:1:entity:"+g.EntityID) {
		t.Fatalf("card stage=%s", stage)
	}
	cards := f.readCards(t, "entity:"+g.EntityID)
	phase25B234AssertIDs(t, phase25B3Items(cards), refs...)
	if len(model.calls()) != 1 {
		t.Errorf("calls=%d, want 1", len(model.calls()))
	}
	f.assertRevisions(t, refs...)
	var a workspace.About
	f.get(t, "/v1/workspace/about", &a)
	if a.Building.Done != 1 || a.Building.Total != 1 {
		t.Errorf("building=%+v", a.Building)
	}
}
