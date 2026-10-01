package postgres

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b1Extract(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, source memory.Ref, items ...map[string]any) []workspace.Memory {
	t.Helper()
	f.set(map[string]any{"items": items})
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source, "source.extract")); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return st.Memories
}
func b1ExtractItem(text, nature string) map[string]any {
	return map[string]any{"kind": "memory", "nature": nature, "text": text, "quote": text, "subject": "用户", "predicate": "验收计划", "acquisition": "direct", "confidence": 1, "explicit": true}
}
func b1MemoryRef(t *testing.T, memories []workspace.Memory, text string) memory.Ref {
	t.Helper()
	for _, m := range memories {
		if m.Text == text {
			return memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		}
	}
	t.Fatalf("extraction did not produce fixture memory %q", text)
	return memory.Ref{}
}
func b1Plan(t *testing.T, reply string) (*Store, memory.Scope, *b1Fake, workspace.DeskTurnRequest, workspace.DeskTurnResponse, memory.Ref) {
	t.Helper()
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, reply)
	req := turnRequest(b1Text(t, "plan", "text"))
	out := mustTurn(t, s, scope, req)
	source := b1TurnSource(t, s, scope, req.RequestID)
	return s, scope, f, req, out, source
}

const b1CreatePlan = `{"reply":"已安排","actions":[{"op":"create_task","title":"和张三对方案"}]}`

