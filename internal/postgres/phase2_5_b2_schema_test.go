package postgres_test

import (
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase25B2_FrozenSchema(t *testing.T) {
	f := phase25B2NewFixture(t)
	ref := f.claim(t, "虚构人物陆青在准备白鹭月报。")
	var retired, by *string
	var compared int
	if err := f.db.QueryRow(f.ctx, `SELECT retired,retired_by::text,compared FROM claims WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID).Scan(&retired, &by, &compared); err != nil {
		t.Fatal(err)
	}
	if retired != nil && *retired != "" || by != nil || compared != 0 {
		t.Errorf("new claim defaults: retired=%v by=%v compared=%d", retired, by, compared)
	}
	for _, update := range []string{
		`UPDATE claims SET retired='unsupported',retired_by=$3 WHERE owner_id=$1 AND id=$2`,
		`UPDATE claims SET retired='superseded',retired_by=NULL WHERE owner_id=$1 AND id=$2 AND $3::uuid IS NOT NULL`,
	} {
		if _, err := f.db.Exec(f.ctx, update, f.scope.OwnerID, ref.ID, memory.NewID()); err == nil {
			t.Error("frozen retirement constraint accepted invalid state")
		}
	}
}
