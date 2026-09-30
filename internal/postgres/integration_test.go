package postgres

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Each test owns a unique schema. Run only against a disposable test database;
// the helper never truncates an existing schema or touches application data.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PCAS_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	var encoding string
	if err := admin.pool.QueryRow(ctx, "SHOW server_encoding").Scan(&encoding); err != nil || encoding != "UTF8" {
		t.Fatalf("integration tests require PostgreSQL UTF8, got %q (%v)", encoding, err)
	}
	if _, err := admin.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	schema := "pcas_test_" + strings.ReplaceAll(string(memory.NewID()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	store, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

func owner() memory.Scope {
	return memory.Scope{OwnerID: memory.NewID(), PrincipalID: "owner", IsOwner: true}
}
func input() memory.IngestRequest {
	return memory.IngestRequest{Connector: "manual", ExternalID: "conversation-1", ExternalVersion: "v1", Title: "旧书店", Text: strings.Repeat("以后想去那家河边的旧书店📚。", 220), MediaType: "text/plain"}
}
func mustIngest(t *testing.T, s *Store, scope memory.Scope, in memory.IngestRequest) memory.IngestResult {
	t.Helper()
	result, err := memory.NewService(s).Ingest(context.Background(), scope, in)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPostgresMigrations(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("migration is not repeatable:", err)
	}
	if err := s.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE schema_migrations SET checksum='modified'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("modified migration went unnoticed")
	}
}

func TestPostgresConcurrentIngestAndVersions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	in := input()
	const writers = 12
	results := make(chan memory.IngestResult, writers)
	failures := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() { result, err := memory.NewService(s).Ingest(ctx, scope, in); results <- result; failures <- err })
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	created := 0
	var id memory.ID
	for result := range results {
		if id != "" && id != result.ID {
			t.Fatal("duplicate source identity")
		}
		id = result.ID
		if !result.Duplicate {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created %d sources", created)
	}
	for _, table := range []string{"sources", "source_versions", "memory_records", "record_versions", "memory_jobs"} {
		var count int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	result, err := s.GetSource(ctx, scope, id, 0)
	if err != nil || result.Source.Text != in.Text || len(result.Processing) != 1 {
		t.Fatalf("read-before-worker failed: %+v %v", result, err)
	}
	changed := in
	changed.Text = "same source version, different body"
	if _, err := s.Ingest(ctx, scope, changed); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("want immutable version conflict, got %v", err)
	}
	changed.ExternalVersion = "v2"
	oldExpression := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	changed.ExpressedAt = &oldExpression
	v2 := mustIngest(t, s, scope, changed)
	if v2.ID != id || v2.Version != 2 {
		t.Fatal("source identity was not preserved")
	}
	first, err := s.GetSource(ctx, scope, id, 1)
	if err != nil || first.Source.Text != in.Text {
		t.Fatal("old version lost", err)
	}
	latest, err := s.GetSource(ctx, scope, id, 0)
	if err != nil || latest.Source.Version != 2 || !latest.Source.ExpressedAt.Equal(oldExpression) {
		t.Fatal("expression time was replaced by import time", err)
	}
	duplicate := mustIngest(t, s, scope, in)
	if duplicate.Version != 1 || !duplicate.Duplicate {
		t.Fatal("old version reimport changed current identity")
	}
}

func TestPostgresScopeAndReimportBlock(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := owner(), owner()
	in := input()
	x := mustIngest(t, s, a, in)
	y := mustIngest(t, s, b, in)
	if x.ID == y.ID {
		t.Fatal("owners share identity")
	}
	if _, err := s.GetSource(ctx, b, x.ID, 0); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("cross-owner read", err)
	}
	agent := memory.Scope{OwnerID: a.OwnerID, PrincipalID: "agent:one"}
	if _, err := s.GetSource(ctx, agent, x.ID, 0); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("ungranted read", err)
	}
	if _, err := s.pool.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3)", string(a.OwnerID), string(x.ID), agent.PrincipalID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSource(ctx, agent, x.ID, 0); err != nil {
		t.Fatal("grant did not permit read", err)
	}
	if _, err := s.Ingest(ctx, agent, in); !errors.Is(err, memory.ErrForbidden) {
		t.Fatal("read grant permitted write", err)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM record_grants WHERE owner_id=$1", string(a.OwnerID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSource(ctx, agent, x.ID, 0); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("grant revocation ineffective", err)
	}
	if _, err := s.pool.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,'agent')", string(b.OwnerID), string(x.ID)); err == nil {
		t.Fatal("cross-owner reference accepted")
	}
	if _, err := s.pool.Exec(ctx, "INSERT INTO reimport_blocks(owner_id,source_key_hash) VALUES($1,$2)", string(a.OwnerID), sourceKey(in.Connector, in.ExternalID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, a, in); !errors.Is(err, memory.ErrBlocked) {
		t.Fatal("reimport not blocked", err)
	}
	in.ExternalVersion = "v2"
	if _, err := s.Ingest(ctx, a, in); !errors.Is(err, memory.ErrBlocked) {
		t.Fatal("new source version bypassed block", err)
	}
	if _, err := s.Ingest(ctx, b, in); err != nil {
		t.Fatal("block crossed owner boundary", err)
	}
}

