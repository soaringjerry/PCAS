package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b2Worker(s *Store) *worker.Worker {
	return worker.New(s, map[string]worker.Handler{"source.extract": s.ProcessExtraction}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func b2OnlyExtraction(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, priority int) {
	t.Helper()
	b2Exec(t, s, `DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'source.extract%'`, string(scope.OwnerID))
	b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority) VALUES(gen_random_uuid(),$1,$2,$3,'source.extract',$4) ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET state='queued',attempts=0,priority=$4,lease_token=NULL,lease_until=NULL,available_at=now()`, string(scope.OwnerID), string(ref.ID), ref.Version, priority)
}

type b2JobState struct {
	State, Code string
	Attempts    int
	Available   time.Time
}

func b2Job(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref) b2JobState {
	t.Helper()
	var out b2JobState
	if err := s.pool.QueryRow(context.Background(), `SELECT state,error_code,attempts,available_at FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND stage='source.extract'`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&out.State, &out.Code, &out.Attempts, &out.Available); err != nil {
		t.Fatal(err)
	}
	return out
}
func b2RunOnce(t *testing.T, w *worker.Worker, want bool) {
	t.Helper()
	worked, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b2Equal(t, worked, want)
}
func b2Ready(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref) {
	t.Helper()
	b2Exec(t, s, `UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, string(scope.OwnerID), string(ref.ID))
}
func b2RetryInterval(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, start time.Time, attempt int) {
	t.Helper()
	end := time.Now()
	state := b2Job(t, s, scope, ref)
	seconds := b2Want[[]int](t, "F2", "retry_seconds")[attempt-1]
	expected := time.Duration(seconds) * time.Second
	if state.Available.Before(start.Add(expected)) || state.Available.After(end.Add(expected)) {
		t.Errorf("attempt %d available_at=%s outside expected %s..%s", attempt, state.Available, start.Add(expected), end.Add(expected))
	}
	b2Equal(t, state.State, "queued")
	b2Equal(t, state.Attempts, attempt)
}
func TestPhase2B2_F1_NewJobsPrecedeBackfill(t *testing.T) {
	s, scope := testStore(t), owner()
	old := b2Source(t, s, scope, "desk", "user", "我过去去成都", nil)
	b2OnlyExtraction(t, s, scope, old, b2Want[int](t, "F1", "backfill_priority"))
	fresh := b2Source(t, s, scope, "desk", "user", "我今天去大理", nil)
	b2OnlyExtraction(t, s, scope, fresh, b2Want[int](t, "F1", "new_priority"))
	job, err := s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil {
		t.Fatal("new claim", err)
	}
	b2Equal(t, job.Record.ID, fresh.ID)
	if err = s.Block(context.Background(), *job, "synthetic_test"); err != nil {
		t.Fatal(err)
	}
	job, err = s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil {
		t.Fatal("old claim", err)
	}
	b2Equal(t, job.Record.ID, old.ID)
}
func TestPhase2B2_F2_SubscriptionRetriesUnavailableAndStopsAtFive(t *testing.T) {
	t.Run("invalid_unavailable_success", func(t *testing.T) {
		s, scope := testStore(t), owner()
		text := "我去成都见老王"
		src := b2Source(t, s, scope, "desk", "user", text, nil)
		b2OnlyExtraction(t, s, scope, src, 0)
		f := b1Model(t, s, "不是 JSON")
		w := b2Worker(s)
		start := time.Now()
		b2RunOnce(t, w, true)
		b2RetryInterval(t, s, scope, src, start, 1)
		b2Equal(t, b2Job(t, s, scope, src).Code, "model_output_invalid")
		b2Ready(t, s, scope, src)
		// Configuration remains present; the provider reports pre-dispatch unavailability.
		goodRegistry := s.models
		goodRegistry.Config.Providers[0].KeyEnv = "PCAS_B2_UNAVAILABLE_KEY"
		t.Setenv("PCAS_B2_UNAVAILABLE_KEY", "")
		start = time.Now()
		b2RunOnce(t, w, true)
		b2RetryInterval(t, s, scope, src, start, 2)
		b2Equal(t, b2Job(t, s, scope, src).Code, b2Want[string](t, "F2", "error"))
		b2Ready(t, s, scope, src)
		t.Setenv("PCAS_B2_UNAVAILABLE_KEY", "synthetic-local-key")
		s.SetModels(goodRegistry)
		f.set(map[string]any{"items": []any{b2Item(text, text, "fact")}})
		b2RunOnce(t, w, true)
		b2Equal(t, b2Job(t, s, scope, src).State, b2Want[string](t, "F2", "first_case"))
		b2Equal(t, len(b2Snapshot(t, s, scope).Memories), 1)
	})
	t.Run("unavailable_five_then_manual_retry", func(t *testing.T) {
		s, scope := testStore(t), owner()
		text := "我喜欢成都"
		src := b2Source(t, s, scope, "desk", "user", text, nil)
		b2OnlyExtraction(t, s, scope, src, 0)
		f := b1Model(t, s, nil)
		goodRegistry := s.models
		goodRegistry.Config.Providers[0].KeyEnv = "PCAS_B2_UNAVAILABLE_KEY"
		t.Setenv("PCAS_B2_UNAVAILABLE_KEY", "")
		w := b2Worker(s)
		for attempt := 1; attempt <= b2Want[int](t, "F2", "stop_after"); attempt++ {
			start := time.Now()
			b2RunOnce(t, w, true)
			if attempt < 5 {
				b2RetryInterval(t, s, scope, src, start, attempt)
				b2Ready(t, s, scope, src)
			} else {
				state := b2Job(t, s, scope, src).State
				if state != "blocked" && state != "failed" {
					t.Errorf("fifth unavailable attempt did not stop: %s", state)
				}
				b2Equal(t, b2Job(t, s, scope, src).Code, b2Want[string](t, "F2", "error"))
				b2ExtractionRecord(t, s, scope, src, "failed", 0)
			}
		}
		b2RunOnce(t, w, false)
		t.Setenv("PCAS_B2_UNAVAILABLE_KEY", "synthetic-local-key")
		s.SetModels(goodRegistry)
		f.set(map[string]any{"items": []any{b2Item(text, text, "preference")}})
		jobID := b2SQLIDs(t, s, `SELECT id::text FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, string(scope.OwnerID), string(src.ID))[0]
		workspaceCommand(t, s, scope, workspace.Command{Type: "retryJob", ID: jobID})
		b2RunOnce(t, w, true)
		b2Equal(t, b2Job(t, s, scope, src).State, "done")
	})
}
func TestPhase2B2_F3_NotConfiguredStopsAndManualRetrySucceeds(t *testing.T) {
	s, scope := testStore(t), owner()
	s.SetModels(&ai.Registry{Config: ai.Configuration{}})
	text := "我喜欢清晨去成都"
	src := b2Source(t, s, scope, "desk", "user", text, nil)
	b2OnlyExtraction(t, s, scope, src, 0)
	w := b2Worker(s)
	b2RunOnce(t, w, true)
	st := b2Job(t, s, scope, src)
	b2Equal(t, st.State, b2Want[string](t, "F3", "state"))
	b2Equal(t, st.Code, b2Want[string](t, "F3", "error"))
	b2RunOnce(t, w, false)
	b1Model(t, s, map[string]any{"items": []any{b2Item(text, text, "preference")}})
	id := b2SQLIDs(t, s, `SELECT id::text FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, string(scope.OwnerID), string(src.ID))[0]
	workspaceCommand(t, s, scope, workspace.Command{Type: "retryJob", ID: id})
	b2RunOnce(t, w, true)
	b2Equal(t, b2Job(t, s, scope, src).State, b2Want[string](t, "F3", "manual_retry"))
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), 1)
}
func TestPhase2B2_F4_BudgetDefersWithoutAttemptAndCalendarHandlesDST(t *testing.T) {
	for _, zone := range b2Want[[]string](t, "F4", "zones") {
		t.Run(zone, func(t *testing.T) {
			s, scope := testStore(t), owner()
			b2Zone(t, s, scope, zone)
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": .01})})
			var calls atomic.Int32
			text := "我喜欢成都"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				secretaryModelReply(w, map[string]any{"items": []any{b2Item(text, text, "preference")}})
			}))
			t.Cleanup(server.Close)
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 1}}}})
			src := b2Source(t, s, scope, "desk", "user", text, nil)
			b2OnlyExtraction(t, s, scope, src, 0)
			// A positive daily budget is already fully consumed before extraction.
			// Only the synthetic usage fixture is advanced to yesterday for recovery.
			b2Exec(t, s, `INSERT INTO background_usage(owner_id,job_id,reserved_cost) VALUES($1,NULL,0.01)`, string(scope.OwnerID))
			start := time.Now()
			b2RunOnce(t, b2Worker(s), true)
			state := b2Job(t, s, scope, src)
			b2Equal(t, state.State, b2Want[string](t, "F4", "state"))
			b2Equal(t, state.Code, b2Want[string](t, "F4", "error"))
			b2Equal(t, state.Attempts, b2Want[int](t, "F4", "attempts"))
			b2Equal(t, calls.Load(), int32(0))
			loc, _ := time.LoadLocation(zone)
			d := start.In(loc).AddDate(0, 0, 1)
			expected := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
			if state.Available.Before(expected) || state.Available.After(expected.Add(10*time.Minute)) {
				t.Errorf("budget defer=%s want local midnight %s with <=10m jitter", state.Available, expected)
			}
			b2Equal(t, nextBudgetDay(start, loc), expected)
			b2Exec(t, s, `UPDATE background_usage SET created_at=now()-interval '2 days' WHERE owner_id=$1`, string(scope.OwnerID))
			b2Ready(t, s, scope, src)
			b2RunOnce(t, b2Worker(s), true)
			b2Equal(t, b2Job(t, s, scope, src).State, "done")
			b2Equal(t, calls.Load(), int32(1))
		})
	}
	t.Run("sydney_dst_25_hour_day", func(t *testing.T) {
		var spec struct {
			Zone, Now, Midnight string
			Hours               int `json:"day_hours"`
		}
		if err := jsonUnmarshalOracle(t, "F4", "dst", &spec); err != nil {
			t.Fatal(err)
		}
		loc, err := time.LoadLocation(spec.Zone)
		if err != nil {
			t.Fatal(err)
		}
		now := b2ParseTime(t, spec.Now)
		want := b2ParseTime(t, spec.Midnight)
		got := nextBudgetDay(now, loc)
		if !got.Equal(want) {
			t.Errorf("DST midnight %s want %s", got, want)
		}
		start := time.Date(2025, 4, 6, 0, 0, 0, 0, loc)
		b2Equal(t, want.Sub(start), time.Duration(spec.Hours)*time.Hour)
	})
}
func TestPhase2B2_F5_ConcurrentBackfillCapAndHourlyRecovery(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, map[string]any{"items": []any{}})
	var spec struct {
		MaxPending     int   `json:"max_pending_sources"`
		MaxHourly      int   `json:"max_enqueued_sources_hour"`
		ArchiveRoots   int   `json:"archive_queued_roots"`
		LongCharacters int   `json:"long_source_characters"`
		Enqueued       []int `json:"first_three_enqueued_sources"`
		NextHour       int   `json:"after_hour_enqueued_sources"`
	}
	b2SupplementFrom(t, "supplement_2e2b8f9", "F5", &spec)
	eligible := []memory.Ref{}
	for n := 0; n < b2Want[int](t, "F5", "sources"); n++ {
		text := b2Label(n)
		if n == b2Want[int](t, "F5", "sources")-1 {
			text = strings.Repeat("长", spec.LongCharacters)
		}
		src := b2Source(t, s, scope, "desk", "user", text, nil)
		eligible = append(eligible, src)
		at := b2Anchor(t, "Asia/Shanghai").Add(time.Duration(n) * time.Minute)
		b2Exec(t, s, `UPDATE record_versions SET recorded_at=$1 WHERE owner_id=$2 AND record_id=$3`, at, string(scope.OwnerID), string(src.ID))
		b2Exec(t, s, `UPDATE memory_records SET created_at=$1,updated_at=$1 WHERE owner_id=$2 AND id=$3`, at, string(scope.OwnerID), string(src.ID))
	}
	system := b2Source(t, s, scope, "actions", "user", "改到四点", nil)
	extraSystems := []memory.Ref{b2Source(t, s, scope, "corrections", "user", "用户纠正记录", nil), b2Source(t, s, scope, "memory-input", "user", "用户记忆记录", nil)}
	processed := b2Source(t, s, scope, "desk", "user", "我已处理过", nil)
	b2Exec(t, s, `INSERT INTO source_extractions(owner_id,source_id,source_version,extractor,state,items) VALUES($1,$2,$3,3,'empty',0)`, string(scope.OwnerID), string(processed.ID), processed.Version)
	b2Exec(t, s, `DELETE FROM memory_jobs WHERE owner_id=$1`, string(scope.OwnerID))
	b2Exec(t, s, `INSERT INTO source_extractions(owner_id,source_id,source_version,extractor,state,items) VALUES($1,$2,1,1,'empty',0)`, string(scope.OwnerID), string(eligible[0].ID))
	// Already queued archive extractions share priority 10, but consume no
	// backfill quota. Keep more than twenty to expose priority-only counting.
	archives := []memory.Ref{}
	for n := 0; n < spec.ArchiveRoots; n++ {
		archives = append(archives, b2Source(t, s, scope, "archive", "user", "合成归档原话"+b2Label(n), nil))
	}
	b2Exec(t, s, `DELETE FROM memory_jobs WHERE owner_id=$1`, string(scope.OwnerID))
	for _, ref := range archives {
		b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority) VALUES(gen_random_uuid(),$1,$2,$3,'source.extract',10)`, string(scope.OwnerID), string(ref.ID), ref.Version)
	}
	peer, err := Open(context.Background(), s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	now := time.Now()
	hourTotal := 0
	for check := 0; check < b2Want[int](t, "F5", "checks"); check++ {
		start := make(chan struct{})
		errs := make(chan error, 2)
		counts := make(chan int, 2)
		var wg sync.WaitGroup
		for _, store := range []*Store{s, peer} {
			wg.Add(1)
			go func(store *Store) {
				defer wg.Done()
				<-start
				n, err := store.BackfillExtractions(context.Background(), now)
				counts <- n
				errs <- err
			}(store)
		}
		close(start)
		wg.Wait()
		close(errs)
		close(counts)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		added := 0
		for n := range counts {
			added += n
		}
		hourTotal += added
		b2Equal(t, added, spec.Enqueued[check])
		pending := b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND backfill_queued_at IS NOT NULL AND stage='source.extract' AND state IN ('queued','leased')`, string(scope.OwnerID))
		if pending > spec.MaxPending {
			t.Error("pending cap exceeded", pending)
		}
		if hourTotal > spec.MaxHourly {
			t.Error("hourly enqueue cap exceeded", hourTotal)
		}
		b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage='source.extract' AND backfill_queued_at>$2::timestamptz-interval '1 hour' AND backfill_queued_at<=$2::timestamptz`, string(scope.OwnerID), now), hourTotal)
		if check == 0 {
			b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage='source.extract' AND priority=10 AND backfill_queued_at IS NULL AND state='queued'`, string(scope.OwnerID)), spec.ArchiveRoots)
			b2Equal(t, hourTotal, 20)
			want := []string{}
			for _, r := range eligible[30:] {
				want = append(want, string(r.ID))
			}
			sort.Strings(want)
			b2Equal(t, b2SQLIDs(t, s, `SELECT record_id::text FROM memory_jobs WHERE owner_id=$1 AND backfill_queued_at IS NOT NULL AND stage='source.extract'`, string(scope.OwnerID)), want)
		}
		if check == 1 {
			b2Equal(t, hourTotal, 30)
		}
		for _, r := range extraSystems {
			b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2`, string(scope.OwnerID), string(r.ID)), 0)
		}
		// Drain roots and all generated segment jobs, including the long source.
		w := b2Worker(s)
		for step := 0; ; step++ {
			if step > len(eligible)*4+len(archives) {
				t.Fatal("extraction queue did not drain within the fixture bound")
			}
			worked, err := w.RunOnce(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				break
			}
		}
		b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND backfill_queued_at IS NOT NULL AND stage='source.extract' AND state IN ('queued','leased')`, string(scope.OwnerID)), 0)
		if check == 0 {
			long := eligible[len(eligible)-1]
			b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'source.extract:%'`, string(scope.OwnerID), string(long.ID)), 2)
		}
		b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id IN ($2,$3)`, string(scope.OwnerID), string(system.ID), string(processed.ID)), 0)
		b2Equal(t, b2Count(t, s, `SELECT count(*) FROM (SELECT record_id,record_version,stage FROM memory_jobs WHERE owner_id=$1 GROUP BY record_id,record_version,stage HAVING count(*)>1) d`, string(scope.OwnerID)), 0)
	}
	for _, ref := range archives {
		b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract' AND backfill_queued_at IS NOT NULL`, string(scope.OwnerID), string(ref.ID)), 0)
	}
	n, err := s.BackfillExtractions(context.Background(), now.Add(time.Hour+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	b2Equal(t, n, spec.NextHour)
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage='source.extract' AND backfill_queued_at>$2::timestamptz-interval '1 hour' AND backfill_queued_at<=$2::timestamptz`, string(scope.OwnerID), now.Add(time.Hour+time.Second)), spec.NextHour)
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND backfill_queued_at IS NOT NULL AND stage='source.extract'`, string(scope.OwnerID)), len(eligible))
}
func TestPhase2B2_F6_PausedArchiveSkippedOrdinaryProcessedResumeWorks(t *testing.T) {
	s, scope := testStore(t), owner()
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	at := b2Anchor(t, "Asia/Shanghai")
	root, src, _ := b2Archive(t, s, scope, at, b2Fixture(t, "archive_user", ""), "")
	b2Equal(t, b2Count(t, s, `SELECT priority FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, string(scope.OwnerID), string(src.ID)), b2Want[int](t, "F6", "archive_priority"))
	batch := string(memory.NewID())
	b2Exec(t, s, `INSERT INTO import_batches(owner_id,id,archive_id,name,state,total,stored) VALUES($1,$2,$3,'合成暂停批次','paused',1,1)`, string(scope.OwnerID), batch, string(root.ID))
	ordinary := b2Source(t, s, scope, "desk", "user", "我去大理", nil)
	b2Exec(t, s, `DELETE FROM memory_jobs WHERE owner_id=$1 AND (stage<>'source.extract' OR record_id NOT IN ($2,$3))`, string(scope.OwnerID), string(src.ID), string(ordinary.ID))
	job, err := s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	b2Equal(t, job.Record.ID, ordinary.ID)
	if err = s.Block(context.Background(), *job, "synthetic_finished"); err != nil {
		t.Fatal(err)
	}
	job, err = s.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatal("paused job leased", job.Record)
	}
	b2Exec(t, s, `UPDATE import_batches SET state='importing' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), batch)
	job, err = s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil {
		t.Fatal("resume claim", err)
	}
	b2Equal(t, job.Record.ID, src.ID)
}

