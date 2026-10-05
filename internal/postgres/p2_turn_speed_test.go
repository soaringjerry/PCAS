package postgres

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type p2QuerySpan struct {
	sql   string
	start time.Time
}
type p2QueryTime struct {
	Calls    int
	Duration time.Duration
}
type p2Trace struct {
	mu    sync.Mutex
	times map[string]p2QueryTime
	plan  func(context.Context, pgx.TraceQueryStartData)
}
type p2TraceKey struct{}

func (p *p2Trace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if p.plan != nil {
		p.plan(ctx, d)
	}
	return context.WithValue(ctx, p2TraceKey{}, p2QuerySpan{d.SQL, time.Now()})
}
func (p *p2Trace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	s := ctx.Value(p2TraceKey{}).(p2QuerySpan)
	kind := p2QueryKind(s.sql)
	p.mu.Lock()
	defer p.mu.Unlock()
	x := p.times[kind]
	x.Calls++
	x.Duration += time.Since(s.start)
	p.times[kind] = x
}
func p2QueryKind(sql string) string {
	kind := "other"
	switch {
	case strings.Contains(sql, "linked AS (SELECT m.member_id"):
		kind = "recall"
	case strings.Contains(sql, "FROM adopted_artifacts a WHERE"):
		kind = "artifact permissions"
	case strings.HasPrefix(sql, "SELECT r.id::text,c.version,c.nature"):
		kind = "memory list"
	case strings.Contains(sql, "FILTER (WHERE coalesce((to_jsonb(cl)"):
		kind = "organize counts"
	case strings.HasPrefix(sql, "SELECT count(*) FROM memory_records r JOIN claim_revisions"):
		kind = "memory total"
	case strings.Contains(sql, "conversation_key") && strings.Contains(sql, "source_contexts"):
		kind = "conversation window"
	}
	return kind
}
func (p *p2Trace) reset() { p.mu.Lock(); defer p.mu.Unlock(); p.times = map[string]p2QueryTime{} }
func p2TracedStore(t *testing.T, s *Store) *p2Trace {
	t.Helper()
	trace := &p2Trace{}
	trace.reset()
	cfg := s.pool.Config()
	if dir := os.Getenv("PCAS_P2_PLAN_DIR"); dir != "" {
		seen := map[string]bool{}
		var planMu sync.Mutex
		trace.plan = func(ctx context.Context, d pgx.TraceQueryStartData) {
			kind := p2QueryKind(d.SQL)
			if kind == "other" {
				return
			}
			// Capture the actual window visibility check rather than its earlier
			// inexpensive conversation-key lookup.
			if kind == "conversation window" && !strings.Contains(d.SQL, "source_evidence") {
				return
			}
			planMu.Lock()
			defer planMu.Unlock()
			if seen[kind] {
				return
			}
			seen[kind] = true
			cc := cfg.ConnConfig.Copy()
			cc.Tracer = nil
			conn, err := pgx.ConnectConfig(ctx, cc)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close(ctx)
			if _, err := conn.Exec(ctx, "SET jit=off"); err != nil {
				t.Error(err)
				return
			}
			rows, err := conn.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT TEXT) "+d.SQL, d.Args...)
			if err != nil {
				t.Error(err)
				return
			}
			defer rows.Close()
			var text strings.Builder
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Error(err)
					return
				}
				text.WriteString(line + "\n")
			}
			if err := rows.Err(); err != nil {
				t.Error(err)
				return
			}
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Error(err)
				return
			}
			if err := os.WriteFile(filepath.Join(dir, t.Name()[strings.LastIndex(t.Name(), "/")+1:]+"-"+strings.ReplaceAll(kind, " ", "-")+".txt"), []byte(text.String()), 0600); err != nil {
				t.Error(err)
			}
		}
	}
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	original := s.pool
	s.pool = pool
	t.Cleanup(func() { s.pool = original; pool.Close() })
	return trace
}

