package postgres

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase26G1C6SecretaryContextAndActionOverflowRemainVisible(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error {
		for kind, n := range map[string]int{"task": 55, "project": 60, "idea": 30} {
			for i := 0; i < n; i++ {
				item := newItem(kind, fmt.Sprintf("Fictitious %s overflow %03d", kind, i))
				if err := saveItem(f.Context, tx, f.Scope, item); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.Reply = func(c phase26Call) string {
		if !strings.Contains(c.System, "你是用户的前台秘书") {
			return m.goldReply(c)
		}
		actions := []any{}
		for i := 0; i < 15; i++ {
			actions = append(actions, map[string]any{"op": "create_task", "title": fmt.Sprintf("Fictitious overflow action %02d", i)})
		}
		b, _ := json.Marshal(map[string]any{"reply": "Fictitious bounded context reply.", "actions": actions, "used": []string{}})
		return string(b)
	}
	m.mu.Unlock()
	start := time.Now()
	out, err := f.Store.DeskTurn(WithMemoryTier(f.Context, "light"), f.Scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase26", Text: "Please arrange the fictitious ceramic specimens."})
	if err != nil {
		t.Fatal(err)
	}
	var prompt string
	for _, call := range m.calls("secretary") {
		if strings.Contains(call.System, "你是用户的前台秘书") {
			prompt = call.Prompt
		}
	}
	for prefix, want := range map[string]int{"T": 40, "P": 50, "I": 20} {
		count := len(regexp.MustCompile(`(?m)^`+prefix+`[0-9]+：`).FindAllString(prompt, -1))
		if count != want {
			t.Errorf("bounded %s prompt rows=%d expected%d", prefix, count, want)
		}
	}
	for reason, want := range map[string]int{"task_context_limit": 15, "project_context_limit": 10, "idea_context_limit": 10} {
		var count int
		if err := f.Store.pool.QueryRow(f.Context, `SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$1 AND reason=$2`, f.Scope.OwnerID, reason).Scan(&count); err != nil || count != want {
			t.Errorf("observable %s count=%d expected%d err=%v", reason, count, want, err)
		}
	}
	skipped := 0
	for _, r := range out.Turn.Receipts {
		if r.Status == "skipped" {
			skipped++
		}
	}

	var retained int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FROM work_items WHERE owner_id=$1`, f.Scope.OwnerID).Scan(&retained); err != nil || retained != 155 {
		t.Errorf("bounded context lost stored items retained=%d expected155 err=%v", retained, err)
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"task_context_limit", "project_context_limit", "idea_context_limit"} {
		if !strings.Contains(string(health), reason) {
			t.Errorf("health hides %s", reason)
		}
	}
	t.Logf("actual prompt tasks/projects/ideas=40/50/20; visible omitted=15/10/10; actions applied10/skipped_receipts=%d; retained_items=155 elapsed=%s health=%s", skipped, time.Since(start), health)
	t.Run("extra_actions_are_counted_and_visible", func(t *testing.T) {
		phase26Finding(t, "S-P26-005")
		var count int
		if err := f.Store.pool.QueryRow(f.Context, `SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$1 AND stage='secretary' AND outcome='overflow' AND reason LIKE '%action%'`, f.Scope.OwnerID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 5 {
			t.Errorf("15 proposed / 10 applied actions leave5 omitted, but observable action overflow=%d skipped_receipts=%d health=%s", count, skipped, health)
		}
	})

}
