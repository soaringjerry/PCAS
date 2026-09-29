package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestEvidencedGraphAndScopedExpansion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	source := mustIngest(t, s, scope, input())
	revision := func(kind memory.Kind) memory.Revision {
		return memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: kind}}
	}
	entity := memory.Entity{Revision: revision(memory.EntityKind), Name: "河边旧书店", Type: "shop", Aliases: []string{"老书屋"}}
	claim := memory.Claim{Revision: revision(memory.ClaimKind), SubjectID: entity.ID, Predicate: "访问意愿", Value: asJSON("以后想去河边旧书店"), Nature: "intention", Acquisition: "direct", Confirmation: "confirmed"}
	episode := memory.Episode{Revision: revision(memory.EpisodeKind), Title: "周末出行讨论", Members: []memory.Ref{source.Ref, claim.Ref}}
	relation := memory.Relation{Revision: revision(memory.RelationKind), From: claim.Ref, To: episode.Ref, Type: "belongs_to"}
	in := memory.CommitRequest{RequestID: memory.NewID(), Entities: []memory.Entity{entity}, Claims: []memory.Claim{claim}, Episodes: []memory.Episode{episode}, Relations: []memory.Relation{relation}}
	for _, ref := range []memory.Ref{claim.Ref, episode.Ref, relation.Ref} {
		in.Evidence = append(in.Evidence, memory.Evidence{Source: source.Ref, Target: ref, Acquisition: "direct", Stance: "supports"})
	}
	refs, err := s.Commit(ctx, scope, in)
	if err != nil || len(refs) != 4 {
		t.Fatalf("commit: %v", err)
	}
	if _, err := s.Commit(ctx, scope, in); err != nil {
		t.Fatal("not idempotent", err)
	}
	result, err := s.Expand(ctx, scope, memory.ExpandRequest{Refs: []memory.Ref{claim.Ref}, Evidence: true})
	if err != nil || len(result.Relations) != 1 || len(result.Evidence) != 1 {
		t.Fatalf("expand: %+v %v", result, err)
	}
	agent := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "external"}
	if _, err := s.Expand(ctx, agent, memory.ExpandRequest{Refs: []memory.Ref{claim.Ref}}); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("ungranted graph exposed", err)
	}
	if _, err := s.pool.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,'external')", string(scope.OwnerID), string(claim.ID)); err != nil {
		t.Fatal(err)
	}
	result, err = s.Expand(ctx, agent, memory.ExpandRequest{Refs: []memory.Ref{claim.Ref}, Evidence: true})
	if err != nil || len(result.Relations) != 0 || len(result.Evidence) != 0 {
		t.Fatal("ungranted endpoints or evidence exposed", err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{source.Ref}, BlockReimport: true}); err != nil {
		t.Fatal("delete graph", err)
	}
	data, err := s.Export(ctx, scope, false, false)
	if err != nil || strings.Contains(string(data), "河边旧书店") {
		t.Fatal("deleted graph leaked", err)
	}
}
