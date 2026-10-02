package postgres

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// A long conversation recalls the same memory every turn. Each turn inherits
// the dependencies of its last six predecessors, so the stored list must stay
// a set; it used to double per turn (1, 2, 4 … 496 after eleven turns).
func TestSecretaryConversationDependenciesStayASet(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"记得。","used":["M1"],"show":[],"links":[],"actions":[]}`)
	})
	text := "去年关于成都的计划"
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: text})
	rawSource := st.Candidates[0].Source
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: text})

	claim := memory.Ref{ID: memory.ID(st.Memories[0].ID), Version: st.Memories[0].Version, Kind: memory.ClaimKind}
	source := memory.Ref{ID: memory.ID(rawSource.SourceID), Version: rawSource.Version, Kind: memory.SourceKind}
	assertSet := func(turnID string) {
		t.Helper()
		var refs []memory.Ref
		if err := s.pool.QueryRow(ctx, "SELECT dependencies FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), turnID).Scan(&refs); err != nil {
			t.Fatal(err)
		}
		counts := map[memory.Ref]int{}
		for _, ref := range refs {
			counts[ref]++
		}
		if len(refs) != 2 || counts[claim] != 1 || counts[source] != 1 {
			t.Fatalf("expected one claim and one supplied source, without duplicates: got=%+v claim=%+v source=%+v", refs, claim, source)
		}
	}

	count := func(turnID string) int {
		t.Helper()
		var n int
		if err := s.pool.QueryRow(ctx, "SELECT jsonb_array_length(dependencies) FROM desk_turns WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), turnID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var conversation *string
	var last workspace.DeskTurnResponse
	for i := 1; i <= 9; i++ {
		req := turnRequest(fmt.Sprintf("关于成都的计划，第 %d 次问", i))
		req.ConversationID = conversation
		last = mustTurn(t, s, scope, req)
		conversation = &last.ConversationID
		assertSet(last.Turn.ID)
	}

	// Rows written before the fix hold the same reference hundreds of times.
	// They must still verify, and must not inflate the turns that follow them.
	if _, err := s.pool.Exec(ctx, `UPDATE desk_turns SET dependencies=(SELECT jsonb_agg(t.dependencies->0) FROM desk_turns t, generate_series(1,600) WHERE t.owner_id=$1 AND t.id=$2) WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), last.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(last.Turn.ID); n != 600 {
		t.Fatal("legacy fixture not written", n)
	}
	turns, err := s.DeskTurns(ctx, scope, last.ConversationID)
	if err != nil || len(turns.Turns) != 9 {
		t.Fatal(err, len(turns.Turns))
	}
	if reply := turns.Turns[8].Reply; reply != "记得。" || strings.Contains(reply, "已变更") {
		t.Fatal("a still-valid legacy turn was hidden", reply)
	}
	req := turnRequest("关于成都的计划，再问一次")
	req.ConversationID = conversation
	next := mustTurn(t, s, scope, req)
	assertSet(next.Turn.ID)
}

func TestUniqueRefsKeepsFirstOccurrenceOrder(t *testing.T) {
	a := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
	b := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
	newer := memory.Ref{ID: a.ID, Version: 2, Kind: memory.ClaimKind}
	got := uniqueRefs([]memory.Ref{a, b, a, newer, b, a})
	if len(got) != 3 || got[0] != a || got[1] != b || got[2] != newer {
		t.Fatal(got)
	}
	if got := uniqueRefs(nil); got == nil || len(got) != 0 {
		t.Fatal("nil input must give an empty, non-nil list", got)
	}
}
