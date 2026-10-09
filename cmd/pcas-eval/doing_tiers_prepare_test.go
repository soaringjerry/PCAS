package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/internal/postgres"
)

func TestTierPreparationAndRestoreValidateLibraryState(t *testing.T) {
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires empty disposable PostgreSQL")
	}
	ctx := context.Background()
	store, pool, cleanup, err := openTemporary(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	suite := doing.Suite{Synthetic: true, Timezone: "UTC", AsOf: "2026-10-07T00:00:00Z", Memories: []doing.Memory{
		{ID: "f1", Group: "虚构折纸", Text: "虚构松泉喜欢方形折纸", ExpressedAt: "2026-10-06T12:00:00Z"},
	}}
	bridge := newTierBridge(doing.FakeModel{}, true)
	defer bridge.server.Close()
	bridge.configure(store)
	seeded, err := seedTierSuite(ctx, store, pool, suite)
	if err != nil {
		t.Fatal(err)
	}
	p, err := prepareTierSuite(ctx, store, pool, seeded.Scope, bridge, filepath.Join(t.TempDir(), "prepare.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Complete || !p.Handover || !p.HandoverInputs || p.OrganizeRule != postgres.OrganizeVersion {
		t.Fatal(p)
	}
	if _, _, err = restoredTierDatabase(ctx, dsn, suite, p); err != nil {
		t.Fatal(err)
	}
	owner := string(seeded.Scope.OwnerID)
	for _, query := range []string{
		"UPDATE claims SET organized=0 WHERE owner_id=$1",
		"UPDATE handovers SET input_hash='obsolete' WHERE owner_id=$1",
		"UPDATE handovers SET rule=0 WHERE owner_id=$1",
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, query, owner); err != nil {
			t.Fatal(err)
		}
		// Inspect the same transaction via commit then undo with a second update;
		// restore must reject each stale product marker, not trust the saved report.
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if _, _, err = restoredTierDatabase(ctx, dsn, suite, p); err == nil {
			t.Fatal("accepted stale state", query)
		}
		if _, err = pool.Exec(ctx, "UPDATE claims SET organized=$2 WHERE owner_id=$1", owner, postgres.OrganizeVersion); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, "UPDATE handovers SET input_hash=library_handover_hash($1),rule=$2 WHERE owner_id=$1", owner, postgres.HandoverVersion); err != nil {
			t.Fatal(err)
		}
	}
	changed := p
	changed.Requirements++
	if _, _, err = restoredTierDatabase(ctx, dsn, suite, changed); err == nil {
		t.Fatal("accepted mismatched extraction counts")
	}
	// P0-55 upgrades only rule classifications; other v2 results remain current.
	original := postgres.OrganizeVersion
	postgres.OrganizeVersion = 3
	t.Cleanup(func() { postgres.OrganizeVersion = original })
	if _, err = pool.Exec(ctx, "UPDATE claims SET organized=2 WHERE owner_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	if _, err = readTierPreparation(ctx, pool, seeded.Scope); err != nil {
		t.Fatal("rejected retained non-rule v2 classification", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE claim_revisions SET category='rule' WHERE owner_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	if _, err = readTierPreparation(ctx, pool, seeded.Scope); err == nil {
		t.Fatal("accepted pending v3 rule classification")
	}

}