func TestPostgresWorkerRecoveryAndProcessing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	in := input()
	ref := mustIngest(t, s, scope, in)
	first, err := s.Claim(ctx, time.Minute)
	if err != nil || first == nil {
		t.Fatal("claim", err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET lease_until=now()-interval '1 second' WHERE id=$1", string(first.ID)); err != nil {
		t.Fatal(err)
	}
	second, err := s.Claim(ctx, time.Minute)
	if err != nil || second == nil || second.ID != first.ID || second.Attempts != 2 {
		t.Fatal("lease recovery", err)
	}
	if err := s.ProcessChunks(ctx, *first); !errors.Is(err, worker.ErrLeaseLost) {
		t.Fatal("old worker committed", err)
	}
	if err := s.Block(ctx, *first, "old_worker"); !errors.Is(err, worker.ErrLeaseLost) {
		t.Fatal("old worker acknowledged", err)
	}
	if err := s.ProcessChunks(ctx, *second); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessChunks(ctx, *second); !errors.Is(err, worker.ErrLeaseLost) {
		t.Fatal("completed lease reused", err)
	}
	rows, err := s.pool.Query(ctx, "SELECT start_rune,end_rune,body FROM chunks ORDER BY ordinal")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	runes := []rune(in.Text)
	for rows.Next() {
		var start, end int
		var body string
		if err := rows.Scan(&start, &end, &body); err != nil {
			t.Fatal(err)
		}
		if body != string(runes[start:end]) {
			t.Fatal("chunk no longer points to original")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count != len(memory.SplitText(in.Text, 1200, 160)) {
		t.Fatal("incorrect chunks", count, err)
	}
	w := worker.New(s, map[string]worker.Handler{"source.chunk": s.ProcessChunks}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for range 3 {
		if worked, err := w.RunOnce(ctx); err != nil || !worked {
			t.Fatal("provider stage missing", err)
		}
	}
	if worked, err := w.RunOnce(ctx); err != nil || worked {
		t.Fatal("queue not drained", err)
	}
	result, err := s.GetSource(ctx, scope, ref.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Processing) != 4 {
		t.Fatal("stage status lost")
	}
	for _, stage := range result.Processing {
		if stage.Stage == "source.chunk" && stage.State != "done" {
			t.Fatal("chunk not completed")
		}
		if stage.Stage != "source.chunk" && (stage.State != "blocked" || stage.ErrorCode != "handler_not_configured") {
			t.Fatal("unconfigured provider reported success")
		}
	}
}

func TestPostgresConcurrentClaimsAndRetryLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	for range 8 {
		in := input()
		in.ExternalID = string(memory.NewID())
		mustIngest(t, s, scope, in)
	}
	jobs := make(chan *worker.Job, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { job, err := s.Claim(ctx, time.Minute); jobs <- job; failures <- err })
	}
	wg.Wait()
	close(jobs)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[memory.ID]bool{}
	for job := range jobs {
		if job == nil || seen[job.ID] {
			t.Fatal("job claimed twice")
		}
		seen[job.ID] = true
		if err := s.Block(ctx, *job, "test_handler"); err != nil {
			t.Fatal(err)
		}
	}
	in := input()
	in.ExternalID = "retry"
	mustIngest(t, s, scope, in)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		job, err := s.Claim(ctx, time.Minute)
		if err != nil || job == nil || job.Attempts != attempt {
			t.Fatal("retry claim", err)
		}
		if err := s.Retry(ctx, *job, "temporary_failure"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET available_at=now() WHERE id=$1", string(job.ID)); err != nil {
			t.Fatal(err)
		}
	}
	if job, err := s.Claim(ctx, time.Minute); err != nil || job != nil {
		t.Fatal("attempt budget not enforced", err)
	}
}

func TestPostgresVersionMustExistAtCommit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'claim',1)", string(scope.OwnerID), string(memory.NewID()))
		return err
	})
	if err == nil {
		t.Fatal("identity committed without its canonical version")
	}
}

func TestPostgresCrashedWorkerExhaustsAttempts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	mustIngest(t, s, scope, input())
	var last *worker.Job
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		job, err := s.Claim(ctx, time.Minute)
		if err != nil || job == nil || job.Attempts != attempt {
			t.Fatal("crash recovery failed", err)
		}
		last = job
		if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET lease_until=now()-interval '1 second' WHERE id=$1", string(job.ID)); err != nil {
			t.Fatal(err)
		}
	}
	if job, err := s.Claim(ctx, time.Minute); err != nil || job != nil {
		t.Fatal("crashed job keeps retrying", err)
	}
	var state, code string
	if err := s.pool.QueryRow(ctx, "SELECT state,error_code FROM memory_jobs WHERE id=$1", string(last.ID)).Scan(&state, &code); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || code != "attempts_exhausted" {
		t.Fatal("exhaustion not recorded")
	}
}

func TestPostgresQueueFailureRollsBackSource(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_job() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected queue failure'; END $$;
		CREATE TRIGGER reject_job BEFORE INSERT ON memory_jobs FOR EACH ROW EXECUTE FUNCTION reject_job()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memory.NewService(s).Ingest(ctx, owner(), input()); err == nil {
		t.Fatal("injected failure did not propagate")
	}
	for _, table := range []string{"memory_records", "record_versions", "sources", "source_versions", "memory_jobs"} {
		var count int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial commit in %s: count=%d err=%v", table, count, err)
		}
	}
}
