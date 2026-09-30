package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestRecallReturnsMatchingTailChunk(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	in := input()
	in.Title = "长篇资料"
	in.Text = strings.Repeat("无关的背景材料。", 5000) + "唯一交付暗号是蓝色灯塔7319。"
	source := mustIngest(t, s, scope, in)
	result, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "蓝色灯塔7319", Mode: memory.Remember, Budget: memory.Budget{Candidates: 5, Tokens: 4000}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "唯一交付暗号是蓝色灯塔7319") {
		t.Fatalf("matched source but lost tail evidence: %s", result.Summary)
	}
	found := false
	for _, ref := range result.Memories {
		if ref.ID == source.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("source locator missing")
	}
	if err := s.ProcessChunks(ctx, leaseStage(t, s, scope, source.Ref, "source.chunk")); err != nil {
		t.Fatal(err)
	}
	result, err = s.Recall(ctx, scope, memory.RecallRequest{Query: "蓝色灯塔7319", Mode: memory.Remember, Budget: memory.Budget{Candidates: 5, Tokens: 4000}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "蓝色灯塔7319") || !strings.Contains(result.Summary, "片段") {
		t.Fatalf("indexed chunks lost evidence or locator: %s", result.Summary)
	}

}

func TestRecallGraphHonorsEffectiveAndKnowledgeTime(t *testing.T) {
	for _, temporal := range []string{"valid", "known"} {
		t.Run(temporal, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			scope := owner()
			source := mustIngest(t, s, scope, input())
			rev := func(kind memory.Kind) memory.Revision {
				return memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: kind}}
			}
			entity := memory.Entity{Revision: rev(memory.EntityKind), Name: "项目主体", Type: "project"}
			seed := memory.Claim{Revision: rev(memory.ClaimKind), SubjectID: entity.ID, Predicate: "seed", Value: asJSON("唯一查询锚点"), Nature: "fact", Acquisition: "direct", Confirmation: "confirmed"}
			hidden := memory.Claim{Revision: rev(memory.ClaimKind), SubjectID: entity.ID, Predicate: "future", Value: asJSON("不应出现的后续结论"), Nature: "fact", Acquisition: "direct", Confirmation: "confirmed"}
			relation := memory.Relation{Revision: rev(memory.RelationKind), From: seed.Ref, To: hidden.Ref, Type: "depends_on"}
			in := memory.CommitRequest{RequestID: memory.NewID(), Entities: []memory.Entity{entity}, Claims: []memory.Claim{seed, hidden}, Relations: []memory.Relation{relation}}
			for _, ref := range []memory.Ref{seed.Ref, hidden.Ref, relation.Ref} {
				in.Evidence = append(in.Evidence, memory.Evidence{Source: source.Ref, Target: ref, Acquisition: "direct", Stance: "supports"})
			}
			if _, err := s.Commit(ctx, scope, in); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			future := now.Add(time.Hour)
			column := "valid_from"
			if temporal == "known" {
				column = "recorded_at"
			}
			if _, err := s.pool.Exec(ctx, "UPDATE record_versions SET "+column+"=$3 WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(hidden.ID), future); err != nil {
				t.Fatal(err)
			}
			result, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "唯一查询锚点", Mode: memory.Remember, Context: memory.WorkingContext{ValidAt: &now, KnownAt: &now}, Budget: memory.Budget{Candidates: 10, Hops: 2}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.Summary, "不应出现的后续结论") {
				t.Fatalf("graph bypassed %s filter: %s", temporal, result.Summary)
			}
			if !strings.Contains(result.Summary, "唯一查询锚点") {
				t.Fatal("lost allowed seed")
			}
		})
	}
}
