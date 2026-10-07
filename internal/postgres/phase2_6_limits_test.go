package postgres

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestPhase26A11ExtractionKeepsAll65ItemsAtScale(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	var text strings.Builder
	items := []map[string]any{}
	for i := 0; i < 65; i++ {
		note := fmt.Sprintf("Fictitious extraction note %02d records ceramic specimen %02d.", i, i)
		text.WriteString(note + "\n")
		items = append(items, map[string]any{"kind": "memory", "text": note, "nature": "fact", "subject": "用户", "predicate": "fictitious_specimen", "quote": note, "confidence": 0.99, "acquisition": "direct"})
	}
	output, _ := json.Marshal(map[string]any{"items": items})
	m.mu.Lock()
	m.Reply = func(phase26Call) string { return string(output) }
	m.mu.Unlock()
	src, err := f.Store.Ingest(f.Context, f.Scope, memory.IngestRequest{Connector: "capture", ExternalID: string(memory.NewID()), Title: "Fictitious extraction source", Text: text.String()})
	if err != nil {
		t.Fatal(err)
	}
	j := worker.Job{ID: memory.NewID(), OwnerID: f.Scope.OwnerID, Record: src.Ref, Stage: "source.extract", LeaseToken: memory.NewID(), Attempts: 1}
	phase26Exec(t, f, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state,attempts,lease_token,lease_until) VALUES($1,$2,$3,$4,$5,'leased',1,$6,now()+interval '5 minutes')`, j.ID, j.OwnerID, j.Record.ID, j.Record.Version, j.Stage, j.LeaseToken)
	start := time.Now()
	if err := f.Store.ProcessExtraction(f.Context, j); err != nil {
		t.Fatal(err)
	}
	rows, err := f.Store.pool.Query(f.Context, `SELECT c.value #>> '{}' FROM evidence e JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(e.owner_id,e.target_id,e.target_version) WHERE e.owner_id=$1 AND e.source_id=$2`, f.Scope.OwnerID, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		seen[v] = true
	}
	rows.Close()
	for _, item := range items {
		if !seen[item["text"].(string)] {
			t.Errorf("lost extracted memory %s", item["text"])
		}
	}
	if len(seen) != 65 || len(m.calls("secretary")) != 1 {
		t.Errorf("extraction count=%d provider=%d", len(seen), len(m.calls("secretary")))
	}
	t.Logf("extracted=65 model_calls=1 elapsed=%s", time.Since(start))
}

func TestPhase26G7A3IndependentHourlyBudgets(t *testing.T) {
	f := phase26LoadFixture(t)
	phase26NewModel(t, f)
	stages := []struct {
		stage string
		limit int
	}{{OrganizeStage, 40}, {CompareStage, 40}, {EntityCompareStage, 30}, {EntityCandidatesStage, 6}, {HandoverStage, 2}}
	for _, tc := range stages {
		for i := 0; i < tc.limit; i++ {
			phase26Exec(t, f, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) VALUES($1,$2,0,$3)`, memory.NewID(), f.Scope.OwnerID, tc.stage)
		}
		if err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error { return backgroundHourlyTx(f.Context, tx, tc.stage, "phase26_limit") }); err == nil {
			t.Errorf("%s exceeded frozen hourly cap=%d", tc.stage, tc.limit)
		}
		// Other stage allowances remain usable until THEIR own independent cap.
		for _, other := range stages {
			if other.stage == tc.stage {
				continue
			}
			var n int
			if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM background_usage WHERE stage=$1`, other.stage).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n < other.limit {
				if err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error { return backgroundHourlyTx(f.Context, tx, other.stage, "phase26_limit") }); err != nil {
					t.Errorf("%s borrowed/exhausted %s allowance: %v", tc.stage, other.stage, err)
				}
			}
		}
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("retained independent quota ledger=%s; reservations are preconditions, not provider attempts", health)
}

func TestPhase26G2A5B5InvalidOutputsPreserveGoodData(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26LibraryPrecondition(t, f)
	phase26Exec(t, f, `INSERT INTO handovers(owner_id,body,rule,built_at,stale,depends,input_hash) VALUES($1,'Fictitious last good handover',2,now()-interval '2 hours',true,'[]','fictitious previous')`, f.Scope.OwnerID)
	m.mu.Lock()
	m.Reply = func(phase26Call) string { return `{"fictitious_invalid":true}` }
	m.mu.Unlock()
	owner := f.Scope.OwnerID
	before := phase26Digest(t, f, []string{"claim_revisions", "deadlines", "assistant_requirements", "handovers"})
	phase26Isolate(t, f, OrganizeStage)
	for i := 0; i < 4; i++ {
		phase26Exec(t, f, `UPDATE claims SET organize_after='-infinity' WHERE owner_id=$1`, owner)
		if _, err := phase26Schedule(f.Context, f.Store, OrganizeStage); err != nil {
			t.Fatal(err)
		}
		j := phase26ClaimStage(t, f, OrganizeStage)
		if err := phase26Process(f.Context, f.Store, j); err != nil {
			e := phase26FinishError(t, f, j, err)
			if e.NoAttempt {
				t.Error("invalid output not counted as attempted failure")
			}
			phase26Exec(t, f, `UPDATE memory_jobs SET available_at=now() WHERE id=$1`, j.ID)
		}
	}
	var marked int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM claims WHERE owner_id=$1 AND organized>=2`, owner).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked != 0 || len(m.calls(OrganizeStage)) != 4 {
		t.Errorf("failed classification marked=%d actualcalls=%d", marked, len(m.calls(OrganizeStage)))
	}
	// Handover failure must retain old body/time and remain readable.
	phase26Isolate(t, f, HandoverStage)
	if _, err := phase26Schedule(f.Context, f.Store, HandoverStage); err != nil {
		t.Fatal(err)
	}
	phase26Isolate(t, f, HandoverStage)
	j := phase26ClaimStage(t, f, HandoverStage)
	phase26FinishError(t, f, j, phase26Process(f.Context, f.Store, j))
	after := phase26Digest(t, f, []string{"claim_revisions", "deadlines", "assistant_requirements", "handovers"})
	for table, hash := range before {
		if after[table] != hash {
			t.Errorf("invalid output overwrote good %s tuple/content", table)
		}
	}
	old, err := f.Store.ReadHandover(f.Context, f.Scope)
	if err != nil || old.Body != "Fictitious last good handover" {
		t.Errorf("last good handover lost: %+v %v", old, err)
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(health), "organize_invalid_output") || !strings.Contains(string(health), "handover_invalid_output") {
		t.Errorf("failure reasons not visible: %s", health)
	}
	t.Logf("four invalid classification calls preserve pending memories; health=%s", health)
	phase26InvalidHealth(t, health, OrganizeStage)
}
