package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestCompareRestoreMarkerCompatibilityAndRuleScope(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			s, scope, ctx := testStore(t), owner(), context.Background()
			b1Model(t, s, `{"duplicates":[],"superseded":[{"old":1,"new":2}]}`)
			refs := compareFixture(t, s, scope, "虚构云海旧期限周三", "虚构云海新期限周五")
			compareProcess(t, s, scope)
			action := string(memory.NewID())
			state, err := s.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Execute(ctx, scope, workspace.Command{Type: "restoreMemory", ID: string(refs[0].ID), RequestID: action, ExpectedRevision: state.Revision}); err != nil {
				t.Fatal(err)
			}
			if legacy {
				if _, err := s.pool.Exec(ctx, `UPDATE background_markers SET stage=$3 WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'memory.compare:%:restored:%'`, string(scope.OwnerID), string(refs[0].ID), fmt.Sprintf("memory.compare_restored:%d:%s", CompareVersion, action)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.pool.Exec(ctx, `UPDATE claims SET compared=0 WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(refs[1].ID)); err != nil {
				t.Fatal(err)
			}
			compareProcess(t, s, scope)
			compareState(t, s, scope, refs, []string{"", ""}, CompareVersion)
			b1Undo(t, s, scope, action)
			var remaining int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_markers WHERE owner_id=$1 AND record_id=$2 AND (stage LIKE 'memory.compare:%:restored:%' OR stage LIKE 'memory.compare_restored:%')`, string(scope.OwnerID), string(refs[0].ID)).Scan(&remaining); err != nil || remaining != 0 {
				t.Fatal("undo left exemption", remaining, err)
			}
			compareState(t, s, scope, refs, []string{"superseded", ""}, CompareVersion)
			// Restoring exempts one rule version, not every future comparison rule.
			workspaceCommand(t, s, scope, workspace.Command{Type: "restoreMemory", ID: string(refs[0].ID)})
			j := compareJob(t, s, scope, false, CompareVersion+1)
			if err := s.processCompareVersion(ctx, j, CompareVersion+1); err != nil {
				t.Fatal(err)
			}
			compareState(t, s, scope, refs, []string{"superseded", ""}, CompareVersion+1)
		})
	}
}
