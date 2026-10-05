package postgres

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestCompareDuplicateDoesNotWidenOldDependencyAccess(t *testing.T) {
	s := testStore(t)
	scope := owner()
	f := b1Model(t, s, `{"reply":"虚构依据保留","used":["M1"],"actions":[]}`)
	refs := compareFixture(t, s, scope, "云杉的虚构汇报期限周五")
	req := turnRequest("云杉的虚构汇报期限是什么")
	out := mustTurn(t, s, scope, req)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), refs[0], true)
	next := compareFixture(t, s, scope, "云杉的虚构汇报期限周五，交给主管")[0]
	if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET expressed_at=expressed_at+interval '1 day' WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(next.ID)); err != nil {
		t.Fatal(err)
	}
	f.set(`{"duplicates":[{"keep":2,"members":[1,2]}],"superseded":[]}`)
	compareProcess(t, s, scope)
	b1Outdated(t, b1History(t, s, scope, out.ConversationID), false)
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(refs[0].ID), AgentIDs: []string{}})
	history := b1History(t, s, scope, out.ConversationID)
	b1Outdated(t, history, true)
	f.set(`{"reply":"按现有可见依据回答","actions":[]}`)
	follow := turnRequest("继续查虚构汇报")
	follow.ConversationID = &out.ConversationID
	mustTurn(t, s, scope, follow)
	b1Absent(t, f.last(t).Prompt, out.Turn.Reply)
	b1HasRef(t, b1Refs(t, s, scope, follow.RequestID), refs[0], false)
}
func TestCompareLostLeaseCannotWriteRetirementOrProgress(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{}`)
	refs := compareFixture(t, s, scope, "旧虚构依据", "新虚构依据")
	entered, release := make(chan struct{}), make(chan struct{})
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		secretaryModelReply(w, `{"duplicates":[],"superseded":[{"old":1,"new":2}]}`)
	})
	j := compareJob(t, s, scope, false, CompareVersion)
	done := make(chan error, 1)
	go func() { done <- s.ProcessCompare(context.Background(), j) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("no call")
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE memory_jobs SET lease_token=gen_random_uuid() WHERE id=$1", string(j.ID)); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("lost lease accepted output")
	}
	compareState(t, s, scope, refs, []string{"", ""}, 0)
	var usage int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose='compare'", string(scope.OwnerID)).Scan(&usage); err != nil || usage != 1 {
		t.Fatal("issued call must still be billed", usage, err)
	}
}
func TestCompareEntityUndoAfterUserEditRefusesAtomically(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{"same":true,"keep":2}`)
	old, a := compareEntityFixture(t, s, scope, "小陈", "小陈负责虚构松林项目")
	keep, _ := compareEntityFixture(t, s, scope, "陈亮", "陈亮就是小陈，负责虚构松林项目")
	j := compareJob(t, s, scope, true, CompareVersion)
	if err := s.ProcessEntityCompare(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	b1Correct(t, s, scope, a, "陈亮现在负责虚构云海项目")
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	cmd := workspace.Command{Type: "undoEntityMerge", ID: string(old.ID), RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
	if _, err = s.Execute(context.Background(), scope, cmd); !errors.Is(err, workspace.ErrChangedSince) {
		t.Fatal("unsafe undo", err)
	}
	var subject string
	if err = s.pool.QueryRow(context.Background(), "SELECT subject_id::text FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=2", string(scope.OwnerID), string(a.ID)).Scan(&subject); err != nil || subject != string(keep.ID) {
		t.Fatal("refused undo partially mutated subject", subject, err)
	}
	var active bool
	if err = s.pool.QueryRow(context.Background(), "SELECT undone_at IS NULL FROM entity_merges WHERE owner_id=$1 AND merged_id=$2", string(scope.OwnerID), string(old.ID)).Scan(&active); err != nil || !active {
		t.Fatal("refused undo mutated merge", active, err)
	}
}
func TestCompareDuplicateEvidenceCopiesEachSourceOnce(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{"duplicates":[{"keep":3,"members":[1,2,3]}],"superseded":[]}`)
	refs := compareFixture(t, s, scope, "合成重复甲", "合成重复乙", "合成重复丙")
	// A repeated proof locator and another duplicate referring to the same source
	// must not multiply evidence on the keeper.
	if _, err := s.pool.Exec(context.Background(), `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance)
 SELECT owner_id,gen_random_uuid(),source_id,source_version,$3,1,'{"line":2}',acquisition,stance FROM evidence WHERE owner_id=$1 AND target_id=$2`, string(scope.OwnerID), string(refs[0].ID), string(refs[1].ID)); err != nil {
		t.Fatal(err)
	}
	compareProcess(t, s, scope)
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM evidence WHERE owner_id=$1 AND target_id=$2", string(scope.OwnerID), string(refs[2].ID)).Scan(&n); err != nil || n != 3 {
		t.Fatal(n, err)
	}
}

func TestCompareRestoreProtectionSurvivesActionSnapshotExpiry(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{"duplicates":[],"superseded":[{"old":1,"new":2}]}`)
	refs := compareFixture(t, s, scope, "合成旧期限周三", "合成新期限周五")
	compareProcess(t, s, scope)
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	cmd := workspace.Command{Type: "restoreMemory", ID: string(refs[0].ID), RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
	if _, err = s.Execute(context.Background(), scope, cmd); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(context.Background(), "UPDATE action_log SET created_at=now()-interval '31 days' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), cmd.RequestID); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构保留期检查事项"})
	if _, err = s.pool.Exec(context.Background(), "UPDATE claims SET compared=0 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(refs[1].ID)); err != nil {
		t.Fatal(err)
	}
	compareProcess(t, s, scope)
	compareState(t, s, scope, refs, []string{"", ""}, CompareVersion)
}
