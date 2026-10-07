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
		// Cards about the user come before comparison (7); the rest share its
		// priority (8) and alternate with it.
		want := 8
		if strings.HasPrefix(j.Stage, "memory.card:") && strings.Contains(j.Stage, ":self:") {
			want = 7
		}
		if priority != want {
			t.Errorf("card priority=%d, want %d", priority, want)
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
