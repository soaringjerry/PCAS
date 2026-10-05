package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func aliasEntityFixture(t *testing.T, s *Store, scope memory.Scope, kind, name string, count int) (memory.Ref, []memory.Ref) {
	t.Helper()
	entity := memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}, Type: kind, Name: name}
	refs := []memory.Ref{}
	for i := range count {
		text := fmt.Sprintf("虚构别名资料 %s 的证据 %d，归属于同一对象。", name, i)
		src := b1Source(t, s, scope, "虚构别名资料", text, "manual")
		claim := memory.Claim{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}}, SubjectID: entity.ID, Predicate: "虚构对象", Value: asJSON(text), Nature: "fact", Acquisition: "direct", Confirmation: "candidate"}
		req := memory.CommitRequest{RequestID: memory.NewID(), Claims: []memory.Claim{claim}, Evidence: []memory.Evidence{{Source: src, Target: claim.Ref, Acquisition: "direct", Stance: "supports"}}}
		if i == 0 {
			req.Entities = []memory.Entity{entity}
		}
		if _, err := s.Commit(context.Background(), scope, req); err != nil {
			t.Fatal(err)
		}
		workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(claim.ID), AgentIDs: []string{"model", "manual"}})
		if _, err := s.pool.Exec(context.Background(), `UPDATE claims SET organized=$3 WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(claim.ID), OrganizeVersion); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(context.Background(), `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,$4)`, string(scope.OwnerID), string(claim.ID), string(entity.ID), kind); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, claim.Ref)
	}
	return entity.Ref, refs
}

func aliasNamesJob(t *testing.T, s *Store, scope memory.Scope) worker.Job {
	t.Helper()
	ctx := context.Background()
	if _, err := s.ScheduleEntityCandidates(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Remove only pending unrelated jobs, never history used by quota gates.
	if _, err := s.pool.Exec(ctx, `DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE $2`, string(scope.OwnerID), EntityCandidatesStage+":%"); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx, time.Minute)
	if err != nil || j == nil || !strings.HasPrefix(j.Stage, EntityCandidatesStage+":") {
		t.Fatal("name-list claim", j, err)
	}
	return *j
}

func aliasAssertSubjects(t *testing.T, s *Store, scope memory.Scope, refs []memory.Ref, entity memory.ID) {
	t.Helper()
	for _, ref := range refs {
		var got string
		if err := s.pool.QueryRow(context.Background(), `SELECT subject_id::text FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&got); err != nil || got != string(entity) {
			t.Fatal("subject", got, entity, err)
		}
	}
}

