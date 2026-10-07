package postgres

import (
	"context"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"testing"
	"time"
)

func p3RejudgeJob(t *testing.T, s *Store) worker.Job {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), "DELETE FROM memory_jobs WHERE stage NOT LIKE '%:rejudge:%'"); e != nil {
		t.Fatal(e)
	}
	j, e := s.Claim(context.Background(), 5*time.Minute)
	if e != nil || j == nil {
		t.Fatal(j, e)
	}
	return *j
}
func TestP3RejudgeOnlySupersededAndReusesPaidWrite(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	fake := b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	refs := compareFixture(t, s, scope, "虚构期限周三", "虚构期限周五", "虚构重复说明", "虚构新的说明")
	for _, i := range []int{0, 2} {
		kind := "superseded"
		if i == 2 {
			kind = "duplicate"
		}
		if _, e := s.pool.Exec(ctx, "UPDATE claims SET retired=$3,retired_by=$4 WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[i].ID, kind, refs[i+1].ID); e != nil {
			t.Fatal(e)
		}
	}
	n, e := s.QueueSupersededRejudge(ctx, scope)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if n, e = s.QueueSupersededRejudge(ctx, scope); e != nil || n != 0 {
		t.Fatal("duplicate queue", n, e)
	}
	job := p3RejudgeJob(t, s)
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION fictional_rejudge_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fictional write failure'; END $$;
 CREATE TRIGGER fictional_rejudge_failure BEFORE INSERT ON background_markers FOR EACH ROW EXECUTE FUNCTION fictional_rejudge_failure()`); e != nil {
		t.Fatal(e)
	}
	if e = s.ProcessCompare(ctx, job); e == nil {
		t.Fatal("expected write failure")
	}
	var retired string
	if e = s.pool.QueryRow(ctx, "SELECT retired FROM claims WHERE id=$1", refs[0].ID).Scan(&retired); e != nil || retired != "superseded" {
		t.Fatal("failure changed result", retired, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER fictional_rejudge_failure ON background_markers"); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(ctx, s.pool.Config().ConnString())
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if e = reopened.ProcessCompare(ctx, job); e != nil {
		t.Fatal(e)
	}
	if len(fake.all()) != 1 {
		t.Fatal("paid call repeated", len(fake.all()))
	}
	if e = s.pool.QueryRow(ctx, "SELECT retired FROM claims WHERE id=$1", refs[0].ID).Scan(&retired); e != nil || retired != "" {
		t.Fatal(retired, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT retired FROM claims WHERE id=$1", refs[2].ID).Scan(&retired); e != nil || retired != "duplicate" {
		t.Fatal("touched duplicate", retired, e)
	}
	if n, e = s.QueueSupersededRejudge(ctx, scope); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}
func TestP3RejudgeRejectsFailureAndChangedPair(t *testing.T) {
	for _, scenario := range []string{"invalid", "restored", "target_changed", "kept"} {
		t.Run(scenario, func(t *testing.T) {
			s, scope, ctx := testStore(t), owner(), context.Background()
			fake := b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
			refs := compareFixture(t, s, scope, "虚构旧要求", "虚构新要求")
			if _, e := s.pool.Exec(ctx, "UPDATE claims SET retired='superseded',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[0].ID, refs[1].ID); e != nil {
				t.Fatal(e)
			}
			response := `{"duplicates":[],"superseded":[{"old":1,"new":2}]}`
			if scenario == "invalid" {
				response = `{}`
			}
			fake.set(response)
			if n, e := s.QueueSupersededRejudge(ctx, scope); e != nil || n != 1 {
				t.Fatal(n, e)
			}
			job := p3RejudgeJob(t, s)
			if scenario == "restored" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "restoreMemory", ID: string(refs[0].ID)})
			}
			if scenario == "target_changed" {
				b1Correct(t, s, scope, refs[1], "虚构本人改过的新要求")
			}
			err := s.ProcessCompare(ctx, job)
			if scenario == "invalid" {
				if err == nil {
					t.Fatal("accepted invalid output")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var retired string
			if e := s.pool.QueryRow(ctx, "SELECT retired FROM claims WHERE id=$1", refs[0].ID).Scan(&retired); e != nil {
				t.Fatal(e)
			}
			want := "superseded"
			if scenario == "restored" {
				want = ""
			}
			if retired != want {
				t.Fatal(retired, want)
			}
			if (scenario == "restored" || scenario == "target_changed") && len(fake.all()) != 0 {
				t.Fatal("called for changed pair")
			}
		})
	}
}
func TestP3UpgradeDoesNotRecompareUnchangedBatches(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	compareFixture(t, s, scope, "虚构云朵蓝色", "虚构窗帘绿色")
	compareProcess(t, s, scope)
	if _, e := s.pool.Exec(ctx, "UPDATE memory_comparison_batches SET rule=1; UPDATE memory_group_progress SET rule=1"); e != nil {
		t.Fatal(e)
	}
	if n, e := s.ScheduleCompare(ctx, time.Now()); e != nil || n != 0 {
		t.Fatal("library-wide upgrade", n, e)
	}
	compareFixture(t, s, scope, "虚构后来新增资料")
	if n, e := s.ScheduleCompare(ctx, time.Now()); e != nil || n != 1 {
		t.Fatal("new memory did not compare", n, e)
	}
	// Previous rule restoration, including legacy spelling, is visible to v2.
	refs := compareFixture(t, s, scope, "虚构手动保留资料")
	if _, e := s.pool.Exec(ctx, `INSERT INTO background_markers(owner_id,record_id,record_version,stage) VALUES($1,$2,1,$3)`, scope.OwnerID, refs[0].ID, fmt.Sprintf("memory.compare_restored:1:fictional")); e != nil {
		t.Fatal(e)
	}
	batch, e := s.nextComparisonBatch(ctx, scope.OwnerID, CompareVersion)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, m := range batch.Memories {
		if m.Ref.ID == refs[0].ID {
			found = m.Protected
		}
	}
	if !found {
		t.Fatal("lost previous-rule user protection")
	}
}

func TestP3RejudgeTwelveFixedPairs(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	fake := b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	expected := map[string]string{}
	for _, c := range p3SupersessionCases {
		refs := compareFixture(t, s, scope, c.Old, c.New)
		if _, e := s.pool.Exec(ctx, "UPDATE claims SET retired='superseded',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[0].ID, refs[1].ID); e != nil {
			t.Fatal(e)
		}
		want := ""
		if c.Superseded {
			want = "superseded"
		}
		expected[string(refs[0].ID)] = want
	}
	if n, e := s.QueueSupersededRejudge(ctx, scope); e != nil || n != 12 {
		t.Fatal(n, e)
	}
	for range 12 {
		job := p3RejudgeJob(t, s)
		response := `{"duplicates":[],"superseded":[]}`
		if expected[string(job.Record.ID)] == "superseded" {
			response = `{"duplicates":[],"superseded":[{"old":1,"new":2}]}`
		}
		fake.set(response)
		if e := s.ProcessCompare(ctx, job); e != nil {
			t.Fatal(e)
		}
	}
	for id, want := range expected {
		var got string
		if e := s.pool.QueryRow(ctx, "SELECT retired FROM claims WHERE id=$1", id).Scan(&got); e != nil || got != want {
			t.Fatal(id, got, want, e)
		}
	}
	if len(fake.all()) != 12 {
		t.Fatal("wrong call count", len(fake.all()))
	}
	if n, e := s.QueueSupersededRejudge(ctx, scope); e != nil || n != 0 {
		t.Fatal("repeated completed pair", n, e)
	}
}

func TestP3RejudgeMalformedProposalIsNotCoexistence(t *testing.T) {
	for _, raw := range []string{`{}`, `{"duplicates":[],"superseded":[{"old":1,"new":999}]}`, `{"duplicates":[],"superseded":[{"old":2,"new":1}]}`, `{"duplicates":[{"keep":2,"members":[1,999]}],"superseded":[]}`} {
		if _, valid := parseRejudgeOutput(raw); valid {
			t.Fatal("accepted malformed judgment", raw)
		}
	}
}
