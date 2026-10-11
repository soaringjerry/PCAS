package comparison

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

// EntityStage is the queue stage of an entity comparison job. One job's stage
// is "memory.entity_compare:<rule version>:<nonce>".
const EntityStage = "memory.entity_compare"

// EntityStore opens the job's session.
type EntityStore interface {
	// Session holds the one connection that scans and comparisons share.
	Session(ctx context.Context) (session EntitySession, release func(), err error)
	RetryLater(job worker.Job, code string) error
}

// EntitySession is the set of reads and writes of one job.
type EntitySession interface {
	// Ready returns the queue's deferral when no provider can generate text.
	Ready() error
	// NextPair returns the input record of the next candidate pair and the
	// records that it names. It records how many memories the samples leave
	// out. found is false when no pair remains.
	NextPair(ctx context.Context, job worker.Job, version int) (record json.RawMessage, refs []memory.Ref, found bool, err error)
	// Finish acknowledges a job that has no pair.
	Finish(ctx context.Context, job worker.Job) error
	// RecordInvalid removes the saved result and records the unreadable
	// output with the pair, so that the answer stays visible.
	RecordInvalid(ctx context.Context, job worker.Job, version int, record json.RawMessage, refs []memory.Ref, output string) error
	// Apply runs fn in one transaction that holds the comparison queue and
	// the job's lease. merge selects the longer write limit of a merge.
	Apply(ctx context.Context, job worker.Job, version int, record json.RawMessage, refs []memory.Ref, merge bool, fn func(EntityUnit) error) error
}

// EntityUnit is the set of reads and writes that one Apply transaction permits.
type EntityUnit interface {
	// NamesUnchanged reports whether both names are still the names that the model saw.
	NamesUnchanged() (bool, error)
	// EvidenceCurrent reports whether each sampled memory is still current.
	EvidenceCurrent() (bool, error)
	// LockEntities locks both entities and reports whether their names are
	// still unchanged under that lock.
	LockEntities() (bool, error)
	// Merge merges the other entity into the entity at index keep.
	Merge(keep int) error
	Event(outcome, reason string, count int) error
	// Receipt records the decision for the pair. same is nil for an unreadable output.
	Receipt(same *bool, output string) error
	DiscardResult() error
	Acknowledge() error
}

// EntityGenerator submits the job's model call and keeps its paid result.
type EntityGenerator interface {
	HasSavedResult(ctx context.Context, job worker.Job) (bool, error)
	GenerateWithRefs(ctx context.Context, job worker.Job, purpose string, instructions prompts.Definition, record json.RawMessage, refs []memory.Ref) (saved json.RawMessage, output string, savedRefs []memory.Ref, err error)
}

// EntityService decides whether two names are one entity.
type EntityService struct {
	Store     EntityStore
	Generator EntityGenerator
	// Version is the current entity comparison rule version. A queued stage can
	// name an older version; the decision is recorded under this one.
	Version int
}

// ValidateEntityAnswer reads the model's answer. A positive answer must say
// which of the two names is kept.
func ValidateEntityAnswer(output string) (same bool, keep int, valid bool) {
	var answer struct {
		Same *bool `json:"same"`
		Keep *int  `json:"keep"`
	}
	valid = modelcall.DecodeStrict([]byte(output), &answer) == nil && answer.Same != nil && (!*answer.Same || answer.Keep != nil && (*answer.Keep == 1 || *answer.Keep == 2))
	if !valid {
		return false, 0, false
	}
	if answer.Keep != nil {
		keep = *answer.Keep
	}
	return *answer.Same, keep, true
}

// Process handles one entity comparison job.
// A merge needs three things at the time of the write: both names unchanged,
// each sampled memory still current, and both names unchanged again under the
// entities' lock. A negative answer needs unchanged names only.
func (s EntityService) Process(ctx context.Context, job worker.Job) error {
	parts := strings.SplitN(job.Stage, ":", 3)
	if len(parts) != 3 || s.Version < 1 {
		return memory.ErrInvalid
	}
	if v, err := strconv.Atoi(parts[1]); err != nil || v < 1 {
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
	record, refs := json.RawMessage("null"), []memory.Ref{}
	if !saved {
		if err := session.Ready(); err != nil {
			return err
		}
		var found bool
		record, refs, found, err = session.NextPair(ctx, job, s.Version)
		if err != nil {
			return err
		}
		if !found {
			return session.Finish(ctx, job)
		}
	}
	record, output, refs, err := s.Generator.GenerateWithRefs(ctx, job, "alias_confirm", prompts.Must("entity-compare"), record, refs)
	if err != nil {
		return err
	}
	// A recovered result carries the pair that its call used.
	var pair struct {
		Entities [2]struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			MemoryCount int    `json:"memoryCount"`
		} `json:"entities"`
	}
	if string(record) == "null" || json.Unmarshal(record, &pair) != nil {
		return memory.ErrInvalid
	}
	same, keep, valid := ValidateEntityAnswer(output)
	if !valid {
		if err := session.RecordInvalid(ctx, job, s.Version, record, refs, output); err != nil {
			return err
		}
		return s.Store.RetryLater(job, "invalid_entity_compare_output")
	}
	a := NamedEntity{Type: pair.Entities[0].Type, Name: pair.Entities[0].Name, MemoryCount: pair.Entities[0].MemoryCount}
	b := NamedEntity{Type: pair.Entities[1].Type, Name: pair.Entities[1].Name, MemoryCount: pair.Entities[1].MemoryCount}
	return session.Apply(ctx, job, s.Version, record, refs, same, func(u EntityUnit) error {
		finish := func() error {
			if err := u.DiscardResult(); err != nil {
				return err
			}
			return u.Acknowledge()
		}
		deferred := func(reason string) error {
			if err := u.Event("deferred", reason, 1); err != nil {
				return err
			}
			return finish()
		}
		unchanged, err := u.NamesUnchanged()
		if err != nil {
			return err
		}
		// A name changed during the call. The pair returns as a new candidate.
		if !unchanged {
			return finish()
		}
		if same {
			current, err := u.EvidenceCurrent()
			if err != nil {
				return err
			}
			if !current {
				return deferred("identity_input_changed")
			}
			locked, err := u.LockEntities()
			if err != nil {
				return err
			}
			if !locked {
				return deferred("identity_name_changed")
			}
			if err := u.Merge(KeepIndex(a, b, keep-1)); err != nil {
				return err
			}
		}
		if err := u.Receipt(&same, output); err != nil {
			return err
		}
		return finish()
	})
}
