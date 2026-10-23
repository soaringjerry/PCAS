package deputy

import (
	"testing"

	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestARunReservesTheInstructionsItIsAnsweredWith(t *testing.T) {
	for _, kind := range []string{"breakdown", "revise"} {
		request := AnswerRequest("o", "t", Snapshot{Run: workspace.Run{ID: "r", Kind: kind, Brief: "简报"}})
		if CostInput(kind, "简报") != request.Instructions.Text()+"简报" {
			t.Fatalf("%s reserves other instructions than its answer uses", kind)
		}
	}
	if instruction, _ := Instructions("revise"); instruction.Text() != prompts.Must("document-revise").Text() {
		t.Fatal("a revision is not answered with its own instructions")
	}
}
