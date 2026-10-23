package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// EarlierMemory is a memory that an earlier segment of the conversation
// wrote and that the owner neither confirmed nor edited. The model of a later
// segment can withdraw it by its Ref.
type EarlierMemory struct {
	Ref   int        `json:"ref"`
	Text  string     `json:"text"`
	Claim memory.Ref `json:"-"`
}

// StateTarget is a source whose extraction state is written. Counted is
// true when its memories decide between done and empty.
type StateTarget struct {
	Source  memory.Ref
	Counted bool
}

// ConversationUnit is the storage of one conversation job, in one
// transaction under the owner lock and the job lease. Each method reads or
// writes; none decides.
type ConversationUnit interface {
	// Sources are the current messages of the conversation in their order,
	// with the historical branches.
	Sources(ctx context.Context, conversation string) ([]memory.SourceResult, error)
	// Extracted is true when every current message of the conversation has
	// an extraction of the current version.
	Extracted(ctx context.Context, conversation string) (bool, error)
	// Enqueue queues the job of one segment, anchored at its first message.
	Enqueue(ctx context.Context, anchor memory.Ref, run string, segment, next int) error
	// ReplaceMemories removes the memories that only these sources support
	// and that the owner neither confirmed nor edited. Answers that used them
	// keep their text.
	ReplaceMemories(ctx context.Context, sources []memory.SourceResult) error
	// Withdraw removes one memory. Answers that used it keep their text.
	Withdraw(ctx context.Context, claim memory.Ref) error
	// WriteStates records the extraction state of each source: state, or
	// done or empty by its memories when state is empty.
	WriteStates(ctx context.Context, targets []StateTarget, state string) error
	// RetireJobs finishes the other extraction jobs of the conversation that
	// do not belong to the run.
	RetireJobs(ctx context.Context, conversation, run string) error
	Acknowledge(ctx context.Context) error
	DiscardPaidResult(ctx context.Context) error
	Earlier(ctx context.Context, run string) ([]EarlierMemory, error)
	Timezone(ctx context.Context) (string, error)
	// Imported is true when the source came from an uploaded archive.
	Imported(ctx context.Context, source memory.Ref) (bool, error)
	// Capture makes the task or the idea of an item.
	Capture(ctx context.Context, source memory.SourceResult, item Item, imported bool) error
	// Remember writes the statement of a decision from the source with the
	// run and the ref next. assigned is true when the memory took that ref.
	// memory.ErrBlocked means the owner removed it before.
	Remember(ctx context.Context, decision Decision, source memory.SourceResult, run string, next int, newAtQuote bool) (assigned bool, err error)
}

// ConversationSegmentWork is one segment of a conversation that is ready
// for the model: the messages that it reads and what it is checked against.
type ConversationSegmentWork struct {
	Conversation, Run string
	Sources, Current  []memory.SourceResult
	Segments          []ConversationSegment
	Ordinal, Next     int
	Earlier           []EarlierMemory
	Timezone          string
}

func (w ConversationSegmentWork) segment() ConversationSegment { return w.Segments[w.Ordinal] }

// Prompt is the model input of the segment. A later segment also reads the
// memories of the earlier ones.
func (w ConversationSegmentWork) Prompt() string {
	segment := w.segment()
	input := map[string]any{"messages": segment.Messages, "context_messages": segment.Context, "timezone": w.Timezone}
	if w.Ordinal > 0 {
		input["earlier_memories"] = w.Earlier
	}
	b, _ := json.Marshal(input)
	return string(b)
}

// HasOwnerText is true when the segment holds a message of the owner. A
// segment without one is applied without a model call.
func (w ConversationSegmentWork) HasOwnerText() bool {
	for _, message := range w.segment().Messages {
		if message.Role == "user" && strings.TrimSpace(message.Text) != "" {
			return true
		}
	}
	return false
}

