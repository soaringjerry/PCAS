package telegram

import (
	"testing"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestApprovedUndoNewerActionCallback(t *testing.T) {
	p, s, b := fixture(t)
	s.undoErr = workspace.ErrNewerAction
	b.enqueue(textUpdate(1, "可撤销事项"))
	step(t, p)
	id := *s.turns[0].Receipts[0].ActionID
	b.enqueue(callbackUpdate(2, "u:"+id, 101))
	step(t, p)
	answers := b.of("answerCallbackQuery")
	if len(s.undoIDs) != 1 || s.undoIDs[0] != id || len(answers) != 1 || decode[string](answers[0].body["text"]) != "后面还有改动，请先撤销它" {
		t.Fatal("wrong newer_action response", answers)
	}
}
