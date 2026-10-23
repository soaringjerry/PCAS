package extraction

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// An extracted task or idea is made at once only when the owner said it
// plainly, now, with this confidence. Twenty automatic tasks per source or
// conversation keep an imported list from flooding the home page; each item
// beyond stays a counted candidate. A title over the item limit stays a
// candidate with its whole text.
const (
	captureConfidence = 0.98
	automaticTasks    = 20
	titleBytes        = 2000
)

// CaptureUnit is the storage of the task or the idea of one item. Each
// method reads or writes; none decides.
type CaptureUnit interface {
	// Captured is true when the source already gave a candidate of the kind
	// with the quotation or the text.
	Captured(ctx context.Context, source memory.Ref, kind, quote, text string) (bool, error)
	// AutomaticTasks is the count of tasks made at once from the source or
	// from its conversation.
	AutomaticTasks(ctx context.Context, source memory.Ref, conversation string) (int, error)
	// Overflow records the count of what did not fit, by reason.
	Overflow(ctx context.Context, reason string, count int) error
	// Create saves the item as an action of the assistant with the summary.
	Create(ctx context.Context, item workspace.Item, action, summary string) error
	SaveCandidate(ctx context.Context, candidate workspace.Candidate) error
}

// CaptureReasons are why an item stays a candidate, with their words for the
// owner. An item without a reason is made at once.
func CaptureReasons(source memory.SourceResult, item Item, imported bool) ([]string, string) {
	reasons, descriptions := []string{}, []string{}
	add := func(code, text string) { reasons = append(reasons, code); descriptions = append(descriptions, text) }
	if imported || source.Context != nil && source.Context.Branch == "historical" {
		add("historical", "历史导入，不作为当前事项")
	}
	if item.Qualification == "ai_suggestion" || source.Context != nil && source.Context.Role != "user" {
		add("ai_suggestion", "AI 或工具的建议，不是本人表达")
	}
	if item.Acquisition == "reported" || item.Qualification == "quoted" {
		add("reported", "转述，留作候选")
	}
	if item.Qualification == "tentative" || !item.Explicit && item.Acquisition == "direct" {
		add("qualified", "带保留，留作候选")
	}
	if item.Acquisition == "inferred" || item.Confidence < captureConfidence || !oneOf(item.Qualification, "", "asserted", "corrected") || !item.Explicit && item.Acquisition != "direct" {
		add("uncertain", "拿不准，留作候选")
	}
	return reasons, strings.Join(descriptions, "；")
}