// Refs are the records that the model input holds.
func (w ConversationSegmentWork) Refs() []memory.Ref {
	segment := w.segment()
	refs := []memory.Ref{}
	for _, message := range append(append([]ConversationMessage{}, segment.Context...), segment.Messages...) {
		refs = append(refs, w.Current[message.Index-1].Source.Ref)
	}
	for _, item := range w.Earlier {
		refs = append(refs, item.Claim)
	}
	return refs
}

// targets are the sources with their counting rule: only the owner's
// messages of the current branch count memories.
func targets(sources []memory.SourceResult) []StateTarget {
	out := make([]StateTarget, 0, len(sources))
	for _, source := range sources {
		out = append(out, StateTarget{Source: source.Source.Ref, Counted: source.Context.Role == "user" && source.Context.Branch != "historical"})
	}
	return out
}

// PrepareConversation handles a job of the conversation and returns its
// segment, or nil when the job finished without one. A message's own job
// starts the run: a conversation without segments loses its automatic
// memories, and an unfinished one queues its first segment. A segment job of
// a changed conversation starts the run again; stale model results are never
// applied.
func PrepareConversation(ctx context.Context, u ConversationUnit, job worker.Job, conversation string) (*ConversationSegmentWork, error) {
	sources, err := u.Sources(ctx, conversation)
	if err != nil {
		return nil, err
	}
	current := CurrentBranch(sources)
	segments := SplitConversation(current)
	run := ConversationRun(sources)
	if !strings.HasPrefix(job.Stage, ConversationPrefix) {
		if len(segments) == 0 {
			if err := u.ReplaceMemories(ctx, sources); err != nil {
				return nil, err
			}
			if err := u.WriteStates(ctx, targets(sources), ""); err != nil {
				return nil, err
			}
			if err := u.RetireJobs(ctx, conversation, run); err != nil {
				return nil, err
			}
			return nil, u.Acknowledge(ctx)
		}
		extracted, err := u.Extracted(ctx, conversation)
		if err != nil {
			return nil, err
		}
		if !extracted {
			// Only one segment is runnable at a time. Its successor is queued
			// in the transaction of its results, acknowledgement, and ref counter.
			if err := u.Enqueue(ctx, current[0].Source.Ref, run, 0, 1); err != nil {
				return nil, err
			}
		}
		// The run is durable now; the other messages' jobs need not wait.
		if err := u.RetireJobs(ctx, conversation, run); err != nil {
			return nil, err
		}
		return nil, u.Acknowledge(ctx)
	}
	jobRun, ordinal, next, err := ParseConversationStage(job.Stage)
	if err != nil {
		return nil, err
	}
	if jobRun != run || ordinal >= len(segments) {
		if len(current) > 0 {
			if err := u.Enqueue(ctx, current[0].Source.Ref, run, 0, 1); err != nil {
				return nil, err
			}
		}
		return nil, u.Acknowledge(ctx)
	}
	earlier, err := u.Earlier(ctx, run)
	if err != nil {
		return nil, err
	}
	timezone, err := u.Timezone(ctx)
	if err != nil {
		return nil, err
	}
	return &ConversationSegmentWork{Conversation: conversation, Run: run, Sources: sources, Current: current, Segments: segments, Ordinal: ordinal, Next: next, Earlier: earlier, Timezone: timezone}, nil
}

