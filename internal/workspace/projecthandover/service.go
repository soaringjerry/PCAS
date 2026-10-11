// Package projecthandover owns the workspace rule for a project's handover:
// three short sections that say where the project stands, what blocks it, and
// what comes next. The model writes sentences with evidence. This package
// keeps a sentence only when its evidence was in the input and is still
// current at the time of the write. Storage supplies the input and the
// transactions.
package projecthandover

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Stage is the queue stage. One job's stage starts with
// "memory.project_handover:<project id>".
const Stage = "memory.project_handover"

// SectionSentences is the number of sentences that one section keeps. Five
// short sentences keep the overview readable in ten seconds. Sentences beyond
// the limit and sentences without valid evidence are counted, never silent.
const SectionSentences = 5

// MinimumInterval is the shortest time between two written handovers of one project.
const MinimumInterval = time.Hour

// Part is one piece of evidence in the model input.
type Part struct {
	Evidence workspace.ProjectEvidence `json:"evidence"`
	Content  any                       `json:"content"`
}

// Input is the model input. It is saved with the paid result, so that a
// recovered result is checked against the evidence that the model saw.
type Input struct {
	Project   workspace.Item              `json:"project"`
	Parts     []Part                      `json:"parts"`
	Deadlines []workspace.LibraryDeadline `json:"deadlines"`
	Events    int64                       `json:"events"`
	Hash      string                      `json:"hash"`
}

// Empty is a handover without sentences.
func Empty(project string) workspace.ProjectHandover {
	return workspace.ProjectHandover{ProjectID: project, Conclusion: []workspace.ProjectSentence{}, Blockers: []workspace.ProjectSentence{}, NextSteps: []workspace.ProjectSentence{}}
}

// Parse reads the model's three sections against the evidence in input.
// A sentence without text, without evidence, or with evidence that is not in
// input is counted as invalid. A sentence beyond the section limit is counted
// as overflow. The result is valid when at least one sentence remains.
func Parse(text string, input Input) (out workspace.ProjectHandover, invalid, overflow int, valid bool) {
	out = Empty(input.Project.ID)
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &raw) != nil {
		return out, 0, 0, false
	}
	allowed := map[workspace.ProjectEvidence]bool{}
	for _, p := range input.Parts {
		allowed[p.Evidence] = true
	}
	for _, section := range []struct {
		key    string
		target *[]workspace.ProjectSentence
	}{{"conclusion", &out.Conclusion}, {"blockers", &out.Blockers}, {"nextSteps", &out.NextSteps}} {
		var sentences []workspace.ProjectSentence
		if string(raw[section.key]) == "null" || json.Unmarshal(raw[section.key], &sentences) != nil {
			return out, invalid, overflow, false
		}
		for _, sentence := range sentences {
			ok := strings.TrimSpace(sentence.Text) != "" && len(sentence.Evidence) > 0
			for _, ref := range sentence.Evidence {
				if !allowed[ref] {
					ok = false
				}
			}
			if !ok {
				invalid++
				continue
			}
			if len(*section.target) >= SectionSentences {
				overflow++
				continue
			}
			*section.target = append(*section.target, sentence)
		}
	}
	return out, invalid, overflow, len(out.Conclusion)+len(out.Blockers)+len(out.NextSteps) > 0
}

// Store reads and writes the records of one project handover job.
type Store interface {
	// Exclusive permits one status call at a time. It returns the queue's own
	// deferral when another call holds the permission.
	Exclusive(ctx context.Context) (release func(), err error)
	// Read runs fn in one consistent read that holds the job's lease.
	Read(ctx context.Context, job worker.Job, fn func(Reader) error) error
	// Apply runs fn in one transaction that holds the owner and the job's lease.
	Apply(ctx context.Context, job worker.Job, fn func(Unit) error) error
	RetryLater(job worker.Job, code string) error
}

// Reader is the set of reads that the preparation permits.
type Reader interface {
	Project(id string) (item workspace.Item, found bool, err error)
	Handover(project string) (workspace.ProjectHandover, error)
	Input(project string) (Input, []memory.Ref, error)
	Acknowledge() error
}