func TestEntityCandidatesTranslationAbbreviationKeepAndUndo(t *testing.T) {
	for _, tc := range []struct {
		name, kind, a, b string
		an, bn           int
		keepA            bool
	}{
		{"translated_place_chinese_tie", "place", "蓝沙湾", "Azure Quay", 1, 1, true},
		{"abbreviated_organization_more_memories", "organization", "云杉大学", "Spruce", 2, 3, false},
		{"counts_beyond_ten_memory_sample", "organization", "Fictitious Cedar University", "杉木大学", 12, 11, true},
		{"project_translation", "project", "晨光计划", "Daybreak Initiative", 1, 1, true},
		{"person_translation", "person", "林青", "Lin Qing", 1, 1, true},
		{"topic_translation", "topic", "星光工艺", "Starlight Craft", 1, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, scope, ctx := testStore(t), owner(), context.Background()
			f := b1Model(t, s, `{"groups":[[1,2]]}`)
			a, ar := aliasEntityFixture(t, s, scope, tc.kind, tc.a, tc.an)
			b, br := aliasEntityFixture(t, s, scope, tc.kind, tc.b, tc.bn)
			j := aliasNamesJob(t, s, scope)
			if err := s.ProcessEntityCandidates(ctx, j); err != nil {
				t.Fatal(err)
			}
			var input entityCandidateBatch
			prompt := f.last(t).Prompt
			if err := json.Unmarshal([]byte(prompt), &input); err != nil || len(input.Entities) != 2 || input.Scope != tc.kind {
				t.Fatal("list", input, err)
			}
			if strings.Contains(prompt, "memories") || strings.Contains(prompt, "虚构别名资料") {
				t.Fatal("memory body leaked into name list")
			}
			counts := map[string]int{}
			for _, e := range input.Entities {
				counts[e.Name] = e.MemoryCount
			}
			if counts[tc.a] != tc.an || counts[tc.b] != tc.bn {
				t.Fatal("name-list counts", counts)
			}
			var merges int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM entity_merges").Scan(&merges); err != nil || merges != 0 {
				t.Fatal("name list merged directly", merges, err)
			}
			// Deliberately recommend the wrong survivor: the writer owns counts
			// and the Chinese-name tie break, not the model.
			f.set(`{"same":true,"keep":1}`)
			j = compareJob(t, s, scope, true, EntityCompareVersion)
			if err := s.ProcessEntityCompare(ctx, j); err != nil {
				t.Fatal(err)
			}
			keep, merged := b, a
			if tc.keepA {
				keep, merged = a, b
			}
			all := append(append([]memory.Ref{}, ar...), br...)
			aliasAssertSubjects(t, s, scope, all, keep.ID)
			for _, name := range []string{tc.a, tc.b} {
				out, err := s.Recall(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}, memory.RecallRequest{Query: name, Team: &memory.TeamRecall{Text: name, Candidates: 100}, Mode: "remember"})
				if err != nil {
					t.Fatal(err)
				}
				seen := map[memory.ID]bool{}
				for _, m := range out.Memories {
					seen[m.ID] = true
				}
				for _, ref := range all {
					if !seen[ref.ID] {
						t.Errorf("alias %s lost %s", name, ref.ID)
					}
				}
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "undoEntityMerge", ID: string(merged.ID)})
			aliasAssertSubjects(t, s, scope, ar, a.ID)
			aliasAssertSubjects(t, s, scope, br, b.ID)
			var copied int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM aliases WHERE owner_id=$1 AND entity_id=$2 AND alias=$3`, string(scope.OwnerID), string(keep.ID), map[bool]string{true: tc.b, false: tc.a}[tc.keepA]).Scan(&copied); err != nil || copied != 0 {
				t.Fatal("undo alias", copied, err)
			}
		})
	}
}

func TestEntityCandidatesNearbyNamesStillRequirePairConfirmation(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	f := b1Model(t, s, `{"groups":[[1,2]]}`)
	a, ar := aliasEntityFixture(t, s, scope, "organization", "云杉工坊", 1)
	b, br := aliasEntityFixture(t, s, scope, "organization", "云杉实验室", 1)
	if err := s.ProcessEntityCandidates(ctx, aliasNamesJob(t, s, scope)); err != nil {
		t.Fatal(err)
	}
	f.set(`{"same":false,"keep":null}`)
	if err := s.ProcessEntityCompare(ctx, compareJob(t, s, scope, true, EntityCompareVersion)); err != nil {
		t.Fatal(err)
	}
	aliasAssertSubjects(t, s, scope, ar, a.ID)
	aliasAssertSubjects(t, s, scope, br, b.ID)
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM entity_merges").Scan(&n); err != nil || n != 0 {
		t.Fatal("negative confirmation merged", n, err)
	}
}

func TestEntityCandidatesCrossTypeAndGroupBookmarks(t *testing.T) {
	for _, kind := range []string{"place", "organization"} {
		for _, translated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/translated=%v", kind, translated), func(t *testing.T) {
				s, scope, ctx := testStore(t), owner(), context.Background()
				f := b1Model(t, s, `{"same":true,"keep":2}`)
				name, topicName := "蓝沙湾", "蓝沙湾"
				if kind == "organization" {
					name, topicName = "云杉学院", "云杉学院"
				}
				if translated {
					topicName = "Fictitious Azure Institute"
				}
				keep, ar := aliasEntityFixture(t, s, scope, kind, name, 1)
				old, br := aliasEntityFixture(t, s, scope, "topic", topicName, 2)
				if translated {
					f.set(`{"groups":[[1,2]]}`)
					if err := s.ProcessEntityCandidates(ctx, aliasNamesJob(t, s, scope)); err != nil {
						t.Fatal(err)
					}
					f.set(`{"same":true,"keep":2}`)
				}
				if err := s.ProcessEntityCompare(ctx, compareJob(t, s, scope, true, EntityCompareVersion)); err != nil {
					t.Fatal(err)
				}
				all := append(append([]memory.Ref{}, ar...), br...)
				aliasAssertSubjects(t, s, scope, all, keep.ID)
				for _, alias := range []string{name, topicName} {
					recall, err := s.Recall(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}, memory.RecallRequest{Query: alias, Team: &memory.TeamRecall{Text: alias, Candidates: 100}, Mode: "remember"})
					if err != nil {
						t.Fatal(err)
					}
					seen := map[memory.ID]bool{}
					for _, m := range recall.Memories {
						seen[m.ID] = true
					}
					for _, ref := range all {
						if !seen[ref.ID] {
							t.Errorf("cross-type alias %s lost %s", alias, ref.ID)
						}
					}
				}
				for _, id := range []memory.ID{old.ID, keep.ID} {
					page, err := s.ListMemories(ctx, scope, workspace.MemoryQuery{Group: string(id), Limit: 1})
					if err != nil || page.Total != 3 || len(page.Items) != 1 || page.Next == "" {
						t.Fatal("merged group page", page, err)
					}
					page2, err := s.ListMemories(ctx, scope, workspace.MemoryQuery{Group: string(id), Limit: 10, Cursor: page.Next})
					if err != nil || page2.Total != 3 || len(page2.Items) != 2 {
						t.Fatal("merged group continuation", page2, err)
					}
				}
				facets, err := s.MemoryFacets(ctx, scope)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, g := range facets.Groups {
					if g.EntityID == string(keep.ID) && g.Count == 3 {
						found = true
					}
				}
				if !found {
					t.Fatal("original topic disappeared from facets", facets)
				}
				if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
					id, err := entityTx(ctx, tx, scope.OwnerID, "topic", topicName)
					if err != nil || id != keep.ID {
						return fmt.Errorf("old topic alias resolved to %s: %w", id, err)
					}
					id, err = findOrganizeGroupTx(ctx, tx, scope.OwnerID, "topic", topicName)
					if err != nil || id != keep.ID {
						return fmt.Errorf("organizer old topic alias resolved to %s: %w", id, err)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				workspaceCommand(t, s, scope, workspace.Command{Type: "undoEntityMerge", ID: string(old.ID)})
				aliasAssertSubjects(t, s, scope, ar, keep.ID)
				aliasAssertSubjects(t, s, scope, br, old.ID)
				page, err := s.ListMemories(ctx, scope, workspace.MemoryQuery{Group: string(old.ID)})
				if err != nil || page.Total != 2 {
					t.Fatal("restored topic group", page, err)
				}
			})
		}
	}
}

func TestEntityCandidatesBatchCoverageAndTypeBoundaries(t *testing.T) {
	names := make([]entityCandidateName, 301)
	for i := range names {
		names[i] = entityCandidateName{Type: "place", Name: fmt.Sprintf("虚构名字%03d", i), MemoryCount: 1, Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}
	}
	names[0].Name, names[300].Name = "蓝沙湾", "Azure Quay"
	batches := entityCandidateBatches(names, EntityCompareVersion)
	covered := map[string]bool{}
	for _, batch := range batches {
		if len(batch.Entities) > 200 {
			t.Fatal("batch too large", len(batch.Entities))
		}
		for i, a := range batch.Entities {
			for _, b := range batch.Entities[i+1:] {
				covered[entityCandidatePairMarker(a.Ref, b.Ref, EntityCompareVersion)] = true
			}
		}
	}
	for i, a := range names {
		for _, b := range names[i+1:] {
			if !covered[entityCandidatePairMarker(a.Ref, b.Ref, EntityCompareVersion)] {
				t.Fatal("pair cannot meet", a.Name, b.Name)
			}
		}
	}
	for _, p := range [][2]string{{"person", "place"}, {"person", "topic"}, {"project", "topic"}, {"project", "organization"}, {"area", "area"}, {"place", "organization"}} {
		if entityPairTypesAllowed(p[0], p[1]) {
			t.Fatal("unsafe type pair", p)
		}
	}
}

func TestEntityCandidatesWrongPersonTypeCannotCrossMerge(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	b1Model(t, s, `{"groups":[[1,2,99]]}`)
	a, ar := aliasEntityFixture(t, s, scope, "person", "虚构蓝沙地区", 1)
	b, br := aliasEntityFixture(t, s, scope, "place", "虚构蓝沙地区", 1)
	if _, err := s.ScheduleCompare(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.entity_compare:%' AND state='queued'`, scope.OwnerID).Scan(&n); err != nil || n != 0 {
		t.Fatal("wrongly typed person cross candidate", n, err)
	}
	aliasAssertSubjects(t, s, scope, ar, a.ID)
	aliasAssertSubjects(t, s, scope, br, b.ID)
}

