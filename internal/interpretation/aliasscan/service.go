// Package aliasscan owns the memory interpretation rule that finds names
// which can refer to one entity. The model groups numbered names. This package
// accepts only groups of names from its input and proposes only pairs whose
// types can be one entity. A proposal is a candidate for the later comparison;
// it merges nothing. Storage supplies the batches and the transactions.
package aliasscan

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Stage is the queue stage. One job's stage is
// "memory.entity_candidates:<rule version>:<nonce>".
const Stage = "memory.entity_candidates"

// HourlyCalls is the number of alias scans that can reserve a model call in one
// hour. Unused capacity is not lent to another stage.
const HourlyCalls = 6

// PairTypesAllowed reports whether names of these two types can be one entity.
// Two names of one type can, for the five entity types. A topic can also be a
// place or an organization.
func PairTypesAllowed(a, b string) bool {
	one := func(t string, types ...string) bool {
		for _, v := range types {
			if t == v {
				return true
			}
		}
		return false
	}
	if a == b {
		return one(a, "person", "place", "organization", "topic", "project")
	}
	return a == "topic" && one(b, "place", "organization") || b == "topic" && one(a, "place", "organization")
}

// Validate reads the model's groups. Each number must name an entity of the
// input, which has count entities numbered from one.
func Validate(output string, count int) (groups [][]int, valid bool) {
	var answer struct {
		Groups [][]int `json:"groups"`
	}
	valid = modelcall.DecodeStrict([]byte(output), &answer) == nil && answer.Groups != nil
	for _, group := range answer.Groups {
		for _, n := range group {
			if n < 1 || n > count {
				valid = false
			}
		}
	}
	return answer.Groups, valid
}

// Pairs lists the candidate pairs of the groups, by entity number. types holds
// the type of each numbered entity. A pair of one entity with itself and a
// pair of types that cannot be one entity are left out.
func Pairs(groups [][]int, types []string) [][2]int {
	pairs := [][2]int{}
	for _, group := range groups {
		for i, a := range group {
			for _, b := range group[i+1:] {
				if a != b && PairTypesAllowed(types[a-1], types[b-1]) {
					pairs = append(pairs, [2]int{a, b})
				}
			}
		}
	}
	return pairs
}

// Store opens the job's session.
type Store interface {
	// Session holds the one connection that scans and comparisons share. It
	// returns the queue's own deferral when another call holds it.
	Session(ctx context.Context) (session Session, release func(), err error)
	RetryLater(job worker.Job, code string) error
}

// Session is the set of reads and writes of one job.
type Session interface {
	// Ready returns the queue's deferral when no provider is configured.
	Ready() error
	// NextBatch returns the input record of the next batch of names and the
	// records that it names. found is false when no batch remains.
	NextBatch(ctx context.Context, job worker.Job) (record json.RawMessage, refs []memory.Ref, found bool, err error)
	// Finish acknowledges a job that has no batch.
	Finish(ctx context.Context, job worker.Job) error
	DiscardResult(ctx context.Context, job worker.Job) error
	// Apply runs fn in one transaction that holds the scan queue and the job's
	// lease. record and refs are the saved input of the paid result.
	Apply(ctx context.Context, job worker.Job, record json.RawMessage, refs []memory.Ref, fn func(Unit) error) error
}

// Unit is the set of writes that one Apply transaction permits.
type Unit interface {
	// ProposePair records two numbered entities as a candidate. Storage skips
	// a pair whose names changed during the call.
	ProposePair(a, b int) error
	// MarkScanned records that this batch was scanned.
	MarkScanned() error
	DiscardResult() error
	Acknowledge() error
}

// Generator submits the job's model call and keeps its paid result.
type Generator interface {
	HasSavedResult(ctx context.Context, job worker.Job) (bool, error)
	// GenerateWithRefs also returns the records that the saved result names.
	GenerateWithRefs(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, savedRefs []memory.Ref, err error)
}

type Service struct {
	Store     Store
	Generator Generator
}

// Process handles one alias scan job.
func (s Service) Process(ctx context.Context, job worker.Job) error {
	parts := strings.SplitN(job.Stage, ":", 3)
	if len(parts) != 3 || parts[0] != Stage {
		return memory.ErrInvalid
	}
	if version, err := strconv.Atoi(parts[1]); err != nil || version < 1 {
		return memory.ErrInvalid
	}
	session, release, err := s.Store.Session(ctx)
	if err != nil {
		return err
	}
	defer release()
	saved, err := s.Generator.HasSavedResult(ctx, job)
	if err != nil {
		return err
	}
	record, refs := json.RawMessage(`{"marker":""}`), []memory.Ref{}
	if !saved {
		if err := session.Ready(); err != nil {
			return err
		}
		var found bool
		record, refs, found, err = session.NextBatch(ctx, job)
		if err != nil {
			return err
		}
		if !found {
			return session.Finish(ctx, job)
		}
	}
	record, output, refs, err := s.Generator.GenerateWithRefs(ctx, job, "alias_scan", prompts.Must("entity-candidates"), record, refs)
	if err != nil {
		return err
	}
	// A recovered result carries the names that its call used.
	var input struct {
		Entities []struct {
			Type string `json:"type"`
		} `json:"entities"`
	}
	if json.Unmarshal(record, &input) != nil || len(refs) != len(input.Entities) {
		return memory.ErrInvalid
	}
	groups, valid := Validate(output, len(input.Entities))
	if !valid {
		if err := session.DiscardResult(ctx, job); err != nil {
			return err
		}
		return s.Store.RetryLater(job, "invalid_entity_candidates_output")
	}
	types := make([]string, len(input.Entities))
	for i, e := range input.Entities {
		types[i] = e.Type
	}
	return session.Apply(ctx, job, record, refs, func(u Unit) error {
		for _, pair := range Pairs(groups, types) {
			if err := u.ProposePair(pair[0], pair[1]); err != nil {
				return err
			}
		}
		if err := u.MarkScanned(); err != nil {
			return err
		}
		if err := u.DiscardResult(); err != nil {
			return err
		}
		return u.Acknowledge()
	})
}
