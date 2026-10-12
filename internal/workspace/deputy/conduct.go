package deputy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// AnswerTimeout is the time that the answer call of a run has.
const AnswerTimeout = 60 * time.Second

// Selection records which output of a run was taken: the answer or its
// checked text. It holds identities and a hash only; the text stays in the
// storage of results.
type Selection struct {
	Main       memory.ID `json:"main"`
	Output     memory.ID `json:"output"`
	OutputHash string    `json:"outputHash"`
	Review     memory.ID `json:"review,omitempty"`
	Fallback   string    `json:"fallback,omitempty"`
}

// CallRecord is what the journal holds about one call of a run.
type CallRecord struct {
	ID        memory.ID
	ErrorCode string
	// Unknown is true when the outcome of the call is not known.
	Unknown bool
	// Reservation is the reservation that the gateway linked to the call, or
	// empty.
	Reservation string
	// Billed is the cost on the call's receipt, or nil.
	Billed   *float64
	Reserved float64
}

// Session is one leased run. Storage holds the lease, the journal of the
// run's calls, and the writes. A session of a resumed run carries what the
// earlier attempt saved.
type Session interface {
	// Owner and Token identify the lease for the calls of the run.
	Owner() memory.ID
	Token() string
	// Run is the run. Conduct changes it; Finish writes it.
	Run() *workspace.Run
	// Started is the start of this attempt.
	Started() time.Time
	// Saved is the call of an earlier attempt, or nil. InputSaved is true
	// when that attempt saved its input: the brief is not built again.
	Saved() *CallRecord
	InputSaved() bool
	// SavedContext is the readiness and the coverage that the saved input
	// holds.
	SavedContext() (ready bool, coverage *contextbuilder.Coverage)
	// Prepare reads the memory that the run is given.
	Prepare(ctx context.Context) (contextbuilder.UseContext, workspace.Agent, error)
	// VerifyPrompt checks that the agent may still use what the brief holds.
	VerifyPrompt(ctx context.Context) error
	// Preparing and Prepared mark the preparation for the timing record.
	Preparing()
	Prepared()
	// Call returns the journal's latest record of the request, or nil.
	Call(ctx context.Context, request modelcall.Request) (*CallRecord, error)
	// FinishRevision writes the paid answer of a revision as a document.
	FinishRevision(ctx context.Context, paid *modelcall.PaidResult) error
	// SettleCost settles the admission of a run whose call has no linked
	// reservation.
	SettleCost(ctx context.Context, cost float64) error
	// Selection returns the saved selection of the answer and its text.
	Selection(ctx context.Context, answer memory.ID) (*Selection, string, error)
	// ReviewAttempted returns the self-check call of an earlier attempt.
	ReviewAttempted(ctx context.Context) (memory.ID, bool, error)
	// VerifySelfcheck checks the run's context before the self-check.
	VerifySelfcheck(ctx context.Context) error
	// SaveSelection saves the selection with the answer's request.
	SaveSelection(ctx context.Context, request modelcall.Request, selection Selection) error
	// Finish writes the end of the run in one transaction: the run, its
	// dependencies, the adoption, the record of each call's application,
	// and the notice.
	Finish(ctx context.Context, end End) error
}

// End is what Conduct gives to the last write of a run.
type End struct {
	Use           contextbuilder.UseContext
	Text          string
	Searches      []string
	Cost          float64
	GenerationErr error
	VerifyErr     error
	// Answer and Review are the calls of the run, or empty.
	Answer, Review memory.ID
	// Saved is the journal's record of the answer call, or nil.
	Saved *CallRecord
	// SelfcheckFallback names why the checked text was not taken, or is
	// empty.
	SelfcheckFallback string
}