func TestEntityCandidatesHourlyBudgetAndRuleRecheck(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	f := b1Model(t, s, `{"groups":[]}`)
	aliasEntityFixture(t, s, scope, "place", "蓝沙湾", 1)
	aliasEntityFixture(t, s, scope, "place", "Azure Quay", 1)
	j := aliasNamesJob(t, s, scope)
	if _, err := s.pool.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state) VALUES(gen_random_uuid(),$1,$2,$3,'memory.organize:1:quota-seed','done')`, scope.OwnerID, j.Record.ID, j.Record.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO background_usage(owner_id,job_id,reserved_cost) SELECT $1,id,0 FROM memory_jobs CROSS JOIN generate_series(1,120) WHERE owner_id=$1 AND stage='memory.organize:1:quota-seed'`, scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	err := s.ProcessEntityCandidates(ctx, j)
	var quota *worker.JobError
	if !errors.As(err, &quota) || quota.Code != "compare_hourly_limit" || !quota.NoAttempt || len(f.all()) != 0 {
		t.Fatal("catalogue bypassed 120 cap", err, len(f.all()))
	}
	if _, err := s.pool.Exec(ctx, `UPDATE background_usage SET created_at=now()-interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessEntityCandidates(ctx, j); err != nil {
		t.Fatal(err)
	}
	if len(f.all()) != 1 {
		t.Fatal("released budget", len(f.all()))
	}
	// The actual name-list invocation must also block the OLD organizer:
	// bring 119 prior calls back into the hour, plus that real list call.
	if _, err := s.pool.Exec(ctx, `UPDATE background_usage SET created_at=now() WHERE id IN(SELECT b.id FROM background_usage b JOIN memory_jobs job ON job.id=b.job_id WHERE job.stage='memory.organize:1:quota-seed' LIMIT 119)`); err != nil {
		t.Fatal(err)
	}
	organizeTestMemory(t, s, scope, "虚构名单调用后的整理哨兵")
	organizeJob := leaseStage(t, s, scope, j.Record, fmt.Sprintf("%s:%d:%s", OrganizeStage, OrganizeVersion, memory.NewID()))
	err = s.ProcessOrganize(ctx, organizeJob)
	if !errors.As(err, &quota) || quota.Code != "organize_hourly_limit" || !quota.NoAttempt || len(f.all()) != 1 {
		t.Fatal("organizer omitted actual name-list call", err, len(f.all()))
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		b, err := nextEntityCandidateBatchTx(ctx, tx, scope.OwnerID, EntityCompareVersion)
		if err != nil || b != nil {
			return fmt.Errorf("same rules repeated list: %+v %w", b, err)
		}
		b, err = nextEntityCandidateBatchTx(ctx, tx, scope.OwnerID, EntityCompareVersion+1)
		if err != nil || b == nil {
			return fmt.Errorf("new rules skipped list: %+v %w", b, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEntityCandidatesModelCallDoesNotHoldOwnerLock(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	b1Model(t, s, `{"groups":[]}`)
	_, first := aliasEntityFixture(t, s, scope, "place", "蓝沙湾", 1)
	_, refs := aliasEntityFixture(t, s, scope, "place", "Azure Quay", 1)
	entered, release := make(chan struct{}), make(chan struct{})
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		secretaryModelReply(w, `{"groups":[[1,2]]}`)
	})
	j := aliasNamesJob(t, s, scope)
	done := make(chan error, 1)
	go func() { done <- s.ProcessEntityCandidates(ctx, j) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("no list call")
	}
	lockErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT owner_id FROM workspace_owners WHERE owner_id=$1 FOR UPDATE NOWAIT", scope.OwnerID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE claims SET retired='superseded',retired_by=$3 WHERE owner_id=$1 AND id=$2`, scope.OwnerID, refs[0].ID, first[0].ID)
		return err
	})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if lockErr != nil {
		t.Fatal("owner row held across list call", lockErr)
	}
	var hints int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.entity_candidates:%:pair:%'`, scope.OwnerID).Scan(&hints); err != nil || hints != 0 {
		t.Fatal("changed list wrote hints", hints, err)
	}
}

