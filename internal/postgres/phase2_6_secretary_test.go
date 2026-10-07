package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The foreground secretary baseline is separate so a failure cannot prevent
// the five scheduler/processor/read/write concurrency cases from executing.
func TestPhase26C1C8SecretaryScaleBaseline(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(f.Context, 60*time.Second)
	defer cancel()
	type result struct {
		out workspace.DeskTurnResponse
		err error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		out, err := f.Store.DeskTurn(WithMemoryTier(runCtx, "light"), f.Scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase26", Text: "Please answer this fictitious ordinary question."})
		done <- result{out, err}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	waits := map[string]int{}
	for {
		select {
		case r := <-done:
			t.Logf("secretary elapsed=%s actual_provider_calls=%d waits=%v reply=%q receipts=%+v error=%v", time.Since(start), len(m.calls("secretary")), waits, r.out.Turn.Reply, r.out.Turn.Receipts, r.err)
			if r.err != nil || r.out.Turn.Reply != "Fictitious acceptance reply." || len(m.calls("secretary")) != 1 {
				t.Fatal("scale baseline did not reach the fictitious model and answer")
			}
			return
		case <-ticker.C:
			rows, err := f.Store.pool.Query(f.Context, `SELECT coalesce(wait_event_type,'')||':'||coalesce(wait_event,''),query,pg_blocking_pids(pid)::text FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND (state='active' OR state='idle in transaction')`)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var event, sql, blockers string
				if err := rows.Scan(&event, &sql, &blockers); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				if strings.HasPrefix(event, "Lock:") {
					key := event + " " + sql
					waits[key]++
					if waits[key] == 1 {
						t.Logf("owned database lock wait: event=%s blockers=%s query=%s", event, blockers, sql)
					}
				}
			}
			rows.Close()
		case <-runCtx.Done():
			// Join the request using its exact context, never terminate by process name.
			r := <-done
			t.Fatalf("secretary deadline: %v result=%+v waits=%v", r.err, r.out.Turn, waits)
		}
	}
}