// Conduct makes one run of the deputy: it gives the run its memory, makes the
// answer call, settles its cost, lets the model check the draft, saves which
// text was taken, and writes the end of the run. A resumed run uses what its
// earlier attempt saved and never repeats a paid call. persist is the context
// of the records; work ends earlier, so that the last write has time.
func Conduct(persist, work context.Context, s Session, steps Steps, errorType func(stage string, err error) string) error {
	ctx := persist
	run := s.Run()
	var u contextbuilder.UseContext
	var useErr error
	saved := s.Saved()
	if s.InputSaved() {
		u.Ready, u.Coverage = s.SavedContext()
	}
	if saved == nil {
		s.Preparing()
		prepared, agent, prepareErr := s.Prepare(work)
		u, useErr = prepared, prepareErr
		if useErr == nil {
			run.MemoryTier = MemoryTier(run.MemoryTier, u.Ready)
		}
		if useErr == nil && u.Ready {
			run.ProjectHandoverWrittenAt = nil
			if u.ProjectHandover != nil {
				run.ProjectHandoverWrittenAt = u.ProjectHandover.WrittenAt
			}
			if run.MemoryTier == "heavy" {
				readerCtx, readerCancel := context.WithDeadline(work, s.Started().Add(reader.Budget))
				picked, refs, keys := steps.Reader.Read(readerCtx, ctx, reader.Task{Scope: memory.Scope{OwnerID: s.Owner(), PrincipalID: "owner", IsOwner: true}, Agent: agent, Thing: &run.ThingID, Text: HeavyTask(*run), Run: run.ID, Phase: "reader:prepare"}, u)
				readerCancel()
				run.MemoryGroups = keys
				run.ContextVersions = uniqueRefs(append(run.ContextVersions, refs...))
				u.Cards = nil
				u.Supplemental = picked
			}
			run.ContextVersions = uniqueRefs(append(run.ContextVersions, u.Dependencies...))
			RefreshMemory(run, u)
		}
	}
	verifyErr := useErr
	if verifyErr == nil && saved == nil {
		verifyErr = s.VerifyPrompt(work)
	}
	end := End{VerifyErr: verifyErr}
	s.Prepared()
	answerStarted := time.Now()
	generationErr := verifyErr
	var mainPaid *modelcall.PaidResult
	request := AnswerRequest(s.Owner(), s.Token(), Snapshot{Run: *run, RawPrompt: run.Brief, Coverage: u.Coverage, MemoryReady: u.Ready})
	if saved != nil && !s.InputSaved() {
		end.Answer = saved.ID
		generationErr = &modelcall.Failure{Code: saved.ErrorCode, InvocationID: saved.ID}
		if saved.ErrorCode == "" {
			generationErr = modelcall.ErrNotApplicable
		}
	} else if verifyErr == nil {
		answerCtx, answerCancel := context.WithTimeout(work, AnswerTimeout)
		mainPaid, generationErr = steps.Calls.Answer(answerCtx, request)
		answerCancel()
		var persistence *modelcall.PersistenceError
		if errors.As(generationErr, &persistence) {
			return generationErr
		}
		if mainPaid != nil {
			end.Answer = mainPaid.InvocationID
			end.Text, end.Searches, end.Cost = mainPaid.Output, mainPaid.Searches, mainPaid.Cost
		}
		row, err := s.Call(ctx, request)
		if err != nil {
			return err
		}
		if row != nil {
			saved = row
			end.Answer = row.ID
		}
	}
	if run.Kind == "revise" && generationErr == nil && mainPaid != nil {
		return s.FinishRevision(ctx, mainPaid)
	}
	if saved != nil {
		end.Cost = SettledCost(end.Cost, saved.Billed, saved.Unknown, saved.Reserved)
	}
	// Only an unused admission without a linked reservation is settled here.
	// The gateway already owns every linked reservation and usage record.
	if saved == nil || saved.Reservation == "" {
		if err := s.SettleCost(ctx, end.Cost); err != nil {
			return err
		}
	}
	var selection *Selection
	if generationErr == nil && mainPaid != nil {
		chosen, selected, err := s.Selection(ctx, end.Answer)
		if err != nil {
			return err
		}
		if chosen != nil {
			selection = chosen
			end.Text = selected
			end.SelfcheckFallback = chosen.Fallback
			end.Review = chosen.Review
		}
	}
	// A prior review without a durable decision is incomplete. Keep the draft.
	// Never repeat its paid call or reinterpret a late response as a new decision.
	reviewAlreadyAttempted := false
	if selection == nil && generationErr == nil && Selfchecks(run.MemoryTier) {
		review, attempted, err := s.ReviewAttempted(ctx)
		if err != nil {
			return err
		}
		reviewAlreadyAttempted = attempted
		if attempted {
			end.Review = review
			end.SelfcheckFallback = "selfcheck_interrupted"
		}
	}
	if selection == nil && !reviewAlreadyAttempted && generationErr == nil && Selfchecks(run.MemoryTier) {
		checkCtx, checkCancel := context.WithTimeout(work, SelfcheckBudget(time.Since(answerStarted)))
		checkedText := ""
		err := s.VerifySelfcheck(checkCtx)
		if err == nil {
			check := SelfcheckRequest(s.Owner(), s.Token(), *run, end.Text)
			var paid *modelcall.PaidResult
			paid, err = steps.Calls.Selfcheck(checkCtx, check)
			readCtx, readCancel := context.WithTimeout(context.WithoutCancel(checkCtx), modelcall.PersistenceTimeout)
			row, readErr := s.Call(readCtx, check)
			readCancel()
			if readErr != nil {
				checkCancel()
				return readErr
			}
			if row != nil {
				end.Review = row.ID
			}
			if err == nil {
				checkedText = paid.Output
				err = checkCtx.Err()
			}
		}
		checkCancel()
		var fallback string
		if end.Text, fallback = AcceptSelfcheck(end.Text, checkedText, err); fallback != "" {
			end.SelfcheckFallback = fallback
			slog.WarnContext(ctx, "memory selfcheck fallback", "stage", "selfcheck", "error_type", errorType("selfcheck", err))
		}
	}
	if generationErr == nil && mainPaid != nil {
		if selection == nil {
			selected := end.Answer
			if end.SelfcheckFallback == "" && end.Review != "" {
				selected = end.Review
			}
			selection = &Selection{Main: end.Answer, Output: selected, OutputHash: fmt.Sprintf("%x", sha256.Sum256([]byte(end.Text))), Review: end.Review, Fallback: end.SelfcheckFallback}
		}
		if err := s.SaveSelection(ctx, request, *selection); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	end.Use, end.GenerationErr, end.Saved = u, generationErr, saved
	return s.Finish(ctx, end)
}

// MemoryTier is the tier of a run: a heavy run whose memory is not built
// reads at the medium tier.
func MemoryTier(tier string, ready bool) string {
	if tier == "heavy" && !ready {
		return "medium"
	}
	return tier
}

func uniqueRefs(refs []memory.Ref) []memory.Ref {
	seen := make(map[memory.Ref]bool, len(refs))
	out := make([]memory.Ref, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}
