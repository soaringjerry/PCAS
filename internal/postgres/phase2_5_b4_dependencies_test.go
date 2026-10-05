package postgres_test

import (
	"net/http"
	"testing"
)

func TestPhase25B4_ActualDeskTurnThenCompareDependencySemantics(t *testing.T) {
	for _, kind := range []string{"duplicate", "superseded"} {
		t.Run(kind, func(t *testing.T) {
			f := phase25B2NewFixture(t)
			texts := []string{"虚构办事旧期限周三。", "虚构办事新期限周五。", "虚构办事统一使用蓝色封面。"}
			g, refs := f.groupTexts(t, texts...)
			f.card(t, g, refs, false)
			f.model(t, func(_ *http.Request, _ int, _ phase25B234ModelRequest) phase25B234ModelReply {
				return phase25B4ReplyJSON(phase25B4Reply{text: "虚构实际秘书回答。", used: []string{"M1"}}, false)
			})
			turn := f.secretaryTurn(t, "处理"+g.Name, "")
			f.assertAnswerOutdated(t, turn.ConversationID, false)
			f.compareModel(t, func(in phase25B2Input) phase25B2WireOutput {
				out := phase25B2Empty()
				old, next := phase25B2N(in, texts[0]), phase25B2N(in, texts[1])
				if kind == "duplicate" {
					out.Duplicates = []phase25B2WireDuplicate{{Keep: next, Members: []int{old, next}}}
				} else {
					out.Superseded = []phase25B2WireSuperseded{{Old: old, New: next}}
				}
				return out
			})
			f.runCompare(t)
			f.assertRetirement(t, refs[0], kind, refs[1])
			f.assertAnswerOutdated(t, turn.ConversationID, kind == "superseded")
			f.assertRevisions(t, refs...)
		})
	}
}
