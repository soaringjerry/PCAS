package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestConversationDispatchRetiresEntrancesBeforeModelAndIndexes(t *testing.T) {
	s, scope := testStore(t), owner()
	b4SmallChunks(t, 1000)
	roles, texts := []string{}, []string{}
	for i := 0; i < 600; i++ {
		roles = append(roles, "user")
		texts = append(texts, fmt.Sprintf("合成第%d条用户资料。", i))
	}
	f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": []any{}} })
	a := b4bImport(t, s, scope, b4bMessages(t, roles, texts))
	b4bOperation(t, s, scope, a.Batch, "organize")
	// An index that was queued hours ago must not delay the ready conversation.
	b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,available_at,created_at) VALUES(gen_random_uuid(),$1,$2,1,'source.tokenize',now()-interval '2 hours',now()-interval '2 hours')`, string(scope.OwnerID), string(a.Sources[0].ID))
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, a.Sources[0], "source.extract")); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, len(f.all()), 0)
	b2Equal(t, b4bProgress(t, s, scope, a.Batch).Organized, 0)
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage='source.extract' AND state='queued'`, string(scope.OwnerID)), 0)
	job, err := s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil || !strings.HasPrefix(job.Stage, conversationExtractionPrefix) {
		t.Fatal("index delayed conversation", job, err)
	}
	if err = s.ProcessExtraction(context.Background(), *job); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, len(f.all()), 1)
	b2Equal(t, b4bProgress(t, s, scope, a.Batch).Organized, 600)
}

