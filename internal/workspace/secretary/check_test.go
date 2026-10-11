package secretary

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestTheSelfcheckCanDropOrCorrectAnActionAndCannotMoveIt(t *testing.T) {
	doc := "d1"
	before := Output{Remember: true, Actions: []Action{
		{Op: "update", Ref: "T1", Set: map[string]json.RawMessage{"due": json.RawMessage(`"2099-01-01"`)}},
		{Op: "update", Ref: "T2"},
		{Op: "create_task", Title: "虚构的任务"},
		{Op: "delegate", Ref: "T1", Kind: "revise", DocumentID: &doc},
	}}
	other := "d2"
	after := Output{Remember: false, Actions: []Action{
		{Op: "update", Ref: "T1", Set: map[string]json.RawMessage{"due": json.RawMessage(`"2099-02-02"`)}},
		{Op: "update", Ref: "T9"},
		{Op: "skip"},
		{Op: "delegate", Ref: "T1", Kind: "revise", DocumentID: &other},
		{Op: "create_task", Title: "检查想加的任务"},
	}}
	got := ConstrainCheck(before, after)
	if len(got.Actions) != 4 || !got.Remember {
		t.Fatalf("%+v", got)
	}
	if string(got.Actions[0].Set["due"]) != `"2099-02-02"` {
		t.Fatal("a corrected field is taken")
	}
	if got.Actions[1].Ref != "T2" {
		t.Fatal("an action is not moved to another item")
	}
	if !got.Actions[2].SelfcheckDropped {
		t.Fatal("a skipped action is dropped")
	}
	if *got.Actions[3].DocumentID != "d1" {
		t.Fatal("a revision is not moved to another document")
	}
	shorter := ConstrainCheck(before, Output{Actions: after.Actions[:1]})
	if !shorter.Actions[1].SelfcheckDropped || !shorter.Actions[3].SelfcheckDropped {
		t.Fatal("an action that the check left out is dropped")
	}
	if !NeedsCheck(Output{Actions: []Action{{Op: "delete_task"}}}) || NeedsCheck(Output{Actions: []Action{{Op: "create_task"}}}) {
		t.Fatal("an answer that deletes or sends is checked")
	}
}

func TestReaderMemoriesGetNewAliasesOnce(t *testing.T) {
	sent := map[string]workspace.Memory{"M1": {ID: "a", Text: "已有"}}
	picked := []workspace.Memory{{ID: "a", Text: "已有"}, {ID: "b", Text: "新读到的", Trust: "confirmed"}}
	if got := AddRead(sent, picked, zone, true); got != "\n重档读者补充 [M2 / trust=confirmed] 新读到的\n" || len(sent) != 2 {
		t.Fatalf("%q", got)
	}
	if got := AddRead(sent, []workspace.Memory{{ID: "c", Text: "再读到的"}}, zone, false); got != "\n重读补充 [M3] 再读到的\n" {
		t.Fatalf("%q", got)
	}
	index := []workspace.StatusCardRef{{Key: "self:rule"}, {Key: "entity:x"}}
	if got := PlannedGroups([]string{"G2", "G9", "G1"}, index); len(got) != 2 || got[0] != "entity:x" || got[1] != "self:rule" {
		t.Fatalf("%v", got)
	}
	this := workspace.Item{Title: "虚构的任务", Notes: "说明", Goal: "目标"}
	if HeavyTaskText("做完它", &this) != "做完它\n事项：虚构的任务\n说明\n\n目标：目标" || HeavyTaskText("你好", nil) != "你好" {
		t.Fatal("heavy task text")
	}
}

func TestWhatBecameOfTheCallsOfATurnAndWhatTheOwnerReads(t *testing.T) {
	if a, r := SelfcheckApplication(nil, nil); a != "applied" || r != "" {
		t.Fatal(a, r)
	}
	if a, r := SelfcheckApplication(context.DeadlineExceeded, nil); a != "skipped" || r != "timeout" {
		t.Fatal(a, r)
	}
	if a, r := SelfcheckApplication(nil, memory.ErrConflict); a != "not_applicable" || r != "" {
		t.Fatal(a, r)
	}
	if a, r := AnswerApplication("verify", memory.ErrConflict); a != "not_applicable" || r != "conflict" {
		t.Fatal(a, r)
	}
	if a, r := AnswerApplication("model", nil); a != "applied" || r != "" {
		t.Fatal(a, r)
	}
	if got := SelfcheckSkipped(ai.ErrUnsupportedCapability); got.Status != "skipped" || got.Text != "当前模型不支持自查所需的输出格式，保留了原稿。" || got.Reason != "unsupported_capability" {
		t.Fatalf("%+v", got)
	}
	if SelfcheckSkipped(context.DeadlineExceeded).Text != "自查未完成，保留了原稿。" {
		t.Fatal("selfcheck receipt")
	}
	if got := CaptureReceipt("model", context.DeadlineExceeded, false, true); got != "已记下原话；模型响应超时，稍后会自动整理" {
		t.Fatal(got)
	}
	if got := CaptureReceipt("order", memory.ErrConflict, true, true); got != "已记下原话；这轮操作未完成，为避免覆盖后续改动，请重新说明要做的事" {
		t.Fatal(got)
	}
	if got := CaptureReceipt("model", context.DeadlineExceeded, false, false); got != "附件已存好；模型响应超时，稍后会自动整理" {
		t.Fatal(got)
	}
}