// The child uses the real queue/HTTP extractor. The parent kills only this
// recorded process, leaving its leased job for another independent Store.
func TestPhase2B2WorkerChild(t *testing.T) {
	if os.Getenv("PCAS_B2_WORKER_CHILD") != "1" {
		return
	}
	s, err := Open(context.Background(), os.Getenv("PCAS_B2_WORKER_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetModels(&ai.Registry{HTTP: &http.Client{Timeout: 30 * time.Second}, Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "本地假抽取", Protocol: "openai", BaseURL: os.Getenv("PCAS_B2_WORKER_URL"), Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	job, err := s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil {
		t.Fatal("child failed to claim", err)
	}
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("PCAS_B2_WORKER_JOB"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessExtraction(context.Background(), *job); err != nil {
		t.Fatal(err)
	}
}
func TestPhase2B2_F7_KilledWorkerRecoversLeaseAndFencesOldCommit(t *testing.T) {
	s, scope := testStore(t), owner()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	text := "我下周去成都见老王"
	item := b2Item(text, text, "plan")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		secretaryModelReply(w, map[string]any{"items": []any{item}})
	}))
	t.Cleanup(server.Close)
	t.Cleanup(unblock)
	src := b2Source(t, s, scope, "desk", "user", text, nil)
	b2OnlyExtraction(t, s, scope, src, 0)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(t.TempDir(), "owned-job.json")
	child := exec.Command(executable, "-test.run=^TestPhase2B2WorkerChild$", "-test.v")
	child.Env = append(os.Environ(), "PCAS_B2_WORKER_CHILD=1", "PCAS_B2_WORKER_DSN="+s.pool.Config().ConnConfig.ConnString(), "PCAS_B2_WORKER_URL="+server.URL, "PCAS_B2_WORKER_JOB="+jobPath)
	var output bytes.Buffer
	child.Stdout = &output
	child.Stderr = &output
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("owned worker did not send actual model request")
	}
	data, err := os.ReadFile(jobPath)
	if err != nil {
		t.Fatal(err)
	}
	var old worker.Job
	if err = json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	waited = true
	if err == nil {
		t.Fatal("worker was not interrupted")
	}
	unblock()
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), 0)
	b2Exec(t, s, `UPDATE memory_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, string(old.ID))
	replacement, err := s.Claim(context.Background(), time.Minute)
	if err != nil || replacement == nil {
		t.Fatal("recovery claim", err)
	}
	b2Equal(t, replacement.ID, old.ID)
	b2Equal(t, replacement.Attempts, b2Want[int](t, "F7", "reclaimed_attempts"))
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	if err = s.ProcessExtraction(context.Background(), *replacement); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), b2Want[int](t, "F7", "memories"))
	if err = s.ProcessExtraction(context.Background(), old); !errors.Is(err, worker.ErrLeaseLost) {
		t.Errorf("killed worker's token must be fenced: %v", err)
	}
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), 1)
	b2Equal(t, calls.Load(), int32(2))
}
func TestPhase2B2_F8_MeteredCallFailureDoesNotRetry(t *testing.T) {
	s, scope := testStore(t), owner()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 1}}}})
	src := b2Source(t, s, scope, "desk", "user", "我去成都", nil)
	b2OnlyExtraction(t, s, scope, src, 0)
	b2RunOnce(t, b2Worker(s), true)
	st := b2Job(t, s, scope, src)
	b2Equal(t, st.State, b2Want[string](t, "F8", "state"))
	b2Equal(t, st.Code, b2Want[string](t, "F8", "error"))
	b2ExtractionRecord(t, s, scope, src, "failed", 0)
	b2RunOnce(t, b2Worker(s), false)
	b2Equal(t, calls.Load(), int32(b2Want[int](t, "F8", "automatic_calls")))
}
