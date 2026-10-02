package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b1AnsweredClaim(t *testing.T) (*Store, memory.Scope, *b1Fake, memory.Ref, workspace.DeskTurnRequest, workspace.DeskTurnResponse) {
	t.Helper()
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"陈述原回答保留","used":["M1"],"actions":[{"op":"create_task","title":"原回执事项"}]}`)
	src := b1Source(t, s, scope, "快速记录", "成都陈述依据", "memory-input")
	claim := b1Claim(t, s, scope, "成都陈述旧说法蓝灯", "fact", "adopted", src)
	req := turnRequest("成都陈述旧说法是什么")
	out := mustTurn(t, s, scope, req)
	if len(out.Turn.Cards) == 0 || len(out.Turn.Receipts) == 0 {
		t.Fatal("fixture requires cards and receipts")
	}
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), claim, true)
	return s, scope, f, claim, req, out
}
func TestPhase2B1_M1_ClaimCorrectionKeepsVisibleExchange(t *testing.T) {
	s, scope, _, claim, _, out := b1AnsweredClaim(t)
	b1Outdated(t, b1History(t, s, scope, out.ConversationID), false)
	b1Correct(t, s, scope, claim, "成都陈述新说法红灯")
	b1Preserved(t, out.Turn, b1History(t, s, scope, out.ConversationID))
}
func TestPhase2B1_M2_ModelHistoryReplacesOnlyOldAnswer(t *testing.T) {
	s, scope, f, claim, _, out := b1AnsweredClaim(t)
	b1Correct(t, s, scope, claim, "成都陈述新说法红灯")
	f.set(`{"reply":"当前新回答","actions":[]}`)
	req := turnRequest("继续回答")
	req.ConversationID = &out.ConversationID
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, out.Turn.Text, "（先前回答的依据已更新，请按现在的资料回答）")
	b1Absent(t, f.last(t).Prompt, out.Turn.Reply)
	for _, receipt := range out.Turn.Receipts {
		b1Contains(t, f.last(t).Prompt, receipt.Text)
	}
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), claim, false)
	// Delegation from the same discussion must apply the same model-history rule.
	f.set(`{"reply":"交给副手","actions":[{"op":"delegate","ref":"R1","kind":"ask","prompt":"根据当前成都资料作答"}]}`)
	req = turnRequest("让副手分析成都资料")
	req.ConversationID = &out.ConversationID
	delegated := mustTurn(t, s, scope, req)
	if len(delegated.State.Runs) == 0 {
		t.Fatal("no delegated run")
	}
	run := delegated.State.Runs[0]
	b1Contains(t, run.Brief, out.Turn.Text, "（先前回答的依据已更新，请按现在的资料回答）")
	b1Absent(t, run.Brief, out.Turn.Reply)
	for _, receipt := range out.Turn.Receipts {
		b1Contains(t, run.Brief, receipt.Text)
	}
	b1HasRef(t, run.ContextVersions, claim, false)
	f.set("当前副手回答")
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	b1Contains(t, f.last(t).Prompt, "（先前回答的依据已更新，请按现在的资料回答）")
	b1Absent(t, f.last(t).Prompt, out.Turn.Reply)
	for _, receipt := range out.Turn.Receipts {
		b1Contains(t, f.last(t).Prompt, receipt.Text)
	}
}
func TestPhase2B1_M3_SourceVersionMarksOutdatedWithoutErasure(t *testing.T) {
	s, scope := b1Store(t), owner()
	b1Model(t, s, `{"reply":"原话旧回答","used":["S1"],"actions":[{"op":"create_task","title":"原话回执"}]}`)
	in := memory.IngestRequest{Connector: "manual", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: b1Text(t, "versioned", "old"), MediaType: "text/plain"}
	src := mustIngest(t, s, scope, in)
	req := turnRequest("成都版本吃什么")
	out := mustTurn(t, s, scope, req)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), src.Ref, true)
	b1Outdated(t, out.Turn, false)
	in.ExternalVersion = "2"
	in.Text = b1Text(t, "versioned", "current")
	mustIngest(t, s, scope, in)
	b1Preserved(t, out.Turn, b1History(t, s, scope, out.ConversationID))
}
func TestPhase2B1_M4_SourceDeletionScrubsDependentTurnRunAndDoc(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"原话旧回答","used":["S1"],"actions":[]}`)
	src := b1Trip(t, s, scope)
	req := turnRequest("我去成都吃什么")
	out := mustTurn(t, s, scope, req)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), src, true)
	f.set("成都副手原结果")
	run := b1Run(t, s, scope, "model", "成都想吃什么")
	st, snapErr := s.Snapshot(context.Background(), scope)
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if len(st.Docs) == 0 {
		t.Fatal("fixture has no adopted document")
	}
	b1Delete(t, s, scope, src)
	turn := b1History(t, s, scope, out.ConversationID)
	if turn.Reply != "（这条回答依据的记忆已变更）" || len(turn.Cards) != 0 {
		t.Error("source deletion did not scrub dependent exchange", turn)
	}
	b1Absent(t, string(asJSON(turn)), out.Turn.Reply, b1Text(t, "trip", "text"))
	w := b1HTTP(t, s, scope, "GET", "/v1/memory/sources/"+string(src.ID), nil)
	if w.Code != 404 {
		t.Error("deleted source still accessible", w.Code)
	}
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range st.Runs {
		if r.ID == run.ID {
			t.Error("deleted source retained dependent run")
		}
	}
	for _, d := range st.Docs {
		if d.Body == run.Output {
			t.Error("dependent document survived source deletion")
		}
	}
	b1AskAfterUndo(t, s, scope, f, b1Text(t, "trip", "text"))
}
func TestPhase2B1_M5_OutdatedReceiptCanUndoAndSurvivesReload(t *testing.T) {
	s, scope, _, claim, _, out := b1AnsweredClaim(t)
	b1Correct(t, s, scope, claim, "成都陈述新说法红灯")
	before := b1History(t, s, scope, out.ConversationID)
	b1Preserved(t, out.Turn, before)
	b1Undo(t, s, scope, b1ReceiptAction(t, before, 0))
	after := b1History(t, s, scope, out.ConversationID)
	if after.Reply != out.Turn.Reply || string(asJSON(after.Cards)) != string(asJSON(out.Turn.Cards)) || !after.Receipts[0].Undone {
		t.Error("undo after outdated not retained", after)
	}
	b1Outdated(t, after, true)
	again := b1History(t, s, scope, out.ConversationID)
	if !again.Receipts[0].Undone {
		t.Error("undone flag lost on reload")
	}
}
func TestPhase2B1_M6_ReplayAndTelegramDuplicateKeepOldAnswer(t *testing.T) {
	t.Run("requestReplay", func(t *testing.T) {
		s, scope, f, claim, req, out := b1AnsweredClaim(t)
		b1Correct(t, s, scope, claim, "成都陈述新说法红灯")
		w := b1HTTP(t, s, scope, "POST", "/v1/desk/turn", req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var replay workspace.DeskTurnResponse
		if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil {
			t.Fatal(err)
		}
		b1Preserved(t, out.Turn, replay.Turn)
		if replay.Turn.ID != out.Turn.ID || len(f.all()) != 1 || len(replay.State.Tasks) != 1 {
			t.Error("replay executed again")
		}
		cached, err := s.DeskTurnByRequest(context.Background(), scope, req.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		b1Preserved(t, out.Turn, cached.Turn)
	})
	t.Run("telegramRedelivery", func(t *testing.T) {
		s, scope := b1Store(t), owner()
		f := b1Model(t, s, `{"reply":"Telegram原回答保留","used":["M1"],"actions":[{"op":"create_task","title":"Telegram一次事项"}]}`)
		src := b1Source(t, s, scope, "快速记录", "成都 Telegram 陈述依据", "memory-input")
		claim := b1Claim(t, s, scope, "成都 Telegram 旧陈述", "fact", "adopted", src)
		bot := b1Telegram(t, s, scope)
		bot.message(1, 71, "成都 Telegram 旧陈述是什么")
		sent := bot.sent(t)
		b1Contains(t, sent, "Telegram原回答保留")
		binding := bot.binding(t)
		before, err := s.DeskTurnByRequest(context.Background(), scope, binding.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		b1Correct(t, s, scope, claim, "成都 Telegram 新陈述")
		bot.message(2, 71, "成都 Telegram 旧陈述是什么")
		resent := bot.sent(t)
		b1Contains(t, resent, "Telegram原回答保留")
		b1Absent(t, resent, "（这条回答依据的记忆已变更）")
		after, err := s.DeskTurnByRequest(context.Background(), scope, binding.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		b1Preserved(t, before.Turn, after.Turn)
		if len(f.all()) != 1 || len(after.State.Tasks) != 1 {
			t.Error("Telegram duplicate reran model/actions")
		}
	})
}
func TestPhase2B1_M7_CorrectedDeputyResultRemainsButStale(t *testing.T) {
	s, scope := b1Store(t), owner()
	b1Model(t, s, "成都副手固定结果")
	src := b1Source(t, s, scope, "快速记录", "成都副手陈述依据", "memory-input")
	claim := b1Claim(t, s, scope, "成都副手旧陈述", "fact", "adopted", src)
	run := b1Run(t, s, scope, "model", "成都副手旧陈述")
	if run.StaleContext {
		t.Fatal("fresh result incorrectly stale")
	}
	st, snapErr := s.Snapshot(context.Background(), scope)
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	b1Correct(t, s, scope, claim, "成都副手新陈述")
	after, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range after.Runs {
		if r.ID == run.ID {
			found = true
			if r.Output != run.Output || !r.StaleContext {
				t.Error("stale result erased or unmarked", r)
			}
		}
	}
	if !found {
		t.Error("stale run deleted")
	}
	if len(after.Docs) != len(st.Docs) {
		t.Error("correction deleted result document")
	}
}
func TestPhase2B1_M8_UnchangedTurnOmitsOutdatedField(t *testing.T) {
	s, scope, f, _, req, out := b1AnsweredClaim(t)
	b1Outdated(t, out.Turn, false)
	b1Outdated(t, b1History(t, s, scope, out.ConversationID), false)
	replay := mustTurn(t, s, scope, req)
	b1Outdated(t, replay.Turn, false)
	if replay.Turn.Reply != out.Turn.Reply || len(f.all()) != 1 {
		t.Error("unchanged replay altered answer or regenerated")
	}
}

func TestPhase2B1_M2_CurrentDestinationUnavailablePreservesQuestionAndReceipt(t *testing.T) {
	for _, change := range []string{"exclude", "invisible"} {
		t.Run(change, func(t *testing.T) {
			s, scope, f, claim, _, out := b1AnsweredClaim(t)
			task := *out.Turn.Receipts[0].ThingID
			if change == "exclude" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: task, MemoryID: string(claim.ID)})
			} else {
				workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(claim.ID), AgentIDs: []string{}})
			}
			f.set("当前场合副手回答")
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task, AgentID: "model", Kind: "ask", Prompt: "根据当前可用资料继续", DeskTurnIDs: []string{out.Turn.ID}})
			run := st.Runs[0]
			b1Contains(t, run.Brief, out.Turn.Text, "（先前回答的依据已更新，请按现在的资料回答）")
			b1Absent(t, run.Brief, out.Turn.Reply)
			for _, receipt := range out.Turn.Receipts {
				b1Contains(t, run.Brief, receipt.Text)
			}
			b1HasRef(t, run.ContextVersions, claim, false)
			if err := s.runAgentOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			b1Contains(t, f.last(t).Prompt, out.Turn.Text, "（先前回答的依据已更新，请按现在的资料回答）")
			b1Absent(t, f.last(t).Prompt, out.Turn.Reply, "成都陈述旧说法蓝灯")
			for _, receipt := range out.Turn.Receipts {
				b1Contains(t, f.last(t).Prompt, receipt.Text)
			}
			visible := b1History(t, s, scope, out.ConversationID)
			if visible.Reply != out.Turn.Reply || string(asJSON(visible.Cards)) != string(asJSON(out.Turn.Cards)) || string(asJSON(visible.Receipts)) != string(asJSON(out.Turn.Receipts)) {
				t.Error("current destination scope erased user-visible exchange")
			}
		})
	}
}
