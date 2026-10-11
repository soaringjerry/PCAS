package secretary

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestWorkspaceInputNamesEachItemByItsAliasAndCutsLongProjectEvidence(t *testing.T) {
	hours, start := 2.5, "2026-03-01"
	long := strings.Repeat("云", 450)
	view := WorkspaceView{
		City: "虚构城", Location: time.FixedZone("Fictitious/Zone", 8*3600),
		Projects: []workspace.Item{
			{ID: "p1", Title: "虚构的项目", Status: "active", Goal: "虚构的目标", Creation: &workspace.ItemCreation{Source: &workspace.SourceRef{Excerpt: "虚构的依据"}}},
			{ID: "p2", Title: "长依据项目", Status: "paused", Goal: long},
		},
		OpenCounts: map[string]int{"p1": 3},
		Tasks: []workspace.Item{
			{Title: "虚构的任务", Status: "todo", Due: "2026-03-05T07:00:00Z", ProjectID: "p1"},
			{Title: "有工作量的任务", Status: "doing", EstimatedHours: &hours, StartDate: &start},
		},
		Ideas:  []workspace.Item{{Title: "虚构的想法", Status: "open"}},
		Recent: []workspace.Item{{Title: "虚构的任务", Status: "todo", Due: "2026-03-05T07:00:00Z"}},
		History: []HistoryTurn{
			{Text: "建个任务", Reply: "已建", Receipts: []HistoryReceipt{{Text: "已建：虚构的任务"}, {Text: "已建：撤销的任务", Undone: true}, {Text: "不可见", Hidden: true}}},
			{Text: "旧问题", Reply: "旧回答", Outdated: true},
			{Text: "", Reply: "没有原话的一轮", Receipts: []HistoryReceipt{{Text: "不显示"}}},
		},
	}
	text, clipped := RenderWorkspace(view)
	want := "用户所在城市：虚构城\n时区：Fictitious/Zone\n\n项目列表：\n" +
		"P1：虚构的项目（未完成 3；状态 active；目标与依据：虚构的目标\n虚构的依据）\n" +
		"P2：长依据项目（未完成 0；状态 paused；目标与依据：" + strings.Repeat("云", 400) + "（项目依据还有内容））\n" +
		"\n未完成任务：\n" +
		"T1：虚构的任务（todo；截止 2026-03-05T07:00:00Z；项目 虚构的项目）\n" +
		"T2：有工作量的任务（doing；截止 ；项目 ；估计工作量 2.5小时；开工日 2026-03-01）\n" +
		"\n想法：\nI1：虚构的想法（open）\n" +
		"\n本对话历史：\n" +
		"问：建个任务\n答：已建\n已建：虚构的任务\n（已撤销）已建：撤销的任务\n（先前事项内容按当前权限隐藏）\n" +
		"问：旧问题\n答：" + OutdatedAnswer + "\n" +
		"问：\n答：没有原话的一轮\n" +
		"本对话的事项：\nR1：虚构的任务（todo；截止 2026-03-05T07:00:00Z）\n"
	if text != want {
		t.Fatalf("got:\n%s\nwant:\n%s", text, want)
	}
	if !reflect.DeepEqual(clipped, []int{50}) {
		t.Fatal(clipped)
	}
}
