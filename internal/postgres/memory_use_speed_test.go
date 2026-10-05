package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestB4SameDatabaseTurnSpeed(t *testing.T) {
	if os.Getenv("PCAS_B4_PERF") == "" {
		t.Skip("set PCAS_B4_PERF for same fictional database before/after speed")
	}
	s, scope := p2Fixture(t, 10000)
	// Four full cards, twelve rules and fifteen deadlines exercise the largest
	// common light payload, rather than measuring only an almost empty card.
	for g := 0; g < 4; g++ {
		var eid string
		err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
			id, err := entityTx(context.Background(), tx, scope.OwnerID, "topic", fmt.Sprintf("虚构季度组%d", g))
			eid = string(id)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		ms := []workspace.Memory{}
		for j := 0; j < 25; j++ {
			i := 97 * (1 + g + 4*j)
			m := workspace.Memory{ID: p2ID("claim", i), Version: 1}
			ms = append(ms, m)
			b4Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'topic')`, scope.OwnerID, m.ID, eid)
		}
		b4Card(t, s, scope, "entity:"+eid, "topic", fmt.Sprintf("虚构季度组%d", g), ms...)
	}
	rules := []workspace.Memory{}
	for i := 1; i <= 12; i++ {
		text := fmt.Sprintf("虚构要求%d：季度汇报每页不超过三行", i)
		b4Exec(t, s, "UPDATE claim_revisions SET value=to_jsonb($3::text),category='rule' WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, p2ID("claim", i), text)
		b4Exec(t, s, "UPDATE source_versions SET body=$3 WHERE owner_id=$1 AND source_id=$2", scope.OwnerID, p2ID("source", i), text)
		b4Exec(t, s, "UPDATE record_search SET search_text=$3 WHERE owner_id=$1 AND record_id=ANY($2::uuid[])", scope.OwnerID, []string{p2ID("source", i), p2ID("claim", i)}, text)
		rules = append(rules, workspace.Memory{ID: p2ID("claim", i), Version: 1})
	}
	b4Card(t, s, scope, "self:rule", "self", "虚构对助手的要求", rules...)
	for i := 1; i <= 15; i++ {
		id := p2ID("claim", 97*i)
		at := time.Now().AddDate(0, 0, i).Truncate(time.Minute)
		text := fmt.Sprintf("虚构霜叶的季度汇报第%d项截止 %s", 97*i, at.UTC().Format(time.RFC3339))
		b4Exec(t, s, "UPDATE claim_revisions SET value=to_jsonb($3::text) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, id, text)
		b4Exec(t, s, "UPDATE source_versions SET body=$3 WHERE owner_id=$1 AND source_id=$2", scope.OwnerID, p2ID("source", 97*i), text)
		b4Exec(t, s, "UPDATE record_search SET search_text=$3 WHERE owner_id=$1 AND record_id=ANY($2::uuid[])", scope.OwnerID, []string{p2ID("source", 97*i), id}, text)
		b4Exec(t, s, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,title) VALUES($1,$2,$3,1,'deadline',$4,$5)`, scope.OwnerID, memory.NewID(), id, at, text)
	}
	trace := p2TracedStore(t, s)
	ctx := b4Context(s)
	for i := 0; i < 3; i++ {
		trace.reset()
		start := time.Now()
		before, err := s.b4BeforeDeskTurn(context.Background(), scope, turnRequest("霜叶的季度汇报准备得怎么样？"))
		oldTime := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("before queries: %+v", trace.times)
		trace.reset()
		start = time.Now()
		after, err := s.DeskTurn(ctx, scope, turnRequest("霜叶的季度汇报准备得怎么样？"))
		newTime := time.Since(start)
		t.Logf("after queries: %+v", trace.times)
		if err != nil {
			t.Fatal(err)
		}
		if before.Turn.Reply != after.Turn.Reply {
			t.Fatal(before, after)
		}
		t.Logf("same fictional database: memories=10000 originals=30000 sample=%d before=%s after_light=%s ratio=%.3f", i, oldTime, newTime, float64(newTime)/float64(oldTime))
	}
}
