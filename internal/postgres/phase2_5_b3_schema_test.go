package postgres_test

import (
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase25B3_ClaimDeletionCascadesItemsAndDeadlines(t *testing.T) {
	f := phase25B234NewFixture(t)
	g, refs := f.cardGroup(t, 4)
	key := f.card(t, g, refs, false)
	f.handover(t, key, false)
	for _, r := range refs {
		f.deadline(t, r, time.Now().Add(48*time.Hour))
	}
	if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{refs[0]}}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"status_card_items", "deadlines"} {
		var removed, remaining int
		if err := f.db.QueryRow(f.ctx, "SELECT count(*) FILTER(WHERE claim_id=$2),count(*) FILTER(WHERE claim_id<>$2) FROM "+table+" WHERE owner_id=$1", f.scope.OwnerID, refs[0].ID).Scan(&removed, &remaining); err != nil {
			t.Fatal(err)
		}
		if removed != 0 || remaining != 3 {
			t.Errorf("%s deleted=%d remaining=%d, want 0,3", table, removed, remaining)
		}
	}
	f.exec(t, `DELETE FROM status_cards WHERE owner_id=$1 AND key=$2`, f.scope.OwnerID, key)
	var n int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM status_card_items WHERE owner_id=$1 AND key=$2`, f.scope.OwnerID, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("card deletion retains %d items", n)
	}
	f.assertRevisions(t, refs[1:]...)
}