// All inputs are fictional. Bulk loading avoids timing ingestion instead of
// measuring the secretary, while retaining real revisions, evidence and grants.
func p2Fixture(t *testing.T, n int) (*Store, memory.Scope) {
	t.Helper()
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	sourceCount := 30000
	if n < 2500 {
		sourceCount = 300
	}
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"虚构季度汇报已准备。","actions":[]}`)
	})
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var self string
		id, err := entityTx(ctx, tx, scope.OwnerID, "self", "虚构用户云杉")
		if err != nil {
			return err
		}
		self = string(id)
		statements := []string{
			`CREATE TEMP TABLE p2_sources ON COMMIT DROP AS SELECT i,md5('p2-source-'||i)::uuid AS id,
 CASE WHEN i%997=0 THEN '虚构人物霜叶准备季度汇报。' ELSE '虚构日常资料编号'||i||'，云杉记录盆栽生长。' END AS body FROM generate_series(1,30000) i`,
			`CREATE TEMP TABLE p2_claims ON COMMIT DROP AS SELECT i,md5('p2-claim-'||i)::uuid AS id,
 CASE WHEN i%97=0 THEN '虚构人物霜叶的季度汇报第'||i||'项已经准备。' ELSE '虚构人物云杉的盆栽观察记录编号'||i||'。' END AS body FROM generate_series(1,$2::int) i`,
			`INSERT INTO memory_records(owner_id,id,kind,version,created_at,updated_at) SELECT $1,id,'source',1,now()-interval '2 days'+i*interval '1 second',now()-interval '2 days'+i*interval '1 second' FROM p2_sources
 UNION ALL SELECT $1,id,'claim',1,now()-interval '1 day'+i*interval '1 second',now()-interval '1 day'+i*interval '1 second' FROM p2_claims`,
			`INSERT INTO record_versions(owner_id,record_id,version,actor,recorded_at,expressed_at) SELECT $1,id,1,'import',now()-interval '2 days'+i*interval '1 second',now()-interval '2 days'+i*interval '1 second' FROM p2_sources
 UNION ALL SELECT $1,id,1,'ai',now()-interval '1 day'+i*interval '1 second',now()-interval '1 day'+i*interval '1 second' FROM p2_claims`,
			`INSERT INTO sources SELECT $1,id,'archive','p2/'||i FROM p2_sources`,
			`INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type) SELECT $1,id,1,'1',decode(repeat('00',32),'hex'),'虚构原话',body,'text/plain' FROM p2_sources`,
			`INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,branch) SELECT $1,id,1,'fictional-conversation-'||(i/20),'user','active' FROM p2_sources`,
			`INSERT INTO claims(owner_id,id,organized) SELECT $1,id,CASE WHEN i%2=0 THEN 1 ELSE 0 END FROM p2_claims`,
			`INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type) SELECT $1,id,1,$3,'fictional-fact',to_jsonb(body),'fact','direct','adopted','initial' FROM p2_claims`,
			`INSERT INTO claim_source_keys(owner_id,source_id,claim_key,claim_id) SELECT $1,s.id,'fictional-'||c.i,c.id FROM p2_claims c JOIN p2_sources s ON s.i=c.i`,
			`INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) SELECT $1,md5('p2-evidence-'||c.i)::uuid,s.id,1,c.id,1,'{}','direct','supports' FROM p2_claims c JOIN p2_sources s ON s.i=c.i`,
			`INSERT INTO activity(owner_id,record_id,last_effective_use_at,pinned) SELECT $1,id,now(),true FROM p2_claims`,
			`INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT $1,id,'model' FROM p2_claims UNION ALL SELECT $1,id,'model' FROM p2_sources`,
			`INSERT INTO record_search(owner_id,record_id,record_version,search_text) SELECT $1,id,1,body FROM p2_claims UNION ALL SELECT $1,id,1,body FROM p2_sources`,
			`INSERT INTO work_items(owner_id,id,kind,title,status,version,document,created_at,updated_at) SELECT $1,md5('p2-task-'||i)::uuid,'task','虚构事项'||i,'todo',1,
 jsonb_build_object('id',md5('p2-task-'||i)::uuid,'kind','task','title','虚构事项'||i,'status','todo','version',1,'notes',CASE WHEN i<=5 THEN '虚构已采纳结果'||i ELSE '' END),now()-interval '3 days'+i*interval '1 second',now()-interval '3 days'+i*interval '1 second' FROM generate_series(1,20) i`,
			`INSERT INTO agent_runs(owner_id,id,thing_id,agent_id,status,reserved_cost,created_at,document) SELECT $1,md5('p2-run-'||i)::uuid,md5('p2-task-'||i)::uuid,'model','done',0,now()-interval '2 days',jsonb_build_object('id',md5('p2-run-'||i)::uuid,'thingId',md5('p2-task-'||i)::uuid,'agentId','model','status','done') FROM generate_series(1,5) i`,
			`INSERT INTO run_dependencies SELECT $1,md5('p2-run-'||i)::uuid,md5('p2-claim-'||i)::uuid,1 FROM generate_series(1,5) i`,
			`INSERT INTO adopted_artifacts SELECT $1,md5('p2-run-'||i)::uuid,md5('p2-task-'||i)::uuid,'notes','','虚构已采纳结果'||i FROM generate_series(1,5) i`,
		}
		for _, sql := range statements {
			sql = strings.ReplaceAll(sql, "generate_series(1,30000)", fmt.Sprintf("generate_series(1,%d)", sourceCount))
			args := []any{}
			if strings.Contains(sql, "$1") {
				args = append(args, string(scope.OwnerID))
				sql = strings.ReplaceAll(sql, "$1", "$1::uuid")
			}
			if strings.Contains(sql, "$2") {
				args = append(args, n)
			}
			if strings.Contains(sql, "$3") {
				args = append(args, self)
				sql = strings.ReplaceAll(sql, "$3", "$2")
			}
			if strings.Contains(sql, "$2") && !strings.Contains(sql, "$1") {
				args = []any{n}
				sql = strings.ReplaceAll(sql, "$2", "$1")
			}
			if _, err := tx.Exec(ctx, sql, args...); err != nil {
				return fmt.Errorf("fixture: %w (%s)", err, sql)
			}
			// Let foreign-key checks choose indexed lookups even during this
			// single bulk-load transaction, before autovacuum can collect stats.
			words := strings.Fields(sql)
			if len(words) > 2 && words[0] == "INSERT" {
				table := strings.Split(words[2], "(")[0]
				if _, err := tx.Exec(ctx, "ANALYZE "+pgx.Identifier{table}.Sanitize()); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
	return s, scope
}

// PCAS_P2_PERF=1 go test -run '^TestP2TurnSpeed$' -count=1 -v ./internal/postgres
// Uses a real queue/admission/turn, an immediate HTTP fake and the full snapshot.
func TestP2TurnSpeed(t *testing.T) {
	if os.Getenv("PCAS_P2_PERF") == "" {
		t.Skip("set PCAS_P2_PERF for the synthetic 30,000-source performance fixture")
	}
	if os.Getenv("PCAS_P2_ENFORCE") != "" && (os.Getenv("PCAS_P2_SIZE") != "" || os.Getenv("PCAS_P2_PLAN_DIR") != "") {
		t.Fatal("acceptance requires both sizes and no EXPLAIN instrumentation")
	}
	totals := map[int]time.Duration{}
	sizes := []int{2500, 10000}
	if v := os.Getenv("PCAS_P2_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		sizes = []int{n}
	}
	sampleCount := 3
	if v := os.Getenv("PCAS_P2_SAMPLES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatal("invalid sample count")
		}
		sampleCount = n
	}
	for _, n := range sizes {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, scope := p2Fixture(t, n)
			if os.Getenv("PCAS_P2_LEGACY") != "" {
				if _, err := s.pool.Exec(context.Background(), "DROP INDEX claim_source_keys_claim_idx, memory_records_active_updated_idx"); err != nil {
					t.Fatal(err)
				}
			}
			trace := p2TracedStore(t, s)
			var samples []time.Duration
			for i := 0; i < sampleCount; i++ {
				trace.reset()
				start := time.Now()
				turn := s.DeskTurn
				if os.Getenv("PCAS_P2_LEGACY") != "" {
					turn = s.p2LegacyDeskTurn
				}
				out, err := turn(context.Background(), scope, turnRequest("霜叶的季度汇报准备得怎么样？"))
				wall := time.Since(start)
				keys := []string{}
				var total time.Duration
				for k, v := range trace.times {
					keys = append(keys, k)
					total += v.Duration
				}
				sort.Strings(keys)
				for _, k := range keys {
					v := trace.times[k]
					t.Logf("sample=%d query=%s calls=%d time=%s", i, k, v.Calls, v.Duration)
				}
				sources := 30000
				if n < 2500 {
					sources = 300
				}
				t.Logf("memories=%d sources=%d tasks=20 adopted=5 sample=%d database=%s wall=%s", n, sources, i, total, wall)
				if err != nil {
					t.Fatal(err)
				}
				if out.Turn.Reply != "虚构季度汇报已准备。" {
					t.Fatal(out.Turn.Reply)
				}
				if os.Getenv("PCAS_P2_ENFORCE") != "" && n == 10000 && total > 2*time.Second {
					t.Fatalf("10,000-claim sample %d exceeded 2s: %s", i, total)
				}
				samples = append(samples, total)
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			totals[n] = samples[len(samples)/2]
		})
	}
	if len(sizes) == 2 {
		t.Logf("median database: 2500=%s 10000=%s ratio=%.3f", totals[2500], totals[10000], float64(totals[10000])/float64(totals[2500]))
	}
	if os.Getenv("PCAS_P2_ENFORCE") != "" && (totals[10000] > 2*time.Second || totals[10000] > 2*totals[2500]) {
		t.Fatal("P2 latency/growth target exceeded")
	}
}
