package postgres

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase26C7EffectiveSignalsChangeRankingAtScale(t *testing.T) {
	f := phase26LoadFixture(t)
	refs := []memory.Ref{}
	for i := 0; i < 8; i++ {
		ref := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
		refs = append(refs, ref)
		in := memory.CommitRequest{RequestID: memory.NewID(), Claims: []memory.Claim{{Revision: memory.Revision{Ref: ref, State: "active"}, SubjectID: f.Entities[998], Predicate: "fictitious_activity", Value: asJSON(fmt.Sprintf("FictitiousSignalRank ceramic record %d", i)), Nature: "fact", Acquisition: "direct", Confirmation: "confirmed"}}, Evidence: []memory.Evidence{{ID: memory.NewID(), Source: f.Sources[0], Target: ref, Acquisition: "direct", Stance: "supports"}}}
		if _, err := f.Store.Commit(f.Context, f.Scope, in); err != nil {
			t.Fatal(err)
		}
		j := worker.Job{OwnerID: f.Scope.OwnerID, Record: ref, Stage: "memory.index", LeaseToken: memory.NewID()}
		if err := f.Store.pool.QueryRow(f.Context, `UPDATE memory_jobs SET state='leased',lease_token=$4,lease_until=now()+interval '5 minutes',attempts=attempts+1 WHERE owner_id=$1 AND record_id=$2 AND record_version=1 AND stage=$3 RETURNING id,attempts`, j.OwnerID, ref.ID, j.Stage, j.LeaseToken).Scan(&j.ID, &j.Attempts); err != nil {
			t.Fatal(err)
		}
		if err := f.Store.ProcessIndex(f.Context, j); err != nil {
			t.Fatal(err)
		}
	}
	phase26Exec(t, f, `ANALYZE record_search`)
	req := memory.RecallRequest{Query: "FictitiousSignalRank", Mode: memory.Remember, Budget: memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}}
	off, err := f.Store.Recall(WithActivityRanking(f.Context, false), f.Scope, req)
	if err != nil || len(off.Memories) != 8 {
		t.Fatalf("ranking control=%+v err=%v", off.Memories, err)
	}
	target := off.Memories[7]
	for _, kind := range []string{"user_mention", "adoption"} {
		if err := f.Store.RecordUse(f.Context, f.Scope, memory.UseEvent{Ref: target, EventID: "fictitious-" + kind, Kind: kind, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	on, err := f.Store.Recall(WithActivityRanking(f.Context, true), f.Scope, req)
	if err != nil || len(on.Memories) != 8 || on.Memories[0].ID != target.ID {
		t.Fatalf("signals did not rank last control item first: on=%+v err=%v", on.Memories, err)
	}
	again, err := f.Store.Recall(WithActivityRanking(f.Context, false), f.Scope, req)
	if err != nil || len(again.Memories) != 8 || again.Memories[7].ID != target.ID {
		t.Fatal("ranking disabled did not restore baseline")
	}
	var mentions, adoptions int
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FILTER(WHERE kind='user_mention'),count(*) FILTER(WHERE kind='adoption') FROM use_events WHERE owner_id=$1 AND record_id=$2`, f.Scope.OwnerID, target.ID).Scan(&mentions, &adoptions); err != nil || mentions != 1 || adoptions != 1 {
		t.Fatalf("signal counts=%d/%d err=%v", mentions, adoptions, err)
	}
	t.Logf("same fictitious target ranks 8th with signals off, 1st with signals on; stored mentions=%d adoptions=%d", mentions, adoptions)
	// Make the eight signal subjects current requirements so every turn sees
	// them independently of lexical retrieval and its candidate-slot budget.
	refIDs := []memory.ID{}
	for _, r := range refs {
		refIDs = append(refIDs, r.ID)
	}
	phase26Exec(t, f, `UPDATE claim_revisions SET category='rule' WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])`, f.Scope.OwnerID, refIDs)
	phase26Exec(t, f, `INSERT INTO assistant_requirements(owner_id,claim_id,claim_version,unrestricted,scope) SELECT owner_id,claim_id,version,true,'' FROM claim_revisions WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])`, f.Scope.OwnerID, refIDs)
	m := phase26NewModel(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	index := 0
	for i, r := range refs {
		if r.ID == target.ID {
			index = i
		}
	}
	note := fmt.Sprintf("FictitiousSignalRank ceramic record %d", index)
	pattern := regexp.MustCompile(`\[(M[0-9]+)[^\]]*\] ` + regexp.QuoteMeta(note))
	turnNumber := 0
	m.mu.Lock()
	m.Reply = func(c phase26Call) string {
		if !strings.Contains(c.System, "你是用户的前台秘书") {
			return m.goldReply(c)
		}
		m.mu.Lock()
		turnNumber++
		n := turnNumber
		m.mu.Unlock()
		match := pattern.FindStringSubmatch(c.Prompt)
		alias := ""
		if len(match) == 2 {
			alias = match[1]
		}
		if alias == "" {
			return `{"reply":"Fictitious missing signal subject in prompt.","actions":[],"used":[]}`
		}
		mentioned, adopted := []string{}, []string{}
		if n == 1 {
			mentioned = append(mentioned, alias)
		} else {
			adopted = append(adopted, alias)
		}
		b, _ := json.Marshal(map[string]any{"reply": "Fictitious accepted signal answer.", "used": []string{alias}, "actions": []any{}, "memoryPlan": map[string]any{"depth": "light", "groups": []string{}, "mentioned": mentioned, "adopted": adopted}})
		return string(b)
	}
	m.mu.Unlock()
	firstReq := workspace.DeskTurnRequest{AgentID: "phase26", RequestID: string(memory.NewID()), Text: "Please explain " + note}
	first, err := f.Store.DeskTurn(WithMemoryTier(f.Context, "light"), f.Scope, firstReq)
	if err != nil || first.Turn.Reply != "Fictitious accepted signal answer." {
		t.Fatalf("actual mention turn failed: %v %+v", err, first.Turn)
	}
	secondReq := workspace.DeskTurnRequest{AgentID: "phase26", RequestID: string(memory.NewID()), ConversationID: &first.ConversationID, Text: "I accept the previous answer about " + note}
	if _, err := f.Store.DeskTurn(WithMemoryTier(f.Context, "light"), f.Scope, secondReq); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Store.DeskTurn(WithMemoryTier(f.Context, "light"), f.Scope, firstReq); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.pool.QueryRow(f.Context, `SELECT count(*) FILTER(WHERE kind='user_mention'),count(*) FILTER(WHERE kind='adoption') FROM use_events WHERE owner_id=$1 AND record_id=$2`, f.Scope.OwnerID, target.ID).Scan(&mentions, &adoptions); err != nil || mentions != 2 || adoptions != 2 || len(m.calls("secretary")) != 2 {
		t.Fatalf("actual turns/replay signal counts=%d/%d calls=%d err=%v", mentions, adoptions, len(m.calls("secretary")), err)
	}
	t.Log("actual secretary mention/adoption add exactly one signal each; request replay adds neither call nor signal")
}
