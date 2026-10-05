package postgres_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Persist a historical answer using the existing public DTO and frozen
// dependency representation. Tests exercise dependency reads, not model output
// decoding or creation of new dependencies by the B4 answer pipeline.
func (f *phase25B234Fixture) seedAnswer(t *testing.T, refs ...memory.Ref) string {
	t.Helper()
	f.scope.Team = true
	if _, err := f.store.Snapshot(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	agent, err := json.Marshal(workspace.Agent{ID: "fictitious-agent", Name: "虚构副手", Enabled: true, MemoryInitialized: true, IncludeInferred: true, MemoryKinds: []string{"fact", "preference", "plan", "decision", "intention"}})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,'fictitious-agent',$2) ON CONFLICT(owner_id,id) DO UPDATE SET document=excluded.document`, f.scope.OwnerID, agent)
	for _, principal := range []string{"fictitious-agent", "agent:fictitious-agent"} {
		f.exec(t, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,$2 FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.scope.OwnerID, principal)
	}
	state, err := f.store.Snapshot(f.ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	conversation, turnID := string(memory.NewID()), string(memory.NewID())
	document, err := json.Marshal(workspace.DeskTurnResponse{ConversationID: conversation, Turn: workspace.SecretaryTurn{ID: turnID, Text: "虚构月报怎么办", Reply: "虚构回答：照便签办理。", Agent: "fictitious-agent", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, State: state})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := json.Marshal(refs)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO desk_turns(owner_id,id,agent_id,question,answer,dependencies,conversation_id,request_id,response) VALUES($1,$2,'fictitious-agent','虚构月报怎么办','虚构回答：照便签办理。',$3,$4,$5,$6)`, f.scope.OwnerID, turnID, deps, conversation, memory.NewID(), document)
	return conversation
}

func (f *phase25B234Fixture) assertAnswerOutdated(t *testing.T, conversation string, want bool) {
	t.Helper()
	turns, err := f.store.DeskTurns(f.ctx, f.scope, conversation)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns.Turns) != 1 {
		t.Fatalf("historical turns=%d, want 1", len(turns.Turns))
	}
	if turns.Turns[0].Outdated != want {
		t.Errorf("answer outdated=%v, want %v", turns.Turns[0].Outdated, want)
	}
}

func TestPhase25B2_X2_3_SupersededDependencyOutdated(t *testing.T) {
	f := phase25B2NewFixture(t)
	old := f.claim(t, "虚构月报周三交。")
	kept := f.claim(t, "虚构月报改到周五交。")
	conversation := f.seedAnswer(t, old)
	f.assertAnswerOutdated(t, conversation, false)
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构依赖月报")), Type: "project"}
	f.labels(t, old, "progress", true, 1, g)
	f.labels(t, kept, "progress", true, 1, g)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		out.Superseded = []phase25B2WireSuperseded{{Old: phase25B2N(in, "虚构月报周三交。"), New: phase25B2N(in, "虚构月报改到周五交。")}}
		return out
	})
	f.runCompare(t)
	f.assertAnswerOutdated(t, conversation, true)
	f.assertRevisions(t, old, kept)
}

func TestPhase25B2_X2_4_DuplicateDependencyStaysCurrent(t *testing.T) {
	f := phase25B2NewFixture(t)
	old := f.claim(t, "虚构月报周五交。")
	kept := f.claim(t, "虚构月报周五提交。")
	conversation := f.seedAnswer(t, old)
	f.assertAnswerOutdated(t, conversation, false)
	g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构依赖月报")), Type: "project"}
	f.labels(t, old, "progress", true, 1, g)
	f.labels(t, kept, "progress", true, 1, g)
	f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
		out := phase25B2Empty()
		keep := phase25B2N(in, "虚构月报周五提交。")
		out.Duplicates = []phase25B2WireDuplicate{{Keep: keep, Members: []int{phase25B2N(in, "虚构月报周五交。"), keep}}}
		return out
	})
	f.runCompare(t)
	f.assertAnswerOutdated(t, conversation, false)
	f.assertRevisions(t, old, kept)
	// Positive control: the same persisted dependency must detect a revision
	// change, so a malformed or ignored cache cannot produce a false pass.
	f.correct(t, old, "虚构验收纠正：月报周六交。")
	f.assertAnswerOutdated(t, conversation, true)
}
