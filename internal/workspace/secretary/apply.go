package secretary

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// ActionLimit is the count of actions that one turn does. One sentence of
// the owner asks for a few things; a longer list is a sign of a wrong answer,
// so the rest is not done and the owner is told.
const ActionLimit = 10

// Step is one action of the answer, in its position. Skipped is the receipt
// of an action that is not tried.
type Step struct {
	// Index is the position of the action in the answer. A created item gets
	// the alias N<Index+1> for the later actions of the same turn.
	Index   int
	Action  Action
	Skipped *workspace.DeskReceipt
}

// PlanActions returns the steps of an answer. An action that the self-check
// dropped gives no step. An action whose fields were not read is skipped with
// its name. The action at the limit is skipped, and no later action is
// planned; overLimit is the count of actions that were left out.
func PlanActions(actions []Action) (steps []Step, overLimit int) {
	for i, a := range actions {
		if a.SelfcheckDropped {
			continue
		}
		if i >= ActionLimit {
			for _, rest := range actions[i:] {
				if !rest.SelfcheckDropped {
					overLimit++
				}
			}
			receipt := Skipped(a.Op, "一次太多了，只做了前 10 件")
			return append(steps, Step{Index: i, Action: a, Skipped: &receipt}), overLimit
		}
		step := Step{Index: i, Action: a}
		if a.ParseErr != nil {
			receipt := Skipped(a.Op, "动作字段没看懂")
			step.Skipped = &receipt
		}
		steps = append(steps, step)
	}
	return steps, 0
}

// Unfinished is the receipt of an action that returned an error or a skipped
// receipt. A skipped receipt keeps its own reason.
func Unfinished(op string, receipt workspace.DeskReceipt, err error) workspace.DeskReceipt {
	if errors.Is(err, workspace.ErrBudget) {
		return Skipped(op, "超过今天的额度")
	}
	if err != nil && receipt.Reason == "" {
		return Skipped(op, "这件事暂时办不了")
	}
	return receipt
}

// NothingChanged is the receipt of an action that ran and changed nothing.
func NothingChanged(op string) workspace.DeskReceipt { return Skipped(op, "没有可执行的修改") }

// CreatedAlias is the alias of an item that the action at index made, or
// empty when the action makes no item.
func CreatedAlias(op string, index int) string {
	if !oneOf(op, "create_task", "create_idea", "create_project") {
		return ""
	}
	return fmt.Sprintf("N%d", index+1)
}

// Reply is the text that the owner reads: the answer, and the groups of
// memory that the turn did not read.
func Reply(reply string, notRead []string) string {
	reply = strings.TrimSpace(reply)
	if len(notRead) > 0 {
		reply += "\n这几组没来得及看：" + strings.Join(notRead, "、")
	}
	return reply
}

// Adopted returns the memories that the owner took up from the turn before:
// the answer names them, this turn showed them, and a source card of the
// turn before cited them.
func Adopted(plan *UsePlan, sent map[string]workspace.Memory, last *workspace.SecretaryTurn) []workspace.Memory {
	out := []workspace.Memory{}
	if plan == nil || last == nil {
		return out
	}
	for _, alias := range plan.Adopted {
		m, ok := sent[alias]
		if !ok {
			continue
		}
		referenced := false
		for _, card := range last.Cards {
			if card.Kind != "sources" {
				continue
			}
			raw, _ := json.Marshal(card.Items)
			var items []workspace.DeskSourceItem
			if json.Unmarshal(raw, &items) == nil {
				for _, item := range items {
					referenced = referenced || item.MemoryID == m.ID
				}
			}
		}
		if referenced {
			out = append(out, m)
		}
	}
	return out
}

// Mentioned returns the shown memories that the owner's sentence mentions.
func Mentioned(plan *UsePlan, sent map[string]workspace.Memory) []workspace.Memory {
	out := []workspace.Memory{}
	if plan == nil {
		return out
	}
	for _, alias := range plan.Mentioned {
		if m, ok := sent[alias]; ok {
			out = append(out, m)
		}
	}
	return out
}