func TestEntityCandidatesRuleUpgradeRechecksEntitiesOnly(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	f := b1Model(t, s, `{"same":false,"keep":null}`)
	unrelated := compareFixture(t, s, scope, "虚构旧版比较已经完成的无关记忆")[0]
	if _, err := s.pool.Exec(ctx, `UPDATE claims SET compared=$3 WHERE owner_id=$1 AND id=$2`, scope.OwnerID, unrelated.ID, CompareVersion); err != nil {
		t.Fatal(err)
	}
	compareEntityFixture(t, s, scope, "虚构星岚", "虚构星岚负责同一份器材")
	compareEntityFixture(t, s, scope, "虚构星岚女士", "虚构星岚女士是器材负责人的完整称呼")
	j := compareJob(t, s, scope, true, EntityCompareVersion)
	if err := s.processEntityCompareVersion(ctx, j, 1); err != nil {
		t.Fatal(err)
	}
	f.set(`{"same":true,"keep":2}`)
	j = compareJob(t, s, scope, true, EntityCompareVersion)
	if err := s.ProcessEntityCompare(ctx, j); err != nil {
		t.Fatal(err)
	}
	var rule, claimRule int
	if err := s.pool.QueryRow(ctx, `SELECT rule FROM entity_merges WHERE owner_id=$1 AND undone_at IS NULL`, scope.OwnerID).Scan(&rule); err != nil || rule != EntityCompareVersion || len(f.all()) != 2 {
		t.Fatal("identity epoch", rule, len(f.all()), err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT compared FROM claims WHERE owner_id=$1 AND id=$2`, scope.OwnerID, unrelated.ID).Scan(&claimRule); err != nil || claimRule != CompareVersion {
		t.Fatal("unrelated claim epoch changed", claimRule, err)
	}
}

func TestEntityCandidatesThreeNamesKeepChineseAndUndoInReverse(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	f := b1Model(t, s, `{"groups":[[1,2,3]]}`)
	a, ar := aliasEntityFixture(t, s, scope, "place", "蓝沙湾", 1)
	b, br := aliasEntityFixture(t, s, scope, "place", "Azure Quay", 1)
	c, cr := aliasEntityFixture(t, s, scope, "place", "LanSha Harbour", 1)
	if err := s.ProcessEntityCandidates(ctx, aliasNamesJob(t, s, scope)); err != nil {
		t.Fatal(err)
	}
	f.set(`{"same":true,"keep":2}`)
	for range 2 {
		if err := s.ProcessEntityCompare(ctx, compareJob(t, s, scope, true, EntityCompareVersion)); err != nil {
			t.Fatal(err)
		}
	}
	aliasAssertSubjects(t, s, scope, append(append(append([]memory.Ref{}, ar...), br...), cr...), a.ID)
	workspaceCommand(t, s, scope, workspace.Command{Type: "undoEntityMerge", ID: string(c.ID)})
	workspaceCommand(t, s, scope, workspace.Command{Type: "undoEntityMerge", ID: string(b.ID)})
	aliasAssertSubjects(t, s, scope, ar, a.ID)
	aliasAssertSubjects(t, s, scope, br, b.ID)
	aliasAssertSubjects(t, s, scope, cr, c.ID)
}

func TestEntityCandidatesInitialCatalogueCallEstimate(t *testing.T) {
	// Supplied inventory sizes only; all names/IDs and counts are fictitious.
	// Match the catalogue's ORDER BY entity_type,name,id before partitioning.
	names := []entityCandidateName{}
	for _, group := range []struct {
		kind string
		n    int
	}{{"organization", 323}, {"person", 92}, {"place", 251}, {"project", 47}, {"topic", 225}} {
		for i := range group.n {
			names = append(names, entityCandidateName{Type: group.kind, Name: fmt.Sprintf("虚构名单%04d", i), MemoryCount: 1, Ref: memory.Ref{ID: memory.NewID(), Kind: memory.EntityKind, Version: 1}})
		}
	}
	counts := map[string]int{}
	batches := entityCandidateBatches(names, EntityCompareVersion)
	for _, batch := range batches {
		counts[batch.Scope]++
	}
	for scope, want := range map[string]int{"organization": 6, "person": 1, "place": 3, "project": 1, "topic": 3, "place_topic": 8, "organization_topic": 11} {
		if counts[scope] != want {
			t.Errorf("scope=%s calls=%d want=%d", scope, counts[scope], want)
		}
	}
	if len(batches) != 33 {
		t.Errorf("initial catalogue calls=%d want=33", len(batches))
	}
	t.Logf("initial catalogue calls=%d scopes=%v; pair confirmations are additional", len(batches), counts)
}

func TestEntityCandidatesAndConfirmationCallsLeaveRowsUnlocked(t *testing.T) {
	for _, stage := range []string{"catalogue", "confirmation"} {
		t.Run(stage, func(t *testing.T) {
			s, scope := testStore(t), owner()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			b1Model(t, s, `{"groups":[[1,2]]}`)
			aliasEntityFixture(t, s, scope, "place", "蓝沙湾", 1)
			aliasEntityFixture(t, s, scope, "place", "Azure Quay", 1)
			j := aliasNamesJob(t, s, scope)
			process := s.ProcessEntityCandidates
			reply := `{"groups":[[1,2]]}`
			if stage == "confirmation" {
				if err := process(ctx, j); err != nil {
					t.Fatal(err)
				}
				j = compareJob(t, s, scope, true, EntityCompareVersion)
				process = s.ProcessEntityCompare
				reply = `{"same":true,"keep":1}`
			}
			entered, release := make(chan struct{}), make(chan struct{})
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				secretaryModelReply(w, reply)
			})
			done := make(chan error, 1)
			go func() { done <- process(ctx, j) }()
			select {
			case <-entered:
			case <-ctx.Done():
				close(release)
				t.Fatal("no model call")
			}
			err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				for _, table := range []string{"workspace_owners", "memory_records", "claims", "memory_jobs", "status_cards"} {
					if _, err := tx.Exec(ctx, "SELECT 1 FROM "+table+" WHERE owner_id=$1 FOR UPDATE NOWAIT", scope.OwnerID); err != nil {
						return fmt.Errorf("%s: %w", table, err)
					}
				}
				return nil
			})
			close(release)
			if err != nil {
				t.Errorf("row held during %s model call: %v", stage, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEntityCompareCompletedResultSurvivesBusyFollowupScheduling(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	model := b1Model(t, s, `{"groups":[[1,2,3]]}`)
	for _, name := range []string{"蓝沙湾", "Azure Quay", "Fictitious Azure Waterfront"} {
		aliasEntityFixture(t, s, scope, "place", name, 1)
	}
	if err := s.ProcessEntityCandidates(ctx, aliasNamesJob(t, s, scope)); err != nil {
		t.Fatal(err)
	}
	j := compareJob(t, s, scope, true, EntityCompareVersion)
	model.set(`{"same":true,"keep":1}`)
	blocker, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':compare-schedule',0))"); err != nil {
		t.Fatal(err)
	}
	// Result writes do not need the scheduler lock. After they commit, a busy
	// opportunistic follow-up must not try to defer the completed lease.
	if err := s.ProcessEntityCompare(ctx, j); err != nil {
		t.Errorf("completed result returned scheduling failure: %v", err)
	}
	var state string
	var merges int
	if err := s.pool.QueryRow(ctx, "SELECT state FROM memory_jobs WHERE id=$1", j.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM entity_merges WHERE owner_id=$1 AND undone_at IS NULL", scope.OwnerID).Scan(&merges); err != nil {
		t.Fatal(err)
	}
	if state != "done" || merges != 1 {
		t.Fatalf("result state=%s merges=%d", state, merges)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	queued, err := s.ScheduleCompare(ctx, time.Now())
	if err != nil || queued == 0 {
		t.Fatal("periodic scheduling did not recover follow-up", queued, err)
	}
}
