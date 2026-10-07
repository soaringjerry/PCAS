package postgres

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestSecretaryMentionSurvivesConcurrentMemoryRevision(t *testing.T) {
	s, scope := testStore(t), owner()
	var target workspace.Memory
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: target.ID, Text: "虚构并发修正后的蓝色便签"})
		secretaryModelReply(w, `{"reply":"虚构回答保留。","used":["M1"],"actions":[],"memoryPlan":{"depth":"light","groups":[],"mentioned":["M1"],"adopted":[]}}`)
	})
	target = b4Memory(t, s, scope, "虚构主动提及蓝色便签")
	req := turnRequest("虚构主动提及蓝色便签")
	out := mustTurn(t, s, scope, req)
	if out.Turn.Reply != "虚构回答保留。" {
		t.Fatal(out.Turn)
	}
	var events int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM use_events WHERE owner_id=$1 AND record_id=$2 AND kind='user_mention'", scope.OwnerID, target.ID).Scan(&events); err != nil || events != 1 {
		t.Fatal("mention lost after unrelated revision", events, err)
	}
	replay := mustTurn(t, s, scope, req)
	if replay.Turn.ID != out.Turn.ID {
		t.Fatal("replay changed answer", replay.Turn)
	}
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM use_events WHERE owner_id=$1 AND record_id=$2 AND kind='user_mention'", scope.OwnerID, target.ID).Scan(&events); err != nil || events != 1 {
		t.Fatal("replay duplicated mention", events, err)
	}
}

func TestActivityRankingUsesEffectiveSignals(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, `{"actions":[]}`) })
	source := b1Source(t, s, scope, "虚构排序依据", "虚构排序的不同依据", "manual")
	for i := 0; i < 8; i++ {
		b1Claim(t, s, scope, "Fictitious ranking record "+strings.Repeat("x", i+1), "fact", "confirmed", source)
	}
	req := memory.RecallRequest{Query: "Fictitious", Mode: memory.Remember, Budget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}}
	off, err := s.Recall(WithActivityRanking(ctx, false), scope, req)
	if err != nil || len(off.Memories) != 8 {
		t.Fatal(off, err)
	}
	target := off.Memories[7]
	if err := s.RecordUse(ctx, scope, memory.UseEvent{Ref: target, EventID: "fictional-effective-use", Kind: "user_mention", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	on, err := s.Recall(WithActivityRanking(ctx, true), scope, req)
	if err != nil || len(on.Memories) != 8 || on.Memories[0].ID != target.ID {
		t.Fatal("effective-use signal failed to affect ordering", on, err)
	}
	offAgain, err := s.Recall(WithActivityRanking(ctx, false), scope, req)
	if err != nil || offAgain.Memories[7].ID != target.ID {
		t.Fatal("disabled effective-use ranking changed ordering", offAgain, err)
	}
}
