package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Stage is the queue stage of an extraction job. A window of a long source
// has the stage "source.extract:<start>".
const Stage = "source.extract"

// Input is what storage reads for the model input of one source.
type Input struct {
	// Project is the project of the source, or empty.
	Project    string
	Timezone   string
	Adjacent   []map[string]any
	Conditions []map[string]string
}

// Result is the read answer for one source text.
type Result struct {
	// Source is the source with the text that the model read.
	Source      memory.SourceResult
	Project     string
	Output      Output
	MentionText string
	Location    *time.Location
}

// Store is the set of reads and writes of one extraction job.
type Store interface {
	// Source returns the job's source and whether it came from an uploaded archive.
	Source(ctx context.Context, job worker.Job) (source memory.SourceResult, imported bool, err error)
	// Current reports under the job's lease whether the job's source version
	// is still current. It acknowledges the job when the version is not.
	Current(ctx context.Context, job worker.Job) (bool, error)
	// FinishEmpty acknowledges the job and records that the source has
	// nothing to extract.
	FinishEmpty(ctx context.Context, job worker.Job) error
	// Conversation reads the job's segment of the conversation of an imported
	// message under the job's lease. found is false when the job finished
	// without a segment to extract.
	Conversation(ctx context.Context, job worker.Job, conversation string) (work ConversationWork, found bool, err error)
	// SplitWindows queues one job for each window and finishes this job.
	SplitWindows(ctx context.Context, job worker.Job, starts []int, imported bool) error
	// Configured returns the queue's failure when no extraction model is set.
	Configured() error
	// Ready returns the queue's failure when the model cannot be used now.
	Ready() error
	Input(ctx context.Context, job worker.Job, source memory.SourceResult) (Input, error)
	// Unpaid reports whether a call with this text costs nothing.
	Unpaid(text string) bool
	// DiscardResult removes the saved result of an answer that was not valid.
	DiscardResult(ctx context.Context, job worker.Job) error
	// Apply writes the items of one answer in one transaction and finishes
	// the job. It writes nothing when the source version is no longer current.
	Apply(ctx context.Context, job worker.Job, result Result) error
	// Failed records that the extraction of the source stopped.
	Failed(ctx context.Context, job worker.Job) error
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, err error)
}

// ConversationWork is one prepared segment of an imported conversation.
type ConversationWork interface {
	// Prompt is the model input of the segment.
	Prompt() string
	// Refs are the records that the model input holds.
	Refs() []memory.Ref
	// HasOwnerText reports whether the segment holds a message of the owner.
	// A segment without one is applied without a model call.
	HasOwnerText() bool
	// Apply writes the statements of the segment, queues the next segment,
	// and finishes the job.
	Apply(ctx context.Context, output ConversationOutput) error
}

// Service reads statements, tasks, and ideas from one source.
type Service struct {
	Store     Store
	Generator Generator
	// MaxAttempts is the queue's attempt limit of one job.
	MaxAttempts int
}