func TestClaimPagesPastCoalescedEntrancesAndPausedHistory(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprint(held), func(t *testing.T) {
			s, scope := testStore(t), owner()
			a := b4bImport(t, s, scope, b4bMessages(t, []string{"user"}, []string{"合成的等待资料。"}))
			if !held {
				b4bOperation(t, s, scope, a.Batch, "organize")
				if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, a.Sources[0], "source.extract")); err != nil {
					t.Fatal(err)
				}
			}
			// Genuine legacy extraction windows can coexist with the newer conversation
			// family. More than a page are blocked by its coalescing (or a stored-only hold).
			b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at,created_at) SELECT gen_random_uuid(),$1,$2,1,'source.extract:legacy:'||n,10,now()-interval '2 hours',now()-interval '2 hours' FROM generate_series(1,1100)n`, string(scope.OwnerID), string(a.Sources[0].ID))
			later := b1Source(t, s, scope, "合成后面的可执行资料", "后面的任务也必须能领取。", "manual")
			b2Exec(t, s, `DELETE FROM memory_jobs WHERE owner_id=$1 AND record_id=$2`, string(scope.OwnerID), string(later.ID))
			b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority) VALUES(gen_random_uuid(),$1,$2,1,'source.tokenize',20)`, string(scope.OwnerID), string(later.ID))
			if !held {
				// Keep the family legitimately unavailable for this claim; its existence
				// still fences the obsolete windows without manufacturing a completed job.
				b2Exec(t, s, `UPDATE memory_jobs SET available_at=now()+interval '1 hour' WHERE owner_id=$1 AND stage LIKE 'source.extract:conversation:%'`, string(scope.OwnerID))
			}
			job, err := s.Claim(context.Background(), time.Minute)
			if err != nil || job == nil || job.Record.ID != later.ID {
				t.Fatal("blocked page hid executable work", job, err)
			}
			b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'source.extract:legacy:%' AND state='queued'`, string(scope.OwnerID)), 1100)
			next, err := s.Claim(context.Background(), time.Minute)
			if err != nil || next != nil {
				t.Fatal("all remaining jobs are held", next, err)
			}
		})
	}
}

func TestClaimConcurrentWorkersAndExpiredLease(t *testing.T) {
	s, scope := testStore(t), owner()
	for i := 0; i < 2; i++ {
		b1Source(t, s, scope, fmt.Sprint(i), fmt.Sprintf("合成并发资料%d", i), "manual")
	}
	var wg sync.WaitGroup
	got := make(chan memory.ID, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := s.Claim(context.Background(), time.Minute)
			if e != nil || j == nil {
				t.Error(j, e)
				return
			}
			got <- j.ID
		}()
	}
	wg.Wait()
	close(got)
	seen := map[memory.ID]bool{}
	for id := range got {
		if seen[id] {
			t.Fatal("duplicate lease", id)
		}
		seen[id] = true
	}
	if len(seen) != 2 {
		t.Fatal("expected two leases", seen)
	}
	for id := range seen {
		b2Exec(t, s, `UPDATE memory_jobs SET lease_until=now()-interval '1 minute' WHERE id=$1`, string(id))
		break
	}
	j, e := s.Claim(context.Background(), time.Minute)
	if e != nil || j == nil || j.Attempts != 2 || !seen[j.ID] {
		t.Fatal("expired lease did not resume", j, e)
	}
}

func TestImportProgressSeparatesPreparationModelAndSearch(t *testing.T) {
	s, scope := testStore(t), owner()
	a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "assistant"}, []string{"合成用户原文。", "合成辅助上下文。"}))
	for _, src := range a.Sources {
		b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state) VALUES(gen_random_uuid(),$1,$2,1,'source.chunk','done')`, string(scope.OwnerID), string(src.ID))
	}
	b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state) VALUES(gen_random_uuid(),$1,$2,1,'source.tokenize','done')`, string(scope.OwnerID), string(a.Sources[0].ID))
	p := b4bProgress(t, s, scope, a.Batch)
	if p.Prepared != 2 || p.Indexed != 1 || p.Organized != 0 || p.Activity != "held" {
		t.Fatal("preparation was mistaken for extraction", p)
	}
	b4bOperation(t, s, scope, a.Batch, "organize")
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, a.Sources[0], "source.extract")); err != nil {
		t.Fatal(err)
	}
	p = b4bProgress(t, s, scope, a.Batch)
	if p.Activity != "queued" || p.Organized != 0 {
		t.Fatal("unclaimed model work shown as running", p)
	}
	b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": []any{}} })
	j, err := s.Claim(context.Background(), time.Minute)
	if err != nil || j == nil {
		t.Fatal(j, err)
	}
	p = b4bProgress(t, s, scope, a.Batch)
	if p.Activity != "organizing" || p.Organized != 0 {
		t.Fatal("active model phase missing", p)
	}
	if err = s.ProcessExtraction(context.Background(), *j); err != nil {
		t.Fatal(err)
	}
	p = b4bProgress(t, s, scope, a.Batch)
	if p.Organized != 2 || p.Indexed != 1 || p.Activity != "indexing" {
		t.Fatal("remaining index work hidden", p)
	}
}

func TestConversationDispatchUpgradePreservesPendingSegments(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": []any{}} })
	a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "assistant"}, []string{"合成升级前用户资料。", "合成升级前上下文。"}))
	b4bOperation(t, s, scope, a.Batch, "organize")
	// Create the same durable first segment that the older worker had already
	// enqueued, without invoking the newer early-retirement behavior.
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Obtain the real normalized conversation key from the source metadata.
	var conversation string
	if err = tx.QueryRow(context.Background(), `SELECT conversation_key FROM source_contexts WHERE owner_id=$1 AND source_id=$2`, string(scope.OwnerID), string(a.Sources[0].ID)).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	sources, err := archiveConversationSources(context.Background(), tx, scope.OwnerID, conversation)
	if err != nil {
		t.Fatal(err)
	}
	if err = enqueueConversationTx(context.Background(), tx, scope.OwnerID, a.Sources[0], conversationRun(sources), 0, 1); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage) VALUES(gen_random_uuid(),$1,$2,1,'source.tokenize')`, string(scope.OwnerID), string(a.Sources[0].ID))
	b2Exec(t, s, `UPDATE memory_jobs SET priority=10,available_at=now()-interval '1 hour' WHERE owner_id=$1 AND stage='source.tokenize'`, string(scope.OwnerID))
	b2Exec(t, s, `DROP INDEX jobs_dispatch_ready_idx`)
	b2Exec(t, s, `DROP FUNCTION imported_job_priority(text)`)
	sql, err := migrations.ReadFile("migrations/032_conversation_dispatch.sql")
	if err != nil {
		t.Fatal(err)
	}
	b2Exec(t, s, string(sql))
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage='source.extract' AND state='queued'`, string(scope.OwnerID)), 0)
	b2Equal(t, b4bProgress(t, s, scope, a.Batch).Organized, 0)
	j, err := s.Claim(context.Background(), time.Minute)
	if err != nil || j == nil || !strings.HasPrefix(j.Stage, conversationExtractionPrefix) {
		t.Fatal("upgrade lost or delayed pending segment", j, err)
	}
	if err = s.ProcessExtraction(context.Background(), *j); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, len(f.all()), 1)
	b2Equal(t, b4bProgress(t, s, scope, a.Batch).Organized, 2)
}

func TestConversationDispatchLargeQueueUsesBoundedIndexPage(t *testing.T) {
	s, scope := testStore(t), owner()
	a := b4bImport(t, s, scope, b4bMessages(t, []string{"user"}, []string{"合成性能验收原文。"}))
	b4bOperation(t, s, scope, a.Batch, "organize")
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, a.Sources[0], "source.extract")); err != nil {
		t.Fatal(err)
	}
	// Populate a realistic reference-only queue, without importing or exposing
	// user content. The ready conversation must outrank 40,000 old index jobs.
	b2Exec(t, s, `WITH inserted AS (INSERT INTO memory_records(owner_id,id,kind,version) SELECT $1,gen_random_uuid(),'source',1 FROM generate_series(1,40000) RETURNING id)
 INSERT INTO record_versions(owner_id,record_id,version) SELECT $1,id,1 FROM inserted`, string(scope.OwnerID))
	b2Exec(t, s, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at,created_at)
 SELECT gen_random_uuid(),owner_id,id,1,'source.tokenize',20,now()-interval '2 hours',now()-interval '2 hours'
 FROM memory_records WHERE owner_id=$1 AND kind='source' AND id NOT IN(SELECT source_id FROM archive_entries WHERE owner_id=$1)`, string(scope.OwnerID))
	b2Exec(t, s, `ANALYZE memory_jobs`)
	b2Exec(t, s, `ANALYZE memory_records`)
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var raw []byte
	if err = tx.QueryRow(context.Background(), "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+claimWindowed, 60.0, string(memory.NewID()), 5).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []map[string]any
	if err = json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	indexed := false
	var inspect func(map[string]any)
	inspect = func(n map[string]any) {
		if n["Index Name"] == "jobs_dispatch_ready_idx" {
			indexed = true
		}
		if n["Relation Name"] == "memory_jobs" && n["Node Type"] == "Seq Scan" {
			rows, _ := n["Actual Rows"].(float64)
			removed, _ := n["Rows Removed by Filter"].(float64)
			if rows+removed > 1000 {
				t.Error("claim examined the entire synthetic queue", rows, removed)
			}
		}
		if children, ok := n["Plans"].([]any); ok {
			for _, p := range children {
				inspect(p.(map[string]any))
			}
		}
	}
	inspect(plan[0]["Plan"].(map[string]any))
	if !indexed {
		t.Fatal("ready page did not use dispatch ordering index")
	}
	t.Logf("40,000 queued indexes: claim execution %v ms", plan[0]["Execution Time"])
	if path := os.Getenv("PCAS_DISPATCH_PLAN_PATH"); path != "" {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