func TestPhase2B1_N1_UndoDeletesPlanKeepsOriginalAndHistory(t *testing.T) {
	s, scope, f, _, out, source := b1Plan(t, b1CreatePlan)
	text := b1Text(t, "plan", "claim")
	memories := b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan"))
	claim := b1MemoryRef(t, memories, text)
	b1Active(t, s, scope, claim, true)
	b1Undo(t, s, scope, b1ReceiptAction(t, out.Turn, 0))
	b1Active(t, s, scope, claim, false)
	b1AssertRetained(t, s, scope, source, out)
	b1AskAfterUndo(t, s, scope, f, text)
}
func TestPhase2B1_N2_ExtractionAfterFullUndoDoesNotRecreatePlan(t *testing.T) {
	s, scope, f, _, out, source := b1Plan(t, b1CreatePlan)
	b1Undo(t, s, scope, b1ReceiptAction(t, out.Turn, 0))
	text := b1Text(t, "plan", "claim")
	memories := b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan"))
	for _, m := range memories {
		if m.Text == text {
			t.Error("extraction recreated fully undone plan")
		}
	}
	b1AssertRetained(t, s, scope, source, out)
	b1AskAfterUndo(t, s, scope, f, text)
	// Positive control: the same plan in another non-undone turn is extracted.
	f.set(b1CreatePlan)
	nextReq := turnRequest(text)
	next := mustTurn(t, s, scope, nextReq)
	nextSource := b1TurnSource(t, s, scope, nextReq.RequestID)
	produced := b1Extract(t, s, scope, f, nextSource, b1ExtractItem(text, "plan"))
	ref := b1MemoryRef(t, produced, text)
	b1Active(t, s, scope, ref, true)
	b1AssertRetained(t, s, scope, nextSource, next)
}
func TestPhase2B1_N3_PartialUndoKeepsMemoryAndRawSupply(t *testing.T) {
	s, scope, f, _, out, source := b1Plan(t, `{"reply":"两件安排","actions":[{"op":"create_task","title":"和张三对方案"},{"op":"create_task","title":"准备成都资料"}]}`)
	text := b1Text(t, "plan", "claim")
	claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan")), text)
	b1Undo(t, s, scope, b1ReceiptAction(t, out.Turn, 0))
	b1Active(t, s, scope, claim, true)
	history := b1History(t, s, scope, out.ConversationID)
	if !history.Receipts[0].Undone || history.Receipts[1].Undone {
		t.Error("partial undo altered other action", history.Receipts)
	}
	f.set(`{"reply":"对照","actions":[]}`)
	req := turnRequest("周五张三方案是什么")
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, text)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, true)
	b1AssertRetained(t, s, scope, source, out)
}
func TestPhase2B1_N4_RememberKeepsPreferenceButDeletesPlan(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"记住且安排了","remember":true,"actions":[{"op":"create_task","title":"开会"}]}`)
	req := turnRequest(b1Text(t, "remember_plan", "text"))
	out := mustTurn(t, s, scope, req)
	source := b1TurnSource(t, s, scope, req.RequestID)
	preference, plan := b1Text(t, "remember_plan", "preference"), b1Text(t, "remember_plan", "plan")
	memories := b1Extract(t, s, scope, f, source, b1ExtractItem(preference, "preference"), b1ExtractItem(plan, "plan"))
	prefRef, planRef := b1MemoryRef(t, memories, preference), b1MemoryRef(t, memories, plan)
	b1Undo(t, s, scope, b1ReceiptAction(t, out.Turn, 0))
	b1Active(t, s, scope, prefRef, true)
	b1Active(t, s, scope, planRef, false)
	b1AssertRetained(t, s, scope, source, out)
	history := b1History(t, s, scope, out.ConversationID)
	remembered := false
	for _, r := range history.Receipts {
		if r.Op == "remember" {
			remembered = true
			if r.Undone {
				t.Error("remember incorrectly undone")
			}
		}
	}
	if !remembered {
		t.Error("remember receipt lost")
	}
	b1AskAfterUndo(t, s, scope, f, plan, out.Turn.Text)
}
func TestPhase2B1_N5_ConfirmedClaimSurvivesFullUndo(t *testing.T) {
	s, scope, f, _, out, source := b1Plan(t, b1CreatePlan)
	claim := b1Claim(t, s, scope, "用户确认周五张三方案", "plan", "confirmed", source)
	b1Undo(t, s, scope, b1ReceiptAction(t, out.Turn, 0))
	b1Active(t, s, scope, claim, true)
	b1AssertRetained(t, s, scope, source, out)
	f.set(`{"reply":"确认过的计划","actions":[]}`)
	req := turnRequest("用户确认周五张三方案")
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, "用户确认周五张三方案")
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), claim, true)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, false)
}
func TestPhase2B1_N6_IndependentEvidenceProtectsClaim(t *testing.T) {
	s, scope, f, _, out, source := b1Plan(t, b1CreatePlan)
	other := b1Source(t, s, scope, "快速记录", "另有会议证据", "manual")
	claim := b1Claim(t, s, scope, "周五张三多证据计划", "plan", "adopted", source, other)
	b1Undo(t, s, scope, b1ReceiptAction(t, out.Turn, 0))
	b1Active(t, s, scope, claim, true)
	b1Active(t, s, scope, other, true)
	b1AssertRetained(t, s, scope, source, out)
	f.set(`{"reply":"证据对照","actions":[]}`)
	req := turnRequest("周五张三多证据计划")
	mustTurn(t, s, scope, req)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), claim, true)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, false)
}
func TestPhase2B1_N7_RefusedUndoIsAtomic(t *testing.T) {
	for _, mode := range []string{"newer_action", "expired"} {
		t.Run(mode, func(t *testing.T) {
			s, scope, f, _, out, source := b1Plan(t, b1CreatePlan)
			text := b1Text(t, "plan", "claim")
			claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan")), text)
			action := b1ReceiptAction(t, out.Turn, 0)
			if mode == "newer_action" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: out.State.Tasks[0].ID, Title: "后续修改"})
			} else {
				if _, err := s.pool.Exec(context.Background(), "UPDATE action_log SET expired_at=now(),changes='[]' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action); err != nil {
					t.Fatal(err)
				}
			}
			b1RefusedUndo(t, s, scope, action, mode)
			b1Active(t, s, scope, claim, true)
			b1AssertRetained(t, s, scope, source, out)
			f.set(`{"reply":"仍可用","actions":[]}`)
			req := turnRequest("周五张三方案")
			mustTurn(t, s, scope, req)
			b1Contains(t, f.last(t).Prompt, text)
			b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, true)
		})
	}
}
func TestPhase2B1_N8_TelegramCallbackUsesRealUndo(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b1CreatePlan)
	bot := b1Telegram(t, s, scope)
	bot.message(1, 71, b1Text(t, "plan", "text"))
	b1Contains(t, bot.sent(t), "已安排")
	binding := bot.binding(t)
	out, err := s.DeskTurnByRequest(context.Background(), scope, binding.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	source := b1TurnSource(t, s, scope, binding.RequestID)
	text := b1Text(t, "plan", "claim")
	claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan")), text)
	bot.undo(t, b1ReceiptAction(t, out.Turn, 0))
	b1Active(t, s, scope, claim, false)
	b1AssertRetained(t, s, scope, source, out)
	b1AskAfterUndo(t, s, scope, f, text)
}
func TestPhase2B1_N9_UndoCommandReplayHasNoSecondDeletion(t *testing.T) {
	s, scope, f, _, out, source := b1Plan(t, b1CreatePlan)
	text := b1Text(t, "plan", "claim")
	claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan")), text)
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	cmd := workspace.Command{Type: "undoAction", ID: b1ReceiptAction(t, out.Turn, 0), RequestID: string(memory.NewID()), ExpectedRevision: st.Revision}
	first, err := s.Execute(context.Background(), scope, cmd)
	if err != nil {
		t.Fatal(err)
	}
	rowsBefore := b1DatabaseRows(t, s, false)
	replay, err := s.Execute(context.Background(), scope, cmd)
	if err != nil {
		t.Fatal("undo replay failed", err)
	}
	if first.Revision != replay.Revision || string(asJSON(first.Tasks)) != string(asJSON(replay.Tasks)) || string(asJSON(first.Activity)) != string(asJSON(replay.Activity)) {
		t.Error("undo replay returned different state")
	}
	rowsAfter := b1DatabaseRows(t, s, false)
	if !reflect.DeepEqual(rowsBefore, rowsAfter) {
		t.Error("undo replay mutated database or repeated deletion")
	}
	b1Active(t, s, scope, claim, false)
	b1AssertRetained(t, s, scope, source, out)
}
func TestPhase2B1_N10_ManualUndoDoesNotCascadeSecretaryMemory(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"已记录","actions":[]}`)
	req := turnRequest(b1Text(t, "plan", "text"))
	out := mustTurn(t, s, scope, req)
	source := b1TurnSource(t, s, scope, req.RequestID)
	text := b1Text(t, "plan", "claim")
	claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan")), text)
	_, action := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "手动新建"})
	b1Undo(t, s, scope, action)
	b1Active(t, s, scope, claim, true)
	b1AssertRetained(t, s, scope, source, out)
	f.set(`{"reply":"手动撤销对照","actions":[]}`)
	query := turnRequest("周五张三方案")
	mustTurn(t, s, scope, query)
	b1Contains(t, f.last(t).Prompt, text)
	b1HasRef(t, b1Refs(t, s, scope, query.RequestID), source, true)
}
func TestPhase2B1_N11_UndoUpdateThenCreationDeletesOnlyCompleteTurn(t *testing.T) {
	s, scope, f, _, first, source := b1Plan(t, `{"reply":"已安排","actions":[{"op":"create_task","title":"和张三对方案","due":"2099-10-02T15:00","remind":"none"}]}`)
	text := b1Text(t, "plan", "claim")
	claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan")), text)
	f.set(`{"reply":"改到四点","actions":[{"op":"update","ref":"R1","set":{"due":"2099-10-02T16:00","remind":"none"}}]}`)
	req := turnRequest("改到四点")
	req.ConversationID = &first.ConversationID
	second := mustTurn(t, s, scope, req)
	secondSource := b1TurnSource(t, s, scope, req.RequestID)
	restored := b1Undo(t, s, scope, b1ReceiptAction(t, second.Turn, 0))
	if restored.Tasks[0].Due != first.State.Tasks[0].Due {
		t.Error("undo update failed to restore original meeting time")
	}
	b1Active(t, s, scope, claim, true)
	f.set(`{"reply":"仍可供给","actions":[]}`)
	query := turnRequest("周五张三方案")
	mustTurn(t, s, scope, query)
	b1HasRef(t, b1Refs(t, s, scope, query.RequestID), source, true)
	b1Undo(t, s, scope, b1ReceiptAction(t, first.Turn, 0))
	b1Active(t, s, scope, claim, false)
	b1AssertRetained(t, s, scope, source, first)
	got, err := s.GetSource(context.Background(), scope, secondSource.ID, 0)
	if err != nil || got.Source.Text != second.Turn.Text {
		t.Error("second original lost", err)
	}
	b1AskAfterUndo(t, s, scope, f, text, "改到四点")
}
func TestPhase2B1_N12_RandomTwentyActionsInTwentySeededGroups(t *testing.T) {
	for group := 0; group < 20; group++ {
		seed := int64(20261001 + group)
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			t.Logf("reproduction seed=%d", seed)
			rng := rand.New(rand.NewSource(seed))
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, `{"reply":"random","actions":[]}`)
			initial := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "初始任务"})
			taskID := initial.Tasks[0].ID
			available := []string{taskID}
			type round struct {
				out           workspace.DeskTurnResponse
				source, claim memory.Ref
				remaining     int
			}
			rounds := []round{}
			type step struct {
				action string
				round  int
			}
			steps := []step{}
			for len(steps) < 20 {
				target := available[rng.Intn(len(available))]
				count := min(1+rng.Intn(3), 20-len(steps))
				actions := []map[string]any{}
				for i := 0; i < count; i++ {
					switch rng.Intn(3) {
					case 0:
						actions = append(actions, map[string]any{"op": "create_task", "title": fmt.Sprintf("seed%d-new%d", seed, len(steps)+i)})
					case 1:
						actions = append(actions, map[string]any{"op": "update", "ref": "THIS", "set": map[string]any{"title": fmt.Sprintf("seed%d-title%d", seed, len(steps)+i)}})
					case 2:
						actions = append(actions, map[string]any{"op": "add_steps", "ref": "THIS", "steps": []string{fmt.Sprintf("seed%d-step%d", seed, len(steps)+i)}})
					}
				}
				text := fmt.Sprintf("计划种子%d第%d轮周五准备方案", seed, len(rounds))
				f.set(map[string]any{"reply": "随机动作已办", "actions": actions})
				req := turnRequest(text)
				req.ThingID = &target
				out := mustTurn(t, s, scope, req)
				// Preserve creation order, independent of random UUID ordering in
				// Snapshot. A seed reproduces the operation/target sequence.
				for _, action := range actions {
					if action["op"] != "create_task" {
						continue
					}
					found := false
					for _, item := range out.State.Tasks {
						if item.Title == action["title"] {
							available = append(available, item.ID)
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("seed=%d created target missing", seed)
					}
				}
				source := b1TurnSource(t, s, scope, req.RequestID)
				claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, b1ExtractItem(text, "plan")), text)
				for i := 0; i < count; i++ {
					steps = append(steps, step{b1ReceiptAction(t, out.Turn, i), len(rounds)})
				}
				rounds = append(rounds, round{out, source, claim, count})
			}
			for i := len(steps) - 1; i >= 0; i-- {
				step := steps[i]
				b1Undo(t, s, scope, step.action)
				rounds[step.round].remaining--
				for _, r := range rounds {
					b1Active(t, s, scope, r.claim, r.remaining > 0)
				}
			}
			final, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatalf("seed=%d: %v", seed, err)
			}
			// Audit revisions/timestamps intentionally record undo. Compare all business
			// fields (including notes/checklist/status/sources), excluding audit metadata
			// and the monotonically increasing revision used to record the undo itself.
			business := func(item workspace.Item) map[string]any {
				m := b1Map(t, item)
				for _, key := range []string{"history", "evolution", "updatedAt", "recordVersion"} {
					delete(m, key)
				}
				return m
			}
			if len(final.Tasks) != len(initial.Tasks) {
				t.Errorf("seed=%d tasks not restored", seed)
			} else if string(asJSON(business(final.Tasks[0]))) != string(asJSON(business(initial.Tasks[0]))) {
				t.Errorf("seed=%d initial task business content not restored", seed)
			}
			if len(final.Ideas) != len(initial.Ideas) || len(final.Projects) != len(initial.Projects) {
				t.Errorf("seed=%d non-task objects changed", seed)
			}
			for _, r := range rounds {
				b1AssertRetained(t, s, scope, r.source, r.out)
			}
		})
	}
}