// Unit is the set of reads and writes that one Apply transaction permits.
type Unit interface {
	Project(id string) (item workspace.Item, found bool, err error)
	// CurrentEvidence returns the evidence of the handover's sentences that
	// still exists and still belongs to the project.
	CurrentEvidence(input Input, handover workspace.ProjectHandover) (Input, error)
	Event(outcome, reason string, count int) error
	SaveHandover(project string, handover workspace.ProjectHandover, input Input) error
	DiscardResult() error
	Acknowledge() error
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	HasSavedResult(ctx context.Context, job worker.Job) (bool, error)
	GenerateForJob(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, err error)
}

type Service struct {
	Store     Store
	Generator Generator
}

// Process handles one project handover job.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	release, err := s.Store.Exclusive(ctx)
	if err != nil {
		return err
	}
	defer release()
	parts := strings.Split(job.Stage, ":")
	if len(parts) < 2 || !memory.ID(parts[1]).Valid() {
		return memory.ErrInvalid
	}
	project := parts[1]
	saved, err := s.Generator.HasSavedResult(ctx, job)
	if err != nil {
		return err
	}
	var input Input
	var refs []memory.Ref
	done := false
	err = s.Store.Read(ctx, job, func(r Reader) error {
		item, found, err := r.Project(project)
		if err != nil {
			return err
		}
		// A project that is gone or finished needs no handover.
		if !found || item.Status == "done" {
			done = true
			return r.Acknowledge()
		}
		if saved {
			return nil
		}
		current, err := r.Handover(project)
		if err != nil {
			return err
		}
		if !current.Stale && current.WrittenAt != nil {
			done = true
			return r.Acknowledge()
		}
		if current.WrittenAt != nil {
			written, err := time.Parse(time.RFC3339Nano, *current.WrittenAt)
			if err != nil {
				return err
			}
			if time.Now().Before(written.Add(MinimumInterval)) {
				return &worker.JobError{Code: "project_handover_interval", Until: written.Add(MinimumInterval), NoAttempt: true}
			}
		}
		input, refs, err = r.Input(project)
		return err
	})
	if err != nil || done {
		return err
	}
	record, _ := json.Marshal(input)
	record, output, err := s.Generator.GenerateForJob(ctx, job, "project_handover", prompts.Must("project-handover"), "", record, refs)
	if err != nil {
		return err
	}
	// A recovered result carries the input that its call used.
	if err = json.Unmarshal(record, &input); err != nil {
		return err
	}
	invalidOutput := false
	err = s.Store.Apply(ctx, job, func(u Unit) error {
		var err error
		invalidOutput, err = apply(u, project, input, output)
		return err
	})
	if err != nil {
		return err
	}
	if invalidOutput {
		// The discarded output and the failure count are committed. The queue
		// tries the job again after its delay.
		return s.Store.RetryLater(job, "project_handover_invalid_output")
	}
	return nil
}

func apply(u Unit, project string, input Input, output string) (invalidOutput bool, err error) {
	// Check the paid snapshot first, then only its referenced records at write
	// time. Deleted or superseded evidence cannot become a new saved sentence.
	out, invalid, overflow, valid := Parse(output, input)
	if valid {
		current, err := u.CurrentEvidence(input, out)
		if err != nil {
			return false, err
		}
		data, _ := json.Marshal(out)
		var removed int
		out, removed, _, valid = Parse(string(data), current)
		invalid += removed
		if valid && len(out.Conclusion)+len(out.Blockers)+len(out.NextSteps) == 0 {
			valid = false
		}
	}
	if invalid > 0 {
		if err := u.Event("overflow", "ungrounded_sentences", invalid); err != nil {
			return false, err
		}
	}
	if overflow > 0 {
		if err := u.Event("overflow", "section_sentence_limit", overflow); err != nil {
			return false, err
		}
	}
	if !valid {
		if err := u.DiscardResult(); err != nil {
			return false, err
		}
		return true, u.Event("failure", "project_handover_invalid_output", 1)
	}
	item, found, err := u.Project(project)
	if err != nil {
		return false, err
	}
	if found && item.Status != "done" {
		if err := u.SaveHandover(project, out, input); err != nil {
			return false, err
		}
	}
	if err := u.DiscardResult(); err != nil {
		return false, err
	}
	return false, u.Acknowledge()
}
