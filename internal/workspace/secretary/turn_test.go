package secretary

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// fakeTurn runs actions against a workspace in memory. An action named
// "fail" returns an error, and "noop" commits without a change.
type fakeTurn struct {
	desk    *desk
	log     []string
	seen    []map[string]workspace.Item
	actions int
}

func (f *fakeTurn) RecordUse(_ context.Context, m workspace.Memory, kind string) error {
	f.log = append(f.log, kind+" "+m.ID)
	return nil
}
func (f *fakeTurn) Overflow(_ context.Context, reason string, count int) error {
	f.log = append(f.log, reason)
	return nil
}
func (f *fakeTurn) Item(ctx context.Context, id string) (workspace.Item, error) {
	return f.desk.Item(ctx, id)
}
func (f *fakeTurn) Cards(context.Context, Output, map[string]workspace.Memory) ([]workspace.DeskCard, error) {
	return []workspace.DeskCard{{Kind: "sources"}}, nil
}
func (f *fakeTurn) Act(ctx context.Context, a Action, aliases map[string]workspace.Item) (ActionOutcome, error) {
	f.actions++
	copied := map[string]workspace.Item{}
	for k, v := range aliases {
		copied[k] = v
	}
	f.seen = append(f.seen, copied)
	out := ActionOutcome{ActionID: "action-" + a.Op}
	switch a.Op {
	case "fail":
		out.Err = errors.New("fictitious failure")
		return out, nil
	case "noop":
		out.Receipt = workspace.DeskReceipt{Op: a.Op, Status: "done"}
		out.Committed = true
		return out, nil
	}
	out.Receipt, out.Err = Execute(ctx, f.desk, a, Env{Aliases: aliases, Location: zone})
	out.Committed = Done(out.Receipt, out.Err)
	out.Changed = out.Committed
	return out, nil
}

func TestApplyingAnAnswerDoesItsActionsInOrderAndOneFailureDoesNotStopTheRest(t *testing.T) {
	turn := &fakeTurn{desk: newDesk()}
	answer := Output{Reply: " 好的 ", Remember: true, MemoryPlan: &UsePlan{Mentioned: []string{"M1"}}, Actions: []Action{
		{Op: "create_task", Title: "虚构的任务"},
		{Op: "fail"},
		{Op: "update", Ref: "N1", Set: map[string]json.RawMessage{"status": json.RawMessage(`"doing"`)}},
		{Op: "update", Ref: "T9"},
		{Op: "noop"},
	}}
	got, err := Apply(context.Background(), turn, Answer{Output: answer, Sent: map[string]workspace.Memory{"M1": {ID: "a"}}, NotRead: []string{"虚构组"}, ReaderNotices: []string{"读者提示"}, KeepsSentence: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reply != "好的\n这几组没来得及看：虚构组" || len(got.Cards) != 1 || strings.Join(turn.log, ",") != "user_mention a" {
		t.Fatalf("%q %v", got.Reply, turn.log)
	}
	ops := []string{}
	for _, r := range got.Receipts {
		ops = append(ops, r.Op+":"+r.Status)
	}
	if strings.Join(ops, ",") != "reader:skipped,create_task:done,fail:skipped,update:done,update:skipped,noop:skipped,remember:done" {
		t.Fatal(ops)
	}
	created := got.Receipts[1]
	if !created.Undoable || created.ActionID == nil || created.ThingID == nil {
		t.Fatalf("%+v", created)
	}
	if got.Receipts[2].Reason != "这件事暂时办不了" || got.Receipts[4].Reason != "找不到要改的那件事" || got.Receipts[5].Reason != "没有可执行的修改" {
		t.Fatalf("%+v", got.Receipts)
	}
	// The third action names the task that the first action made.
	if _, ok := turn.seen[2]["N1"]; !ok || turn.desk.items[*created.ThingID].Status != "doing" {
		t.Fatal("a later action did not get the item of an earlier action")
	}
	if _, ok := turn.seen[0]["N1"]; ok {
		t.Fatal("the alias exists before its action")
	}
}

func TestApplyingAnAnswerStopsAtTheActionLimitAndKeepsACheckOnlySentenceUnremembered(t *testing.T) {
	turn := &fakeTurn{desk: newDesk()}
	actions := []Action{}
	for i := 0; i < 12; i++ {
		actions = append(actions, Action{Op: "noop"})
	}
	got, err := Apply(context.Background(), turn, Answer{Output: Output{Reply: "好", Remember: true, Actions: actions}})
	if err != nil || turn.actions != 10 || strings.Join(turn.log, ",") != "action_limit" {
		t.Fatal(err, turn.actions, turn.log)
	}
	last := got.Receipts[len(got.Receipts)-1]
	if last.Reason != "一次太多了，只做了前 10 件" || len(got.Receipts) != 11 {
		t.Fatalf("%d %+v", len(got.Receipts), last)
	}
	if got.Ask != nil || !Done(workspace.DeskReceipt{Status: "done"}, nil) || Done(workspace.DeskReceipt{Status: "skipped"}, nil) || Done(workspace.DeskReceipt{Status: "done"}, memory.ErrInvalid) {
		t.Fatal("done")
	}
}
