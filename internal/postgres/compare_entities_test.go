package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func compareEntityFixture(t *testing.T, s *Store, scope memory.Scope, name, text string) (memory.Ref, memory.Ref) {
	t.Helper()
	entity := memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}, Type: "person", Name: name}
	claim := memory.Claim{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}}, SubjectID: entity.ID, Predicate: "虚构人物", Value: asJSON(text), Nature: "fact", Acquisition: "direct", Confirmation: "candidate"}
	src := b1Source(t, s, scope, "虚构人物资料", text, "manual")
	if _, err := s.Commit(context.Background(), scope, memory.CommitRequest{RequestID: memory.NewID(), Entities: []memory.Entity{entity}, Claims: []memory.Claim{claim}, Evidence: []memory.Evidence{{Source: src, Target: claim.Ref, Acquisition: "direct", Stance: "supports"}}}); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(claim.ID), AgentIDs: []string{"model", "manual"}})
	if _, err := s.pool.Exec(context.Background(), "UPDATE claims SET organized=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(claim.ID), OrganizeVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'person')", string(scope.OwnerID), string(claim.ID), string(entity.ID)); err != nil {
		t.Fatal(err)
	}
	return entity.Ref, claim.Ref
}
func TestCompareEntityMergeAliasesAndUndo(t *testing.T) {
	s := testStore(t)
	scope := owner()
	f := b1Model(t, s, `{"same":true,"keep":2}`)
	old, a := compareEntityFixture(t, s, scope, "小陈", "小陈负责虚构松林项目")
	keep, b := compareEntityFixture(t, s, scope, "陈亮", "陈亮就是负责松林项目的小陈")
	j := compareJob(t, s, scope, true, CompareVersion)
	if err := s.ProcessEntityCompare(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	var p comparisonEntityPair
	if err := json.Unmarshal([]byte(f.last(t).Prompt), &p); err != nil || len(p.Entities[0].Memories) != 1 || len(p.Entities[1].Memories) != 1 {
		t.Fatal(err, p)
	}
	for _, ref := range []memory.Ref{a, b} {
		var subject string
		if err := s.pool.QueryRow(context.Background(), "SELECT subject_id::text FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(ref.ID)).Scan(&subject); err != nil || subject != string(keep.ID) {
			t.Fatal(subject, err)
		}
	}
	err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		for _, name := range []string{"小陈", "陈亮"} {
			id, err := entityTx(context.Background(), tx, scope.OwnerID, "person", name)
			if err != nil {
				return err
			}
			if id != keep.ID {
				t.Errorf("alias %s resolved to %s", name, id)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"小陈", "陈亮"} {
		out, err := s.Recall(context.Background(), memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}, memory.RecallRequest{Query: name, Team: &memory.TeamRecall{Text: name, Candidates: 20}, Mode: "remember"})
		if err != nil {
			t.Fatal(err)
		}
		found := map[memory.ID]bool{}
		for _, candidate := range out.Memories {
			found[candidate.ID] = true
		}
		if !found[a.ID] || !found[b.ID] {
			t.Errorf("alias recall %s lacks merged memories: %+v", name, out)
		}
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "undoEntityMerge", ID: string(old.ID)})
	for _, pair := range [][2]memory.Ref{{a, old}, {b, keep}} {
		var subject string
		if err := s.pool.QueryRow(context.Background(), "SELECT subject_id::text FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(pair[0].ID)).Scan(&subject); err != nil || subject != string(pair[1].ID) {
			t.Fatal("undo subject", subject, err)
		}
	}
	var remaining int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM aliases WHERE owner_id=$1 AND entity_id=$2 AND alias='小陈'", string(scope.OwnerID), string(keep.ID)).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("undo copied alias", remaining, err)
	}
	if n, err := s.ScheduleCompare(context.Background(), time.Now()); err != nil || n != 1 {
		t.Fatal("undo merge must not immediately remerge, only group comparison can queue", n, err)
	}
}
func TestCompareEntityNegativeAndAmbiguousNames(t *testing.T) {
	s := testStore(t)
	scope := owner()
	f := b1Model(t, s, `{"same":false,"keep":null}`)
	compareEntityFixture(t, s, scope, "老王", "老王在虚构松林学校教书")
	compareEntityFixture(t, s, scope, "老王", "老王经营虚构云朵面包店")
	compareEntityFixture(t, s, scope, "那个王先生", "那个王先生来自虚构岚谷")
	j := compareJob(t, s, scope, true, CompareVersion)
	if err := s.ProcessEntityCompare(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM entity_merges WHERE owner_id=$1", string(scope.OwnerID)).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := s.ScheduleCompare(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.entity_compare:%' AND state='queued'", string(scope.OwnerID)).Scan(&n); err != nil || n != 0 || len(f.all()) != 1 {
		t.Fatal("negative pair repeatedly queued", n, len(f.all()), err)
	}
}
