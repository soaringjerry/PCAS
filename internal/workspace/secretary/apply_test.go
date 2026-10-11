package secretary

import (
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestATurnDoesTenActionsAndTellsTheOwnerAboutTheRest(t *testing.T) {
	actions := []Action{}
	for i := 0; i < 13; i++ {
		actions = append(actions, Action{Op: "create_task"})
	}
	actions[2].SelfcheckDropped = true
	actions[4].ParseErr = memory.ErrInvalid
	actions[11].SelfcheckDropped = true
	steps, over := PlanActions(actions)
	// Positions count each action of the answer, also a dropped one.
	if len(steps) != 10 || over != 2 || steps[2].Index != 3 || steps[9].Index != 10 {
		t.Fatalf("%d %d %+v", len(steps), over, steps)
	}
	if steps[3].Skipped == nil || steps[3].Skipped.Reason != "动作字段没看懂" || steps[0].Skipped != nil {
		t.Fatalf("%+v", steps[3])
	}
	if last := steps[9]; last.Skipped == nil || last.Skipped.Reason != "一次太多了，只做了前 10 件" {
		t.Fatalf("%+v", last)
	}
	if few, over := PlanActions(actions[:3]); len(few) != 2 || over != 0 {
		t.Fatal(len(few), over)
	}
}

func TestAnUnfinishedActionKeepsItsOwnReason(t *testing.T) {
	own := Skipped("update", "找不到要改的那件事")
	if got := Unfinished("update", own, nil); got.Reason != "找不到要改的那件事" {
		t.Fatal(got)
	}
	if got := Unfinished("update", own, memory.ErrInvalid); got.Reason != "找不到要改的那件事" {
		t.Fatal(got)
	}
	if got := Unfinished("delegate", workspace.DeskReceipt{Op: "delegate", Status: "done"}, errors.New("fictitious failure")); got.Status != "skipped" || got.Reason != "这件事暂时办不了" {
		t.Fatal(got)
	}
	if got := Unfinished("delegate", own, workspace.ErrBudget); got.Reason != "超过今天的额度" {
		t.Fatal(got)
	}
	if NothingChanged("update").Reason != "没有可执行的修改" {
		t.Fatal("nothing changed")
	}
	if CreatedAlias("create_task", 0) != "N1" || CreatedAlias("create_project", 4) != "N5" || CreatedAlias("update", 0) != "" {
		t.Fatal("only a creation gives an alias")
	}
	if Reply("  好的 \n", nil) != "好的" || Reply("好的", []string{"虚构组", "另一组"}) != "好的\n这几组没来得及看：虚构组、另一组" {
		t.Fatal("reply")
	}
}

func TestAMemoryIsAdoptedOnlyWhenTheTurnBeforeCitedIt(t *testing.T) {
	sent := map[string]workspace.Memory{"M1": {ID: "a"}, "M2": {ID: "b"}}
	last := &workspace.SecretaryTurn{Cards: []workspace.DeskCard{{Kind: "sources", Items: []workspace.DeskSourceItem{{MemoryID: "a"}}}, {Kind: "timeline", Items: []workspace.DeskSourceItem{{MemoryID: "b"}}}}}
	plan := &UsePlan{Adopted: []string{"M1", "M2", "M9"}, Mentioned: []string{"M2", "M9"}}
	if got := Adopted(plan, sent, last); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("%+v", got)
	}
	if len(Adopted(plan, sent, nil)) != 0 || len(Adopted(nil, sent, last)) != 0 {
		t.Fatal("no turn before, or no plan, adopts nothing")
	}
	if got := Mentioned(plan, sent); len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("%+v", got)
	}
}
