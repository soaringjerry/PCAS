package claim

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Original is the source version that a statement quotes.
type Original struct {
	Body        string
	Connector   string
	ExpressedAt *time.Time
	RecordedAt  time.Time
}

// Quoted is a memory already drawn from the same quotation of a source
// version. Editable is true when the assistant wrote it and the owner did not
// confirm it.
type Quoted struct {
	Ref      memory.Ref
	Editable bool
}

// RememberUnit is the storage of one statement, in its transaction. Each
// method reads or writes; none decides.
type RememberUnit interface {
	Original(ctx context.Context, source memory.Ref) (Original, error)
	// Blocked is true when the owner erased a memory with the key.
	Blocked(ctx context.Context, key [32]byte) (bool, error)
	// Redirected is the active memory that a merge left for the key.
	Redirected(ctx context.Context, key [32]byte) (memory.Ref, bool, error)
	// AtQuote is the memory that the source version supports at the
	// quotation, the owner's first, then the latest.
	AtQuote(ctx context.Context, source memory.Ref, quote string) (Quoted, bool, error)
	// Supplement fills the statement's dates, time of saying and mentions
	// where the memory has none, without a new revision.
	Supplement(ctx context.Context, ref memory.Ref, in Statement) error
	// Prior is the memory that an earlier version of the source gave the key.
	Prior(ctx context.Context, source memory.ID, key [32]byte) (memory.Ref, bool, error)
	// Reassert adds the source version as evidence of the memory at the span
	// and indexes the memory again.
	Reassert(ctx context.Context, ref memory.Ref, in Statement, start, end int) error
	Self(ctx context.Context) (memory.ID, error)
	// Entity is the entity of the type and the name, made when there is none.
	Entity(ctx context.Context, kind, name string) (memory.ID, error)
	// SourceEntity is the entity of the name that the source introduced.
	SourceEntity(ctx context.Context, name string, source memory.ID) (memory.ID, bool, error)
	// NewSourceEntity makes an entity of unknown type that the source introduced.
	NewSourceEntity(ctx context.Context, name string, source memory.ID, actor string) (memory.ID, error)
	// Create saves a new memory about the subject, its evidence at the span,
	// its key for the source, its grants to the enabled agents, and when it
	// was last brought up, and indexes it.
	Create(ctx context.Context, in Statement, subject memory.ID, start, end int, key [32]byte) (memory.Ref, error)
	// Confirm keeps the owner's own words as a source of the memory.
	Confirm(ctx context.Context, ref memory.Ref, title, text string) error
}

// Remember writes a statement once. What the owner erased is not written
// again (memory.ErrBlocked). A merged key gives the memory it was merged
// into. A structured statement at a quotation that already gave a memory
// gives that memory, and fills it when the assistant's unconfirmed memory
// lacks the details. A later version of a source that says the same thing
// is new evidence for the same memory. Otherwise the memory is new, about
// the owner, a named entity, or an entity the source introduced; the owner's
// own memory keeps their words as its source. A quotation that the source
// does not hold is invalid.
func Remember(ctx context.Context, u RememberUnit, in Statement) (memory.Ref, error) {
	in, err := Normalize(in)
	if err != nil {
		return memory.Ref{}, err
	}
	original, err := u.Original(ctx, in.Source)
	if err != nil {
		return memory.Ref{}, err
	}
	in.ExpressedAt = ExpressedAt(in.ExpressedAt, original.ExpressedAt, original.RecordedAt, original.Connector)
	start, end, found := QuoteSpan(original.Body, in.Quote)
	if !found {
		return memory.Ref{}, memory.ErrInvalid
	}
	key := Key(in)
	blocked, err := u.Blocked(ctx, key)
	if err != nil {
		return memory.Ref{}, err
	}
	if blocked {
		return memory.Ref{}, memory.ErrBlocked
	}
	if ref, ok, err := u.Redirected(ctx, key); err != nil || ok {
		return ref, err
	}
	if in.Structured && in.Quote != "" && !in.AllowNewAtQuote {
		quoted, ok, err := u.AtQuote(ctx, in.Source, in.Quote)
		if err != nil {
			return memory.Ref{}, err
		}
		if ok {
			if quoted.Editable {
				if err := u.Supplement(ctx, quoted.Ref, in); err != nil {
					return quoted.Ref, err
				}
			}
			return quoted.Ref, nil
		}
	}
	prior, ok, err := u.Prior(ctx, in.Source.ID, key)
	if err != nil {
		return memory.Ref{}, err
	}
	if ok {
		// A repeated assertion in a newer source version is fresh evidence for
		// the same claim, not a second claim and not continued reliance on v1.
		if err := u.Reassert(ctx, prior, in, start, end); err != nil {
			return memory.Ref{}, err
		}
		return prior, nil
	}
	subject, err := subjectOf(ctx, u, in)
	if err != nil {
		return memory.Ref{}, err
	}
	ref, err := u.Create(ctx, in, subject, start, end, key)
	if err != nil {
		return ref, err
	}
	if in.Actor == "user" {
		if err := u.Confirm(ctx, ref, ConfirmedTitle, in.Text); err != nil {
			return ref, err
		}
	}
	return ref, nil
}

// subjectOf is who a new memory is about: the owner or the named entity of
// a structured statement, else the entity of the name that the source
// introduced, made when there is none.
func subjectOf(ctx context.Context, u RememberUnit, in Statement) (memory.ID, error) {
	if in.Structured && in.SubjectType != "" {
		if in.SubjectType == "self" {
			return u.Self(ctx)
		}
		return u.Entity(ctx, in.SubjectType, in.Subject)
	}
	id, ok, err := u.SourceEntity(ctx, in.Subject, in.Source.ID)
	if err != nil || ok {
		return id, err
	}
	return u.NewSourceEntity(ctx, in.Subject, in.Source.ID, in.Actor)
}