// CaptureItem makes the task or the idea of an item, or keeps it as a candidate
// with its reasons. A source gives one candidate of a kind for a quotation
// or a text. The accepted candidate stays as the fence against a replay,
// also after an undo removes the item.
func CaptureItem(ctx context.Context, u CaptureUnit, source memory.SourceResult, item Item, imported bool, project string, now time.Time) error {
	if !oneOf(item.Kind, "task", "idea") {
		return memory.ErrInvalid
	}
	captured, err := u.Captured(ctx, source.Source.Ref, item.Kind, item.Quote, item.Text)
	if err != nil || captured {
		return err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	reasons, reason := CaptureReasons(source, item, imported)
	v := workspace.Candidate{ID: string(memory.NewID()), Kind: item.Kind, Text: item.Text, ProjectID: project, Confidence: item.Confidence, Source: workspace.SourceRef{SourceID: string(source.Source.Ref.ID), Version: source.Source.Ref.Version, Label: source.Source.Title, Excerpt: item.Quote, At: stamp}, State: "pending", CreatedAt: stamp, Reasons: reasons, Reason: reason}
	if len(reasons) == 0 && item.Kind == "task" {
		conversation := ""
		if source.Context != nil {
			conversation = source.Context.Conversation
		}
		count, err := u.AutomaticTasks(ctx, source.Source.Ref, conversation)
		if err != nil {
			return err
		}
		if count >= automaticTasks {
			v.Reasons = append(v.Reasons, "overflow")
			v.Reason = "超过本段对话或资料自动创建上限，保留为候选"
			if err := u.Overflow(ctx, "automatic_task_limit", 1); err != nil {
				return err
			}
		}
	}
	if len(v.Reasons) == 0 && len(v.Text) > titleBytes {
		v.Reasons = append(v.Reasons, "overflow")
		v.Reason = "事项标题超过既有2000字节上限，完整内容保留为候选"
		if err := u.Overflow(ctx, "automatic_title_bytes", len(v.Text)-titleBytes); err != nil {
			return err
		}
	}
	if len(v.Reasons) == 0 {
		created := workspace.NewItem(v.Kind, v.Text)
		created.History[0].By = "ai"
		created.Evolution[0].By = "ai"
		created.ProjectID = project
		created.Sources = append(created.Sources, v.Source)
		action := string(memory.NewID())
		created.Creation = &workspace.ItemCreation{By: "background_extraction", ActionID: action, Source: &v.Source}
		label := map[string]string{"task": "待办", "idea": "想法"}[v.Kind]
		if err := u.Create(ctx, created, action, "建了"+label+"「"+v.Text+"」"); err != nil {
			return err
		}
		v.State = "accepted"
		v.ResolvedInto = created.ID
	}
	return u.SaveCandidate(ctx, v)
}

// SourceUnit is the storage of the answer for one source, in one
// transaction under the owner lock and the job lease.
type SourceUnit interface {
	CaptureUnit
	// Current is false when the job's source version was replaced.
	Current(ctx context.Context) (bool, error)
	Acknowledge(ctx context.Context) error
	// DeskTurn says whether the owner undid the whole secretary turn of a
	// desk source, and whether that turn asked to remember.
	DeskTurn(ctx context.Context, external string) (undone, remembered bool, err error)
	WakeIdeas(ctx context.Context) (bool, error)
	// Imported is true when the job's source came from an uploaded archive.
	Imported(ctx context.Context) (bool, error)
	// CandidateExists is true when the job's source version already gave a
	// candidate of the kind with the text.
	CandidateExists(ctx context.Context, kind, text string) (bool, error)
	// Remember writes the statement of a decision; memory.ErrBlocked means
	// the owner removed it before.
	Remember(ctx context.Context, decision Decision, source memory.SourceResult, project string, newAtQuote bool) (memory.ID, error)
	// ApplySignals wakes the ideas that the source gives a sign of.
	ApplySignals(ctx context.Context, source memory.Source, signals []Signal) error
	// MergeCapture closes the source's unclassified quick note.
	MergeCapture(ctx context.Context) error
	DiscardPaidResult(ctx context.Context) error
	// Complete finishes the job and records the source's extraction state.
	Complete(ctx context.Context) error
}

// ApplySource writes the answer for one source. Under the owner lock it
// checks again that the source is current and whether an undo removed its
// secretary turn while the model was asked. Each item is decided; a memory
// is remembered and its candidate accepted, a task or an idea is captured. An
// uploaded archive wakes no idea. Once anything was taken, the source's
// unclassified quick note is closed.
func ApplySource(ctx context.Context, u SourceUnit, job memory.Ref, result Result, now time.Time) error {
	current, err := u.Current(ctx)
	if err != nil {
		return err
	}
	if !current {
		return u.Acknowledge(ctx)
	}
	source := result.Source
	facts := Facts{Source: source, MentionText: result.MentionText, Location: result.Location}
	if source.Source.Connector == "desk" {
		if facts.Undone, facts.Remembered, err = u.DeskTurn(ctx, source.Source.ExternalID); err != nil {
			return err
		}
	}
	wake, err := u.WakeIdeas(ctx)
	if err != nil {
		return err
	}
	if facts.Imported, err = u.Imported(ctx); err != nil {
		return err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	accepted := 0
	seenQuotes := map[string]bool{}
	for _, raw := range result.Output.Items {
		decision := Decide(raw, facts)
		item := decision.Item
		switch decision.Action {
		case Skip:
			continue
		case Capture:
			if err := CaptureItem(ctx, u, source, item, facts.Imported, result.Project, now); err != nil {
				return err
			}
			accepted++
			continue
		}
		duplicate, err := u.CandidateExists(ctx, item.Kind, item.Text)
		if err != nil {
			return err
		}
		if duplicate && item.Kind != "memory" {
			continue
		}
		v := workspace.Candidate{ID: string(memory.NewID()), Kind: item.Kind, Text: item.Text, MemoryKind: item.Nature, Confidence: item.Confidence, Source: workspace.SourceRef{SourceID: string(job.ID), Version: job.Version, Label: source.Source.Title, Excerpt: item.Quote, At: stamp}, State: "pending", CreatedAt: stamp}
		if decision.Action == Remember {
			// The model can draw several memories from one quotation. A later
			// one must not overwrite the first.
			quoteKey := string(job.ID) + "/" + item.Quote
			id, err := u.Remember(ctx, decision, source, result.Project, seenQuotes[quoteKey])
			seenQuotes[quoteKey] = true
			if errors.Is(err, memory.ErrBlocked) {
				continue
			}
			if err != nil {
				return err
			}
			v.ResolvedInto = string(id)
			v.State = "accepted"
		}
		if !duplicate {
			if err := u.SaveCandidate(ctx, v); err != nil {
				return err
			}
		}
		accepted++
	}
	if wake {
		if err := u.ApplySignals(ctx, source.Source, UsableSignals(result.Output, source, facts.Imported)); err != nil {
			return err
		}
	}
	// The original unclassified capture remains until extraction succeeds.
	if accepted > 0 {
		if err := u.MergeCapture(ctx); err != nil {
			return err
		}
	}
	if err := u.DiscardPaidResult(ctx); err != nil {
		return err
	}
	return u.Complete(ctx)
}
