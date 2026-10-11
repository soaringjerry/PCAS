package secretary

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func intPointerValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// NeedsCheck reports whether an answer must be checked before it is applied:
// it holds an action that deletes or sends.
func NeedsCheck(out Output) bool {
	for _, a := range out.Actions {
		if strings.Contains(a.Op, "delete") || strings.Contains(a.Op, "send") {
			return true
		}
	}
	return false
}

// Slots stay in their original order so N references cannot be rebound. A
// rejected revision keeps the original slot; the checker cannot add authority.
// ConstrainCheck returns the answer after its self-check. The check can
// drop an action or correct its fields. It cannot add an action, move an
// action to another item, project, or document, or change what is remembered.
func ConstrainCheck(before, after Output) Output {
	actions := append([]Action{}, before.Actions...)
	for i := len(after.Actions); i < len(actions); i++ {
		actions[i].SelfcheckDropped = true
	}
	for i, a := range after.Actions {
		if i >= len(actions) {
			break
		}
		old := before.Actions[i]
		if a.Op == "skip" {
			actions[i].SelfcheckDropped = true
			continue
		}
		if old.Op == "delegate" && (old.Kind != a.Kind || pointerValue(old.DocumentID) != pointerValue(a.DocumentID) || intPointerValue(old.BaseVersion) != intPointerValue(a.BaseVersion)) || old.Op == "add_steps" && len(a.Steps) > len(old.Steps) || old.Op == "close_date" && old.As != a.As {
			continue
		}
		if old.ParseErr != nil || a.ParseErr != nil || old.Op != a.Op || old.Ref != a.Ref || pointerValue(old.Project) != pointerValue(a.Project) {
			continue
		}
		// Changing a project in an update redirects the action to another object.
		if string(old.Set["project"]) != string(a.Set["project"]) {
			continue
		}
		actions[i] = a
	}
	after.Actions = actions
	after.MemoryPlan = before.MemoryPlan
	after.Remember = before.Remember
	return after
}

// HeavyTaskText is what the heavy reader is asked about: the owner's
// sentence and the open item.
func HeavyTaskText(text string, this *workspace.Item) string {
	if this != nil {
		text += "\n事项：" + this.Title + "\n" + this.Notes + "\n" + this.Body + "\n目标：" + this.Goal
	}
	return text
}

// AddRead gives each memory that a reader picked an alias, unless the turn
// already showed it, and returns the lines that are added to the model input.
// withTrust is true for the reading before the answer.
func AddRead(sent map[string]workspace.Memory, picked []workspace.Memory, loc *time.Location, withTrust bool) string {
	var out strings.Builder
	for _, m := range picked {
		exists := false
		for _, old := range sent {
			if old.ID == m.ID {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		alias := fmt.Sprintf("M%d", len(sent)+1)
		sent[alias] = m
		if withTrust {
			fmt.Fprintf(&out, "\n重档读者补充 [%s / trust=%s] %s\n", alias, m.Trust, m.Text+contextbuilder.MemorySuffix(m, loc))
		} else {
			fmt.Fprintf(&out, "\n重读补充 [%s] %s\n", alias, m.Text)
		}
	}
	return out.String()
}

// PlannedGroups returns the keys of the groups that the answer names by
// their alias G1, G2, and so on.
func PlannedGroups(aliases []string, index []workspace.StatusCardRef) []string {
	keys := []string{}
	for _, alias := range aliases {
		for i, g := range index {
			if alias == fmt.Sprintf("G%d", i+1) {
				keys = append(keys, g.Key)
			}
		}
	}
	return keys
}

// SelfcheckSkipped is the receipt of a self-check that did not finish. The
// draft is kept.
func SelfcheckSkipped(err error) workspace.DeskReceipt {
	text := "自查未完成，保留了原稿。"
	if errors.Is(err, ai.ErrUnsupportedCapability) {
		text = "当前模型不支持自查所需的输出格式，保留了原稿。"
	}
	return workspace.DeskReceipt{Op: "selfcheck", Text: text, Status: "skipped", Reason: ErrorType("selfcheck", err)}
}

// SelfcheckApplication says what became of the self-check call of a turn:
// "applied", "skipped" when the check failed, or "not_applicable" when the
// turn did not finish. reason is the class of the check's failure.
func SelfcheckApplication(checkErr, turnErr error) (application, reason string) {
	application = "applied"
	if checkErr != nil {
		application, reason = "skipped", ErrorType("selfcheck", checkErr)
	}
	if turnErr != nil {
		application = "not_applicable"
	}
	return application, reason
}

// AnswerApplication says what became of the answer call of a turn: "applied",
// or "not_applicable" with the class of the failure of the turn.
func AnswerApplication(stage string, turnErr error) (application, reason string) {
	if turnErr != nil {
		return "not_applicable", ErrorType(stage, turnErr)
	}
	return "applied", ""
}

// CaptureReceipt is what the owner reads when a turn did not finish. unordered
// is true when the turn lost its place in the conversation; hasText is false
// when the owner sent attachments only.
func CaptureReceipt(stage string, err error, unordered, hasText bool) string {
	text := CaptureText(stage, err)
	if unordered {
		text = "已记下原话；这轮操作未完成，为避免覆盖后续改动，请重新说明要做的事"
	}
	if !hasText {
		text = strings.Replace(text, "已记下原话", "附件已存好", 1)
	}
	return text
}