// ApplyConversation writes the answer of a segment: the first segment
// replaces the automatic memories of the conversation, a later one removes
// what the model withdrew, and each item is decided against the messages
// of the segment. It records which messages are processed, queues the next
// segment or finishes the run, and finishes the job. A conversation that
// changed while the model was asked starts its run again.
func ApplyConversation(ctx context.Context, u ConversationUnit, w ConversationSegmentWork, output ConversationOutput) error {
	location, err := time.LoadLocation(w.Timezone)
	if err != nil {
		return err
	}
	fresh, err := u.Sources(ctx, w.Conversation)
	if err != nil {
		return err
	}
	if run := ConversationRun(fresh); run != w.Run {
		if live := CurrentBranch(fresh); len(live) > 0 {
			if err := u.Enqueue(ctx, live[0].Source.Ref, run, 0, 1); err != nil {
				return err
			}
		}
		return u.Acknowledge(ctx)
	}
	if w.Ordinal == 0 {
		if err := u.ReplaceMemories(ctx, w.Sources); err != nil {
			return err
		}
	} else {
		// Read again, so that a confirmation or an edit of the owner while
		// the model was asked protects the memory.
		eligible, err := u.Earlier(ctx, w.Run)
		if err != nil {
			return err
		}
		withdraw := map[int]bool{}
		for _, ref := range output.Withdraw {
			withdraw[ref] = true
		}
		for _, item := range eligible {
			if withdraw[item.Ref] {
				if err := u.Withdraw(ctx, item.Claim); err != nil {
					return err
				}
			}
		}
	}
	segment := w.segment()
	grounding := ""
	allowed := map[int]ConversationMessage{}
	for _, message := range segment.Messages {
		allowed[message.Index] = message
		grounding += "\n" + message.Text
	}
	for _, message := range segment.Context {
		grounding += "\n" + message.Text
	}
	next := w.Next
	seenQuotes := map[string]bool{}
	for _, raw := range output.Items {
		decision, source := DecideConversation(raw, allowed, w.Current, grounding, location)
		switch decision.Action {
		case Skip:
			continue
		case Capture:
			imported, err := u.Imported(ctx, source.Source.Ref)
			if err != nil {
				return err
			}
			if err := u.Capture(ctx, source, decision.Item, imported); err != nil {
				return err
			}
			continue
		}
		// The model can draw several memories from one quotation. A later
		// one must not overwrite the first.
		quoteKey := string(source.Source.Ref.ID) + "/" + decision.Item.Quote
		assigned, err := u.Remember(ctx, decision, source, w.Run, next, seenQuotes[quoteKey])
		seenQuotes[quoteKey] = true
		if errors.Is(err, memory.ErrBlocked) {
			continue
		}
		if err != nil {
			return err
		}
		if assigned {
			next++
		}
	}
	processed := []memory.SourceResult{}
	for _, message := range segment.Messages {
		if message.Part == message.Parts {
			processed = append(processed, w.Current[message.Index-1])
		}
	}
	for _, source := range w.Sources {
		if source.Context.Branch == "historical" {
			processed = append(processed, source)
		}
	}
	if err := u.WriteStates(ctx, targets(processed), ""); err != nil {
		return err
	}
	if err := u.RetireJobs(ctx, w.Conversation, w.Run); err != nil {
		return err
	}
	if w.Ordinal+1 < len(w.Segments) {
		anchor := w.Current[w.Segments[w.Ordinal+1].Messages[0].Index-1].Source.Ref
		if err := u.Enqueue(ctx, anchor, w.Run, w.Ordinal+1, next); err != nil {
			return err
		}
	} else {
		// Withdrawals may change the counts of earlier messages. Refresh the
		// whole conversation and retire the redundant message jobs.
		if err := u.WriteStates(ctx, targets(w.Sources), ""); err != nil {
			return err
		}
		if err := u.RetireJobs(ctx, w.Conversation, w.Run); err != nil {
			return err
		}
	}
	if err := u.DiscardPaidResult(ctx); err != nil {
		return err
	}
	return u.Acknowledge(ctx)
}

// FailedConversation marks the messages of the job's segment and of the
// segments after it as failed, when the job belongs to the current run.
func FailedConversation(ctx context.Context, u ConversationUnit, job worker.Job, conversation string) error {
	sources, err := u.Sources(ctx, conversation)
	if err != nil {
		return err
	}
	run, ordinal, _, err := ParseConversationStage(job.Stage)
	if err != nil || run != ConversationRun(sources) {
		return err
	}
	current := CurrentBranch(sources)
	segments := SplitConversation(current)
	if ordinal >= len(segments) {
		return nil
	}
	remaining := []memory.SourceResult{}
	for _, segment := range segments[ordinal:] {
		for _, message := range segment.Messages {
			remaining = append(remaining, current[message.Index-1])
		}
	}
	return u.WriteStates(ctx, targets(remaining), "failed")
}
