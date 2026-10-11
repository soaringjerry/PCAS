package deputy

import (
	"reflect"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestOnlyAMarkdownChecklistBecomesSubtasks(t *testing.T) {
	output := "虚构的说明\n- [ ] 第一步\n  - [x] 第二步　\n- [] 不是清单\n1. 也不是\n- [ ]  "
	if got := Checklist(output); !reflect.DeepEqual(got, []string{"第一步", "第二步"}) {
		t.Fatalf("%q", got)
	}
	for kind, want := range map[string]string{"breakdown": "subtasks", "plan": "subtasks", "revise": "doc"} {
		if got := AdoptionFor(workspace.Run{Kind: kind}, output); got != want {
			t.Fatal(kind, got)
		}
	}
	for kind, want := range map[string]string{"summary": "progress", "draft": "doc", "breakdown": "doc"} {
		if got := AdoptionFor(workspace.Run{Kind: kind}, "1. 虚构的编号\n2. 列表"); got != want {
			t.Fatal(kind, got)
		}
	}
	// The manual command also takes a plain list.
	if got := AdoptionLines("1. 虚构的一步\n- 另一步\n\n• 第三步"); !reflect.DeepEqual(got, []string{"虚构的一步", "另一步", "第三步"}) {
		t.Fatalf("%q", got)
	}
	if got := AdoptionLines("前言\n- [ ] 只取清单"); !reflect.DeepEqual(got, []string{"只取清单"}) {
		t.Fatalf("%q", got)
	}
}

func TestNoticeOfAFinishedRunIsShort(t *testing.T) {
	if got := NoticeReason(workspace.Run{Status: "failed", Error: "虚构的原因"}); got != "副手没做成：虚构的原因" {
		t.Fatal(got)
	}
	if got := NoticeReason(workspace.Run{Status: "done", Output: "  "}); got != "副手做完了。" {
		t.Fatal(got)
	}
	long := NoticeReason(workspace.Run{Status: "done", Output: strings.Repeat("云", 700)})
	if !strings.HasPrefix(long, "副手做完了：\n") || !strings.HasSuffix(long, "…") || len([]rune(long)) != len([]rune("副手做完了：\n"))+601 {
		t.Fatal(len([]rune(long)))
	}
}
