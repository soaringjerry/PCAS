package itemcommand

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// CandidateWrites are the writes that the review of a candidate can need.
type CandidateWrites interface {
	// Remember writes the candidate as a statement of the given nature and
	// returns the identity of the memory.
	Remember(ctx context.Context, text, nature, projectID string, source workspace.SourceRef) (string, error)
	Item(ctx context.Context, id string) (workspace.Item, error)
	// SaveItem saves an item without a history entry.
	SaveItem(ctx context.Context, item workspace.Item) error
}

// ReviewCandidate applies the owner's decision to a candidate: accept it as
// a memory, a task, or an idea; ignore it; restore it; or merge it into an
// item. Only a pending candidate can be accepted, ignored, or merged, and
// only an ignored candidate can be restored. The caller saves the candidate.
func ReviewCandidate(ctx context.Context, v *workspace.Candidate, c workspace.Command, w CandidateWrites) error {
	switch c.Type {
	case "acceptCandidate":
		if v.State != "pending" {
			return memory.ErrConflict
		}
		if err := requireText(c.Text); err != nil {
			return err
		}
		v.Kind = c.Kind
		v.Text = c.Text
		v.ProjectID = c.ProjectID
		v.MemoryKind = c.MemoryKind
		v.Due = c.Due
		if c.Kind == "memory" {
			kind := c.MemoryKind
			if kind == "" {
				kind = "fact"
			}
			id, err := w.Remember(ctx, c.Text, kind, c.ProjectID, v.Source)
			if err != nil {
				return err
			}
			v.ResolvedInto = id
		} else if oneOf(c.Kind, "task", "idea") {
			item := workspace.NewItem(c.Kind, c.Text)
			item.ProjectID = c.ProjectID
			item.Due = c.Due
			item.Sources = append(item.Sources, v.Source)
			if err := w.SaveItem(ctx, item); err != nil {
				return err
			}
			v.ResolvedInto = item.ID
		} else {
			return memory.ErrInvalid
		}
		v.State = "accepted"
	case "ignoreCandidate":
		if v.State != "pending" {
			return memory.ErrConflict
		}
		v.State = "ignored"
	case "restoreCandidate":
		if v.State != "ignored" {
			return memory.ErrConflict
		}
		v.State = "pending"
	case "mergeCandidate":
		if v.State != "pending" {
			return memory.ErrConflict
		}
		item, err := w.Item(ctx, c.TargetID)
		if err != nil {
			return err
		}
		item.Sources = append(item.Sources, v.Source)
		if item.Kind == "task" {
			item.Notes += "\n" + v.Text
		} else {
			item.Body += "\n" + v.Text
		}
		item.Version++
		item.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := w.SaveItem(ctx, item); err != nil {
			return err
		}
		v.State = "merged"
		v.ResolvedInto = item.ID
	default:
		return memory.ErrInvalid
	}
	return nil
}

// bulkLimit is the count of items that one bulk command can change. A
// larger selection is refused, so that the owner sees that nothing was done.
const bulkLimit = 100

// Expand returns the single commands of a bulk command, one for each
// distinct item. candidate gives the candidate that a bulk acceptance takes
// its fields from.
func Expand(c workspace.Command, candidate func(id string) (workspace.Candidate, error)) ([]workspace.Command, error) {
	if len(c.IDs) == 0 || len(c.IDs) > bulkLimit {
		return nil, memory.ErrInvalid
	}
	out := []workspace.Command{}
	seen := map[string]bool{}
	for _, id := range c.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		next := c
		next.ID = id
		next.IDs = nil
		next.Type = map[string]string{"bulkStatus": "setTaskStatus", "bulkDefer": "deferTask", "bulkMove": "moveThing", "bulkAccept": "acceptCandidate", "bulkIgnore": "ignoreCandidate"}[c.Type]
		if c.Type == "bulkAccept" {
			v, err := candidate(id)
			if err != nil {
				return nil, err
			}
			next.Kind = v.Kind
			next.Text = v.Text
			next.MemoryKind = v.MemoryKind
			next.ProjectID = v.ProjectID
			next.Due = v.Due
		}
		out = append(out, next)
	}
	return out, nil
}
