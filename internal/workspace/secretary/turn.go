package secretary

import (
	"context"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// ActionOutcome is what became of one action in its own transaction.
type ActionOutcome struct {
	// Receipt and Err are what the action returned.
	Receipt workspace.DeskReceipt
	Err     error
	// Committed is false when the action was rolled back.
	Committed bool
	// Changed is true when the committed action left a change that the owner
	// can undo. ActionID names that change.
	Changed  bool
	ActionID string
	// Companions are the receipts of projects that the action made on its way.
	Companions []workspace.DeskReceipt
}

// Done reports whether an action is kept. An action that returned an error
// or a skipped receipt is rolled back.
func Done(receipt workspace.DeskReceipt, err error) bool {
	return err == nil && receipt.Status != "skipped"
}

// Turn is the unit of work in which an answer is applied. All its writes are
// in the transaction of the turn.
type Turn interface {
	// RecordUse records that the owner took up or mentioned a shown memory.
	RecordUse(ctx context.Context, m workspace.Memory, kind string) error
	// Overflow records a count of things that the turn left out.
	Overflow(ctx context.Context, reason string, count int) error
	// Act does one action in its own transaction inside the turn. It keeps
	// the action when Done says so, and it rolls it back otherwise.
	Act(ctx context.Context, a Action, aliases map[string]workspace.Item) (ActionOutcome, error)
	Item(ctx context.Context, id string) (workspace.Item, error)
	// Cards builds the cards of the answer from what the turn showed.
	Cards(ctx context.Context, answer Output, sent map[string]workspace.Memory) ([]workspace.DeskCard, error)
}

// Answer is a read answer with what the turn showed to the model.
type Answer struct {
	Output Output
	// Sent are the memories that the turn showed, by their alias.
	Sent map[string]workspace.Memory
	// Last is the turn before this one in the conversation, or nil.
	Last *workspace.SecretaryTurn
	// NotRead are the groups of memory that the turn did not read, and
	// ReaderNotices are the notices of the reader.
	NotRead, ReaderNotices []string
	// Aliases are the items that the turn showed, by their alias.
	Aliases map[string]workspace.Item
	// KeepsSentence is true when the owner's sentence is kept as a source: it
	// has text and the turn is not check-only.
	KeepsSentence bool
}

// Applied is what applying an answer adds to the turn.
type Applied struct {
	Reply    string
	Receipts []workspace.DeskReceipt
	Ask      *workspace.DeskAsk
	Cards    []workspace.DeskCard
}

// Apply applies a read answer: it writes the reply, records which memories
// the owner took up or mentioned, does the actions in their order, and
// builds the cards. An action can name an item that an earlier action of
// the same turn made. One action that fails does not stop the others.
func Apply(ctx context.Context, t Turn, in Answer) (Applied, error) {
	out := Applied{Reply: Reply(in.Output.Reply, in.NotRead), Receipts: []workspace.DeskReceipt{}}
	for _, note := range in.ReaderNotices {
		out.Receipts = append(out.Receipts, workspace.DeskReceipt{Op: "reader", Text: note, Status: "skipped"})
	}
	for _, m := range Adopted(in.Output.MemoryPlan, in.Sent, in.Last) {
		if err := t.RecordUse(ctx, m, "adoption"); err != nil {
			return out, err
		}
	}
	for _, m := range Mentioned(in.Output.MemoryPlan, in.Sent) {
		if err := t.RecordUse(ctx, m, "user_mention"); err != nil {
			return out, err
		}
	}
	out.Ask = in.Output.Ask
	if out.Ask != nil && out.Ask.Options == nil {
		out.Ask.Options = []string{}
	}
	// Keep execution-only N aliases out of the model's original context
	// and cards. Array positions include skipped and malformed actions.
	aliases := make(map[string]workspace.Item, len(in.Aliases)+ActionLimit)
	for alias, item := range in.Aliases {
		aliases[alias] = item
	}
	steps, overLimit := PlanActions(in.Output.Actions)
	if overLimit > 0 {
		if err := t.Overflow(ctx, "action_limit", overLimit); err != nil {
			return out, err
		}
	}
	for _, step := range steps {
		a := step.Action
		if step.Skipped != nil {
			if a.ParseErr != nil && step.Index < ActionLimit {
				slog.WarnContext(ctx, "secretary action skipped", "stage", "parse", "error_type", ErrorType("parse", a.ParseErr))
			}
			out.Receipts = append(out.Receipts, *step.Skipped)
			continue
		}
		done, err := t.Act(ctx, a, aliases)
		if err != nil {
			return out, err
		}
		receipt := done.Receipt
		if !done.Committed {
			receipt = Unfinished(a.Op, receipt, done.Err)
		} else {
			// The summary and receipt describe the actual persisted result.
			if done.Changed {
				id := done.ActionID
				receipt.ActionID = &id
				receipt.Undoable = true
			} else {
				receipt = NothingChanged(a.Op)
			}
			out.Receipts = append(out.Receipts, done.Companions...)
			if alias := CreatedAlias(a.Op, step.Index); alias != "" && receipt.Status == "done" && receipt.ThingID != nil {
				item, err := t.Item(ctx, *receipt.ThingID)
				if err != nil {
					return out, err
				}
				aliases[alias] = item
			}
		}
		out.Receipts = append(out.Receipts, receipt)
	}
	if in.Output.Remember && in.KeepsSentence {
		out.Receipts = append(out.Receipts, workspace.DeskReceipt{Op: "remember", Text: "记下了，会整理进记忆", Status: "done"})
	}
	var err error
	out.Cards, err = t.Cards(ctx, in.Output, in.Sent)
	return out, err
}
