package secretary

import (
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

// OutdatedAnswer replaces an earlier answer whose evidence has changed.
const OutdatedAnswer = "（先前回答的依据已更新，请按现在的资料回答）"

// projectEvidenceLimit is the longest goal and evidence of one project in
// the model input, in characters. Its basis is the size of the project list:
// the list is sent on each turn.
const projectEvidenceLimit = 400

// HistoryReceipt is one receipt of an earlier turn.
type HistoryReceipt struct {
	Text string
	// Undone is true when the owner undid the action.
	Undone bool
	// Hidden is true when the agent of this turn may not read the item.
	Hidden bool
}

// HistoryTurn is one earlier turn of the conversation.
type HistoryTurn struct {
	Text, Reply string
	// Outdated is true when the evidence of the answer has changed.
	Outdated bool
	Receipts []HistoryReceipt
}

// WorkspaceView is the workspace part of the model input of one turn. The
// order of each list gives the items their aliases: P1, T1, I1, and R1.
type WorkspaceView struct {
	City     string
	Location *time.Location
	Projects []workspace.Item
	// OpenCounts is the count of open tasks of each project.
	OpenCounts map[string]int
	Tasks      []workspace.Item
	Ideas      []workspace.Item
	History    []HistoryTurn
	// Recent are the items that this conversation made or changed.
	Recent []workspace.Item
}

// RenderWorkspace writes the workspace part of the model input. clipped
// holds, for each project whose goal and evidence were cut, the count of
// characters that were left out. The caller records each count.
func RenderWorkspace(v WorkspaceView) (text string, clipped []int) {
	var out strings.Builder
	fmt.Fprintf(&out, "用户所在城市：%s\n时区：%s\n\n项目列表：\n", v.City, v.Location)
	projectNames := map[string]string{}
	for _, p := range v.Projects {
		projectNames[p.ID] = p.Title
	}
	for i, p := range v.Projects {
		evidence := p.Goal
		if p.Creation != nil && p.Creation.Source != nil && evidence != p.Creation.Source.Excerpt {
			evidence += "\n" + p.Creation.Source.Excerpt
		}
		runes := []rune(evidence)
		if len(runes) > projectEvidenceLimit {
			evidence = string(runes[:projectEvidenceLimit]) + "（项目依据还有内容）"
			clipped = append(clipped, len(runes)-projectEvidenceLimit)
		}
		fmt.Fprintf(&out, "P%d：%s（未完成 %d；状态 %s；目标与依据：%s）\n", i+1, p.Title, v.OpenCounts[p.ID], p.Status, evidence)
	}
	fmt.Fprintln(&out, "\n未完成任务：")
	for i, t := range v.Tasks {
		// Effort and start date only appear once they exist; every other task
		// line stays byte-identical to the pre-phase-3 prompt.
		plan := ""
		if t.EstimatedHours != nil {
			plan = fmt.Sprintf("；估计工作量 %s小时；开工日 %s", effort.FormatHours(*t.EstimatedHours), pointerValue(t.StartDate))
		}
		fmt.Fprintf(&out, "T%d：%s（%s；截止 %s；项目 %s%s）\n", i+1, t.Title, t.Status, t.Due, projectNames[t.ProjectID], plan)
	}
	fmt.Fprintln(&out, "\n想法：")
	for i, t := range v.Ideas {
		fmt.Fprintf(&out, "I%d：%s（%s）\n", i+1, t.Title, t.Status)
	}
	fmt.Fprintln(&out, "\n本对话历史：")
	for _, t := range v.History {
		reply := t.Reply
		if t.Outdated {
			reply = OutdatedAnswer
		}
		fmt.Fprintf(&out, "问：%s\n答：%s\n", t.Text, reply)
		// A turn without the owner's text shows no receipt.
		if t.Text == "" {
			continue
		}
		for _, receipt := range t.Receipts {
			switch {
			case receipt.Hidden:
				fmt.Fprintln(&out, "（先前事项内容按当前权限隐藏）")
			case receipt.Undone:
				fmt.Fprintln(&out, "（已撤销）"+receipt.Text)
			default:
				fmt.Fprintln(&out, receipt.Text)
			}
		}
	}
	fmt.Fprintln(&out, "本对话的事项：")
	for i, t := range v.Recent {
		fmt.Fprintf(&out, "R%d：%s（%s；截止 %s）\n", i+1, t.Title, t.Status, t.Due)
	}
	return out.String(), clipped
}