// Process handles one extraction job. A job that the queue will not try
// again marks the source's extraction as failed. A budget deferral does not.
func (s Service) Process(ctx context.Context, job worker.Job) (err error) {
	defer func() {
		var failure *worker.JobError
		if errors.As(err, &failure) && failure.Code != "budget_deferred" && (!failure.Retry || job.Attempts >= s.MaxAttempts) {
			if persistErr := s.Store.Failed(ctx, job); persistErr != nil {
				err = persistErr
			}
		}
	}()
	source, imported, err := s.Store.Source(ctx, job)
	if err != nil {
		return err
	}
	groundingText := source.Source.Text
	if current, err := s.Store.Current(ctx, job); err != nil || !current {
		return err
	}
	// What the system wrote itself has nothing to extract.
	if oneOf(source.Source.Connector, "actions", "corrections", "memory-input") {
		return s.Store.FinishEmpty(ctx, job)
	}
	if imported && source.Context != nil && source.Context.Conversation != "" {
		return s.conversation(ctx, job, source.Context.Conversation)
	}
	if imported && source.Context != nil && oneOf(source.Context.Role, "assistant", "system", "tool") {
		return s.Store.FinishEmpty(ctx, job)
	}
	if err := s.Store.Configured(); err != nil {
		return err
	}
	// Long imports are separate fenced jobs with overlapping context windows.
	text := []rune(source.Source.Text)
	if starts := WindowStarts(len(text)); len(starts) > 0 && job.Stage == Stage {
		return s.Store.SplitWindows(ctx, job, starts, imported)
	}
	if parts := strings.SplitN(job.Stage, ":", 2); len(parts) == 2 {
		start, err := strconv.Atoi(parts[1])
		if err != nil {
			return memory.ErrInvalid
		}
		var ok bool
		if source.Source.Text, ok = WindowText(text, start); !ok {
			return memory.ErrInvalid
		}
	}
	if err := s.Store.Ready(); err != nil {
		return err
	}
	input, err := s.Store.Input(ctx, job, source)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return err
	}
	modelInput, err := json.Marshal(map[string]any{"source_project_id": input.Project, "source": source.Source.Text, "source_context": source.Context, "expressed_at": ExpressedAt(source), "timezone": input.Timezone, "adjacent_messages": input.Adjacent, "pending_conditions": input.Conditions})
	if err != nil {
		return err
	}
	mentionText := groundingText
	for _, message := range input.Adjacent {
		if text, ok := message["text"].(string); ok {
			mentionText += "\n" + text
		}
	}
	record, err := json.Marshal(map[string]any{"rawPrompt": string(modelInput)})
	if err != nil {
		return err
	}
	instructions := prompts.Must("structured-extraction")
	// Reserve before submitting a background generation. Repeated processing can
	// retry DB work, but an ambiguous costly request requires explicit user retry.
	saved, answer, err := s.Generator.GenerateForJob(ctx, job, "extraction", instructions, string(modelInput), record, []memory.Ref{job.Record})
	if err != nil {
		return err
	}
	// A recovered result carries the project that its call used.
	var paid struct {
		RawPrompt string `json:"rawPrompt"`
	}
	if err = json.Unmarshal(saved, &paid); err != nil {
		return err
	}
	var paidScope struct {
		ProjectID string `json:"source_project_id"`
	}
	if err = json.Unmarshal([]byte(paid.RawPrompt), &paidScope); err != nil {
		return err
	}
	output, valid := ParseOutput(answer)
	if !valid {
		if err := s.Store.DiscardResult(ctx, job); err != nil {
			return err
		}
		// Only a call that costs nothing is made again without the owner.
		return &worker.JobError{Code: "model_output_invalid", Retry: s.Store.Unpaid(instructions.Text() + string(modelInput))}
	}
	return s.Store.Apply(ctx, job, Result{Source: source, Project: paidScope.ProjectID, Output: output, MentionText: mentionText, Location: location})
}

// conversation extracts one segment of the conversation of an imported
// message. An answer that is not valid is discarded; the queue tries a free
// call again.
func (s Service) conversation(ctx context.Context, job worker.Job, conversation string) error {
	work, found, err := s.Store.Conversation(ctx, job, conversation)
	if err != nil || !found {
		return err
	}
	output := ConversationOutput{}
	if work.HasOwnerText() {
		if err := s.Store.Configured(); err != nil {
			return err
		}
		if err := s.Store.Ready(); err != nil {
			return err
		}
		prompt := work.Prompt()
		record, err := json.Marshal(map[string]any{"rawPrompt": prompt})
		if err != nil {
			return err
		}
		instructions := prompts.Must("conversation-extraction")
		_, answer, err := s.Generator.GenerateForJob(ctx, job, "extraction", instructions, prompt, record, work.Refs())
		if err != nil {
			return err
		}
		var valid bool
		if output, valid = ParseConversationOutput(answer); !valid {
			if err := s.Store.DiscardResult(ctx, job); err != nil {
				return err
			}
			return &worker.JobError{Code: "model_output_invalid", Retry: s.Store.Unpaid(instructions.Text() + prompt)}
		}
	}
	return work.Apply(ctx, output)
}
