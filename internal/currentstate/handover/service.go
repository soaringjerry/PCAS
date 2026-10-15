// Package handover owns the current-state rule for the owner's global
// handover: nine fixed sections that tell a new assistant who the owner is
// and how to work with them. The model writes the sections with references to
// numbered memories. This package accepts the text only when each section is
// grounded in memories that are not inferred. Storage supplies the input and
// the transactions.
package handover

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Stage is the queue stage of a global handover job.
const Stage = "memory.handover"

// HourlyCalls is the number of handovers that can reserve a model call in one
// hour. Unused capacity is not lent to another stage.
const HourlyCalls = 2

// Titles are the nine sections, in their fixed order.
var Titles = []string{"他是谁和现在的处境", "怎么跟他配合", "现在手上的事", "时间和节奏", "资源和限制", "口味和标准", "重要的人", "他的叫法", "他看重什么"}

// NoBasis is the body of a section that has no memory to rely on.
const NoBasis = "（暂无依据）"

// BodyCharacters is the longest handover that is accepted.
const BodyCharacters = 1800

// MinimumInterval is the shortest time between two written handovers.
const MinimumInterval = time.Hour

// Section is one section of the model's output.
type Section struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Refs  []int  `json:"refs"`
}

// Output is the model's output.
type Output struct {
	Sections []Section `json:"sections"`
}

// Empty is the handover of an owner without usable memories.
func Empty() string {
	sections := []string{}
	for _, title := range Titles {
		sections = append(sections, "## "+title+"\n"+NoBasis)
	}
	return strings.Join(sections, "\n\n")
}

// Parse accepts the model's output and returns the handover text.
// trusts holds the trust of each numbered memory of the input, in order.
// The output must have the nine sections in order. A section with text must
// refer to at least one memory, each reference must exist, and none can be an
// inferred memory. A section without a basis must say so and refer to nothing.
func Parse(text string, trusts []string) (string, bool) {
	var output Output
	if json.Unmarshal([]byte(text), &output) != nil || len(output.Sections) != len(Titles) {
		return "", false
	}
	sections := []string{}
	for i, section := range output.Sections {
		if section.Title != Titles[i] {
			return "", false
		}
		body := strings.TrimSpace(section.Body)
		grounded := len(section.Refs) > 0
		for _, n := range section.Refs {
			if n < 1 || n > len(trusts) || trusts[n-1] == "inferred" {
				grounded = false
				break
			}
		}
		if !grounded {
			if body != NoBasis || len(section.Refs) != 0 {
				return "", false
			}
		} else if body == "" {
			return "", false
		}
		sections = append(sections, "## "+Titles[i]+"\n"+body)
	}
	body := strings.Join(sections, "\n\n")
	if utf8.RuneCountInString(body) > BodyCharacters {
		return "", false
	}
	return body, true
}

// Store reads and writes the records of one global handover job.
type Store interface {
	// Exclusive permits one status call at a time.
	Exclusive(ctx context.Context) (release func(), err error)
	// Read runs fn in one consistent read that holds the job's lease.
	Read(ctx context.Context, job worker.Job, fn func(Reader) error) error
	DiscardResult(ctx context.Context, job worker.Job) error
	RetryLater(job worker.Job, code string) error
	// Apply runs fn in one transaction that holds the job's lease.
	Apply(ctx context.Context, job worker.Job, fn func(Unit) error) error
}

// Reader is the set of reads that the preparation permits.
type Reader interface {
	// WrittenAt is the time of the current handover, or nil when there is none.
	WrittenAt() (*time.Time, error)
	// Input assembles the input record that is saved with the paid result.
	Input() (record json.RawMessage, refs []memory.Ref, err error)
}

// Unit is the set of writes that one Apply transaction permits.
type Unit interface {
	// SaveHandover stores the text with the hash of the input that it was
	// written from. Storage marks it stale when the input changed during the call.
	SaveHandover(body, inputHash string) error
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

// Process handles one global handover job.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	release, err := s.Store.Exclusive(ctx)
	if err != nil {
		return err
	}
	defer release()
	saved, err := s.Generator.HasSavedResult(ctx, job)
	if err != nil {
		return err
	}
	record := json.RawMessage(`{}`)
	var refs []memory.Ref
	if !saved {
		err = s.Store.Read(ctx, job, func(r Reader) error {
			written, err := r.WrittenAt()
			if err != nil {
				return err
			}
			if written != nil && time.Now().Before(written.Add(MinimumInterval)) {
				return &worker.JobError{Code: "handover_interval", Until: written.Add(MinimumInterval), NoAttempt: true}
			}
			record, refs, err = r.Input()
			return err
		})
		if err != nil {
			return err
		}
	}
	record, output, err := s.Generator.GenerateForJob(ctx, job, "handover", prompts.Must("handover"), "", record, refs)
	if err != nil {
		return err
	}
	// A recovered result carries the input that its call used.
	var snapshot struct {
		Input struct {
			Memories []struct {
				Trust string `json:"trust"`
			} `json:"memories"`
			InputHash string `json:"inputHash"`
		} `json:"input"`
	}
	if err := json.Unmarshal(record, &snapshot); err != nil {
		return err
	}
	trusts := make([]string, len(snapshot.Input.Memories))
	for i, m := range snapshot.Input.Memories {
		trusts[i] = m.Trust
	}
	body, valid := Parse(output, trusts)
	if !valid {
		if err := s.Store.DiscardResult(ctx, job); err != nil {
			return err
		}
		return s.Store.RetryLater(job, "handover_invalid_output")
	}
	return s.Store.Apply(ctx, job, func(u Unit) error {
		if err := u.SaveHandover(body, snapshot.Input.InputHash); err != nil {
			return err
		}
		if err := u.DiscardResult(); err != nil {
			return err
		}
		return u.Acknowledge()
	})
}
