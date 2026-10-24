// Package erasure decides what one erasure of memory removes: the records
// themselves, what was derived from them, and every copy of their text.
package erasure

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// Targets is the most records one request erases.
const Targets = 100

// Request is one erasure. An undo keeps the answers that used the records:
// they are marked outdated instead of removed.
type Request struct {
	Targets        []memory.Ref
	IncludeSources bool
	BlockReimport  bool
	KeepAnswers    bool
}

// Unit is the storage of one erasure, in the owner's transaction. Each
// method reads or writes; none decides.
type Unit interface {
	// Lock locks the record and gives its version; memory.ErrNotFound when
	// there is none.
	Lock(ctx context.Context, id string) (int, error)
	// Sources are the sources that the records were drawn from.
	Sources(ctx context.Context, ids []string) ([]string, error)
	// Closure is the records with every record derived from them: chunks,
	// evidence, derived views, relations, revisions, correction sources,
	// entity disambiguations, archive entries and derived source versions.
	// With answers it also follows the memory used by an adopted answer to
	// the action source of that answer.
	Closure(ctx context.Context, ids []string, answers bool) ([]string, error)
	// WholeEpisodes are the organized episodes whose members are all in ids.
	WholeEpisodes(ctx context.Context, ids []string) ([]string, error)
	// RedactArchives erases the original binary of each archive that holds
	// one of the sources.
	RedactArchives(ctx context.Context, ids []string) error
	// LoneSubjects are the entities that only the erased memories speak of,
	// when the assistant made them or an erased memory is about them; never
	// the owner.
	LoneSubjects(ctx context.Context, ids []string) ([]string, error)
	// KeepAnswers marks the answers that used the records outdated and moves
	// their references to the records' own sources.
	KeepAnswers(ctx context.Context, ids []string) error
	// PurgeArtifacts removes what was adopted from the runs that used the
	// records, and gives ids with the records of those artifacts.
	PurgeArtifacts(ctx context.Context, ids []string) ([]string, error)
	// Block keeps the records, their sources and their claims from being
	// imported again.
	Block(ctx context.Context, ids []string) error
	// ScrubCalls removes the records from the inputs of the model calls.
	ScrubCalls(ctx context.Context, ids []string) error
	// DropAnswers empties the desk turns that used the records or asked for
	// them, each receipt saying only the receipt text, and removes the runs that used them with their documents and
	// training samples, keeping their spending.
	DropAnswers(ctx context.Context, ids []string, receipt string) error
	// Detach removes every row that refers to the records, and the blobs of
	// their source versions.
	Detach(ctx context.Context, ids []string) error
	Items(ctx context.Context) ([]workspace.Item, error)
	SaveItem(ctx context.Context, item workspace.Item) error
	Remove(ctx context.Context, ids []string) error
}

// Erase removes the targets, their sources when asked, everything derived
// from them, an organized episode left with no other member, and an entity
// left with nothing said of it. An item keeps no reference to what was
// erased. A version that is not the current one is a conflict.
func Erase(ctx context.Context, u Unit, in Request, now time.Time) error {
	if len(in.Targets) == 0 || len(in.Targets) > Targets {
		return memory.ErrInvalid
	}
	ids := []string{}
	for _, ref := range in.Targets {
		if !ref.ID.Valid() {
			return memory.ErrInvalid
		}
		version, err := u.Lock(ctx, string(ref.ID))
		if err != nil {
			return err
		}
		if ref.Version > 0 && ref.Version != version {
			return memory.ErrConflict
		}
		ids = append(ids, string(ref.ID))
	}
	if in.IncludeSources {
		sources, err := u.Sources(ctx, ids)
		if err != nil {
			return err
		}
		ids = append(ids, sources...)
	}
	answers := !in.KeepAnswers
	ids, err := u.Closure(ctx, ids, answers)
	if err != nil {
		return err
	}
	// Automatically organized episodes have no independent factual authority.
	episodes, err := u.WholeEpisodes(ctx, ids)
	if err != nil {
		return err
	}
	if len(episodes) > 0 {
		if ids, err = u.Closure(ctx, append(ids, episodes...), answers); err != nil {
			return err
		}
	}
	// An archive may hold several records. Its original goes when one of
	// them goes; its other normalized sources stay.
	if err := u.RedactArchives(ctx, ids); err != nil {
		return err
	}
	subjects, err := u.LoneSubjects(ctx, ids)
	if err != nil {
		return err
	}
	ids = append(ids, subjects...)
	if in.KeepAnswers {
		err = u.KeepAnswers(ctx, ids)
	} else {
		ids, err = u.PurgeArtifacts(ctx, ids)
	}
	if err != nil {
		return err
	}
	if in.BlockReimport {
		if err := u.Block(ctx, ids); err != nil {
			return err
		}
	}
	if err := u.ScrubCalls(ctx, ids); err != nil {
		return err
	}
	if !in.KeepAnswers {
		if err := u.DropAnswers(ctx, ids, secretary.DeletedReply); err != nil {
			return err
		}
	}
	if err := u.Detach(ctx, ids); err != nil {
		return err
	}
	// Source previews are derived copies, so items forget them too.
	items, err := u.Items(ctx)
	if err != nil {
		return err
	}
	erased := map[string]bool{}
	for _, id := range ids {
		erased[id] = true
	}
	for _, item := range items {
		if Forget(&item, erased) {
			item.Version++
			item.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
			if err := u.SaveItem(ctx, item); err != nil {
				return err
			}
		}
	}
	return u.Remove(ctx, ids)
}

// Forget removes from an item every reference to an erased record: its
// creation source and memories, its sources, its revisions, and the source
// that met a condition. A condition without its source is not met; an idea
// that only it woke goes back to the shelf. It is true when the item changed.
func Forget(item *workspace.Item, erased map[string]bool) bool {
	changed := false
	if item.Creation != nil {
		if item.Creation.Source != nil && erased[item.Creation.Source.SourceID] {
			item.Creation.Source = nil
			changed = true
		}
		ids := []string{}
		for _, id := range item.Creation.MemoryIDs {
			if erased[id] {
				changed = true
			} else {
				ids = append(ids, id)
			}
		}
		item.Creation.MemoryIDs = ids
	}
	kept := []workspace.SourceRef{}
	for _, source := range item.Sources {
		if erased[source.SourceID] {
			changed = true
		} else {
			kept = append(kept, source)
		}
	}
	for i := range item.Conditions {
		if item.Conditions[i].MetBy != nil && erased[item.Conditions[i].MetBy.SourceID] {
			item.Conditions[i].MetBy = nil
			item.Conditions[i].Met = false
			item.Conditions[i].MetAt = ""
			if item.Wake != nil && item.Wake.ConditionID == item.Conditions[i].ID {
				item.Wake = nil
				if item.Status == "awakened" {
					item.Status = "shelved"
				}
			}
			changed = true
		}
	}
	evolution := []workspace.Revision{}
	for _, rev := range item.Evolution {
		if rev.SourceID != "" && erased[rev.SourceID] {
			changed = true
		} else {
			evolution = append(evolution, rev)
		}
	}
	item.Evolution = evolution
	if changed {
		item.Sources = kept
	}
	return changed
}
