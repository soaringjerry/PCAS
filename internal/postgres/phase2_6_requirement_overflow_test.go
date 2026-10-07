package postgres

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase26C2C6GlobalRequirementsOverflowIsVisibleAndPrioritized(t *testing.T) {
	f := phase26LoadFixture(t)
	phase26NewModel(t, f)
	phase26LibraryPrecondition(t, f)
	ids := append([]memory.ID{}, f.Claims[:40]...)
	ids = append(ids, f.Claims[364], f.Claims[365])
	// Forty-two equally long requirements exceed the dedicated 6000-rune allowance.
	// Confirmed older rows must outrank the newest unconfirmed rows.
	phase26Exec(t, f, `UPDATE claim_revisions SET value=to_jsonb(repeat('F',200)),confirmation=CASE WHEN claim_id=ANY($3::uuid[]) THEN 'confirmed' ELSE 'unknown' END WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])`, f.Scope.OwnerID, ids, ids[:10])
	phase26Exec(t, f, `UPDATE assistant_requirements SET unrestricted=true,scope='' WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])`, f.Scope.OwnerID, ids)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	var kept, global, totalRunes int
	err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error {
		u, err := f.Store.startUseContextTx(f.Context, tx, f.Scope)
		if err != nil {
			return err
		}
		if err := f.Store.finishUseContextTx(f.Context, tx, f.Scope, workspace.Agent{ID: "phase26", MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}}, nil, "Fictitious unrelated context", nil, &u); err != nil {
			return err
		}
		seen := map[memory.ID]bool{}
		for _, r := range u.Rules {
			seen[memory.ID(r.ID)] = true
			if strings.HasPrefix(u.RequirementScopes[r.ID], "不限范围") {
				global++
				totalRunes += len([]rune(r.Text))
			}
		}
		for _, id := range ids[:10] {
			if !seen[id] {
				t.Errorf("confirmed requirement omitted before unconfirmed rows: %s", id)
			}
		}
		kept = len(u.Rules)
		if totalRunes > 6000 || global != 30 || len(u.Deadlines) != 48 {
			t.Errorf("dedicated requirement/deadline capacity global=%d chars=%d deadlines=%d", global, totalRunes, len(u.Deadlines))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var omitted int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$1 AND stage='secretary' AND reason='global_requirement_char_budget'`, f.Scope.OwnerID).Scan(&omitted); err != nil {
		t.Fatal(err)
	}
	// Raw global total=42: the original26 plus16 newly scoped-to-global rows.
	// Coordinator change after the live rollout (contract section 2 item 4
	// revised): scoped requirements are no longer handed over wholesale, so
	// only the 30 global ones that fit are kept; the scoped ones are counted.
	if omitted != 12 || kept != 30 {
		t.Errorf("overflow kept=%d omitted=%d expected30/12", kept, omitted)
	}
	var scoped int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$1 AND stage='secretary' AND reason='scoped_requirement_left_to_recall'`, f.Scope.OwnerID).Scan(&scoped); err != nil || scoped != 324 {
		t.Errorf("scoped requirements left to recall=%d expected324 err=%v", scoped, err)
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil || !strings.Contains(string(health), "global_requirement_char_budget") {
		t.Errorf("global overflow not observable: %s %v", health, err)
	}
	t.Logf("per-turn global=%d runes=%d total_requirements=%d visible_omitted=%d", global, totalRunes, kept, omitted)
}
