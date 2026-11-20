// Package access decides what a run of an agent may still use: the memories
// and sources its answer was made from, and what of an item an agent sees.
package access

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Claim is a memory that a run used, as it is now for the run's agent.
type Claim struct {
	Version     int
	Nature      string
	Acquisition string
	Scope       map[string]json.RawMessage
	// AI is true when the memory was drawn from what the assistant said.
	AI bool
	// Retired is why the memory left the current ones; KeeperCurrent is true
	// when the memory it was merged into is current.
	Retired       string
	KeeperCurrent bool
}

// Unit reads for one check, in its transaction. Each method reads; none
// decides.
type Unit interface {
	Agent(ctx context.Context, id string) (workspace.Agent, error)
	// Excluded are the memories that the owner took out of the item's context.
	Excluded(ctx context.Context, item string) ([]string, error)
	// Claims are the active memories with their applicable versions that are
	// granted to the agent, by ID.
	Claims(ctx context.Context, agent string, ids []string) (map[string]Claim, error)
	// Sources are the versions of the active sources that the agent sees for
	// the item, by ID.
	Sources(ctx context.Context, agent string, item *string, ids []string) (map[string]int, error)
}

// Verify is nil when the run's agent is enabled and every memory and source
// the run used is still what it was, may still be read by the agent, and fits
// the item it serves: not excluded there, and of no other project. An
// answer made from what the agent may no longer read is a conflict. When an
// old answer is rechecked, a memory merged or superseded since keeps its
// access: that changes whether it is valid, not who may read the old text.
func Verify(ctx context.Context, u Unit, run workspace.Run, item *workspace.Item, recheck bool) error {
	agent, err := u.Agent(ctx, run.AgentID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	project, excluded := "", []string{}
	var thing *string
	if item != nil {
		project = item.ProjectID
		if item.Kind == "project" {
			project = item.ID
		}
		if excluded, err = u.Excluded(ctx, item.ID); err != nil {
			return err
		}
		if item.ID != "" {
			thing = &item.ID
		}
	}
	refs := uniqueRefs(run.ContextVersions)
	claimIDs, sourceIDs := []string{}, []string{}
	for _, ref := range refs {
		if ref.Kind == memory.SourceKind {
			sourceIDs = append(sourceIDs, string(ref.ID))
		} else {
			claimIDs = append(claimIDs, string(ref.ID))
		}
	}
	claims, sources := map[string]Claim{}, map[string]int{}
	if len(claimIDs) > 0 {
		if claims, err = u.Claims(ctx, run.AgentID, claimIDs); err != nil {
			return err
		}
	}
	if len(sourceIDs) > 0 {
		if sources, err = u.Sources(ctx, run.AgentID, thing, sourceIDs); err != nil {
			return err
		}
	}
	for _, ref := range refs {
		id := string(ref.ID)
		if ref.Kind == memory.SourceKind {
			if version, ok := sources[id]; !ok || version != ref.Version {
				return memory.ErrConflict
			}
			continue
		}
		claim, ok := claims[id]
		if !ok || !recheck && !Usable(claim.Retired, claim.KeeperCurrent) {
			return memory.ErrConflict
		}
		if claim.Version != ref.Version || !slices.Contains(agent.MemoryKinds, claim.Nature) || !agent.IncludeInferred && (claim.Acquisition == "inferred" || claim.AI) {
			return memory.ErrConflict
		}
		if item != nil {
			var claimProject string
			if raw, ok := claim.Scope["project_id"]; ok && modelcall.DecodeStrict(raw, &claimProject) != nil {
				return memory.ErrConflict
			}
			if slices.Contains(excluded, id) || claimProject != "" && claimProject != project {
				return memory.ErrConflict
			}
		}
	}
	return nil
}

// Usable is true when a memory may still be a new answer's dependency: it is
// current, or it was merged into a memory that is.
func Usable(retired string, keeperCurrent bool) bool {
	return retired == "" || retired == "duplicate" && keeperCurrent
}

// Withhold takes out of the item what an adopted answer of the kind wrote
// there when the answer is no longer allowed: the notes, the body, the
// progress, the checklist entry, or the title of a task, which then asks to
// be authorized again. It is true when the item holds such writing.
func Withhold(item *workspace.Item, kind, id string, allowed bool) (used bool) {
	switch kind {
	case "notes":
		used = item.Notes != ""
		if !allowed {
			item.Notes = ""
		}
	case "body":
		used = item.Body != ""
		if !allowed {
			item.Body = ""
		}
	case "progress":
		used = item.Progress != ""
		if !allowed {
			item.Progress = ""
		}
	case "check":
		checks := []workspace.Check{}
		for _, check := range item.Checklist {
			if check.ID == id {
				used = true
			}
			if check.ID != id || allowed {
				checks = append(checks, check)
			}
		}
		item.Checklist = checks
	case "task":
		used = true
		if !allowed {
			item.Title = workspace.ReauthorizeTitle
		}
	}
	return used
}

func uniqueRefs(refs []memory.Ref) []memory.Ref {
	seen := make(map[memory.Ref]bool, len(refs))
	out := make([]memory.Ref, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}
