package secretary

import (
	"context"
	"errors"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// errUnordered is the failure of a turn that lost its place in the
// conversation. Its sentence is kept; nothing else is done.
var errUnordered = errors.New("secretary turn incomplete")

// View is what the context of a turn shows before the model is asked.
type View struct {
	// Agent is the name of the agent that answers.
	Agent string
	// Aliases are the items that the turn shows, by their alias.
	Aliases map[string]workspace.Item
	// Last is the turn before this one in the conversation, or nil.
	Last *workspace.SecretaryTurn
}

// Session is one turn in its place in the conversation. It gives the turn
// its reads and its writes, all in one transaction.
type Session interface {
	Turn
	// Context reads the context of the turn. failure is a failure that
	// falls back to keeping the sentence.
	Context(ctx context.Context) (view View, failure error)
	// Input builds the model input and checks that the agent may use what
	// it holds. stage names the place of a failure.
	Input(ctx context.Context) (state *Asking, stage string, failure error)
	// Asked gives the session the state after the model was asked.
	Asked(state *Asking)
	// BeginWrite starts the writes of the turn. From here the owner's
	// workspace is locked for this turn.
	BeginWrite(ctx context.Context) error
	// VerifyTargets checks that what the answer wants to change did not
	// change while the model was asked.
	VerifyTargets(ctx context.Context, answer Output) error
	// CheckFailed records that the self-check did not finish.
	CheckFailed(ctx context.Context, reason string) error
	// RecordApplication records what became of a paid call of the turn.
	RecordApplication(ctx context.Context, stage, application, reason string) error
	// KeepSentence keeps the owner's sentence of a turn that did not finish.
	KeepSentence(ctx context.Context, unordered bool) error
	// SaveSentence saves the owner's sentence as the source of the turn.
	SaveSentence(ctx context.Context) error
}

// Conducting is one turn that is to be conducted.
type Conducting struct {
	// Unordered is true when the turn lost its place in the conversation.
	Unordered bool
	// HasText is false when the owner sent attachments only, and CheckOnly
	// is true for a check-only turn.
	HasText, CheckOnly bool
	Limits             Limits
}

// Conducted is what a turn gives back to the owner.
type Conducted struct {
	Agent    string
	Reply    string
	Receipts []workspace.DeskReceipt
	Ask      *workspace.DeskAsk
	Cards    []workspace.DeskCard
	// Finished is false when the turn fell back to keeping the sentence.
	Finished bool
}

// Conduct conducts one turn: it reads the context, asks the model, checks
// that the targets of the answer are unchanged, and applies the answer. A
// turn that cannot finish keeps the owner's sentence and says why; it changes
// nothing else. Each paid call is recorded with what became of it.
func Conduct(ctx context.Context, s Session, m Model, in Conducting) (Conducted, error) {
	out := Conducted{Receipts: []workspace.DeskReceipt{}, Cards: []workspace.DeskCard{}}
	view, failure := s.Context(ctx)
	stage := "context"
	if in.Unordered {
		failure, stage = errUnordered, "order"
	}
	out.Agent = view.Agent
	var asked Asked
	var state *Asking
	if failure == nil {
		state, stage, failure = s.Input(ctx)
		if failure == nil {
			var err error
			asked, err = Ask(ctx, m, state, in.Limits)
			s.Asked(state)
			if err != nil {
				return out, err
			}
			failure, stage = asked.Failure, asked.Stage
		}
	}
	if err := s.BeginWrite(ctx); err != nil {
		return out, err
	}
	if failure == nil {
		stage = "verify"
		failure = s.VerifyTargets(ctx, asked.Answer)
	}
	if asked.Checked {
		application, reason := SelfcheckApplication(asked.CheckFailure, failure)
		if asked.CheckFailure != nil {
			if err := s.CheckFailed(ctx, reason); err != nil {
				return out, err
			}
			if failure == nil {
				out.Receipts = append(out.Receipts, SelfcheckSkipped(asked.CheckFailure))
			}
		}
		if err := s.RecordApplication(ctx, "selfcheck", application, reason); err != nil {
			return out, err
		}
	}
	if asked.Generated {
		application, reason := AnswerApplication(stage, failure)
		if err := s.RecordApplication(ctx, "answer", application, reason); err != nil {
			return out, err
		}
	}
	if failure != nil {
		slog.WarnContext(ctx, "secretary capture fallback", "stage", stage, "error_type", ErrorType(stage, failure))
		if err := s.KeepSentence(ctx, in.Unordered); err != nil {
			return out, err
		}
		out.Receipts = append(out.Receipts, workspace.DeskReceipt{Op: "capture", Text: CaptureReceipt(stage, failure, in.Unordered, in.HasText), Status: "done"})
		return out, nil
	}
	if err := s.SaveSentence(ctx); err != nil {
		return out, err
	}
	answer := Answer{Output: asked.Answer, Sent: state.Sent, Last: view.Last, Aliases: view.Aliases, KeepsSentence: in.HasText && !in.CheckOnly}
	if state.Use.Coverage != nil {
		answer.NotRead, answer.ReaderNotices = state.Use.Coverage.Skipped, state.Use.Coverage.Notices
	}
	applied, err := Apply(ctx, s, answer)
	if err != nil {
		return out, err
	}
	out.Reply, out.Ask, out.Cards, out.Finished = applied.Reply, applied.Ask, applied.Cards, true
	out.Receipts = append(out.Receipts, applied.Receipts...)
	return out, nil
}
