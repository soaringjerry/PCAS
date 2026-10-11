package deputy

import (
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// CheckRequest checks the request of a run: a known kind and a prompt.
func CheckRequest(kind, prompt string) error {
	switch kind {
	case "plan", "breakdown", "summary", "draft", "ask", "revise":
	default:
		return memory.ErrInvalid
	}
	if strings.TrimSpace(prompt) == "" || len(prompt) > 1<<20 {
		return memory.ErrInvalid
	}
	return nil
}

// OrderMemories returns the memories that a run is given, in their order:
// the structured matches of the recall, the memories that the secretary
// prepared for the delegation, each preference and decision, and the other
// recalled memories. A structured or prepared match needs the same version.
// An explicit long-term constraint stays applicable also when the words of
// the task do not repeat it.
func OrderMemories(all []workspace.Memory, structured, prepared, recalled []memory.Ref) []workspace.Memory {
	byID := map[string]workspace.Memory{}
	for _, m := range all {
		byID[m.ID] = m
	}
	ordered := []workspace.Memory{}
	selected := map[string]bool{}
	add := func(m workspace.Memory) {
		if !selected[m.ID] {
			ordered = append(ordered, m)
			selected[m.ID] = true
		}
	}
	for _, refs := range [][]memory.Ref{structured, prepared} {
		for _, ref := range refs {
			if m, ok := byID[string(ref.ID)]; ok && m.Version == ref.Version {
				add(m)
			}
		}
	}
	for _, m := range all {
		if m.Kind == "preference" || m.Kind == "decision" {
			add(m)
		}
	}
	for _, ref := range recalled {
		if m, ok := byID[string(ref.ID)]; ok {
			add(m)
		}
	}
	return ordered
}

// CanPaste reports whether a run takes a result by hand: it waits for one
// and its context did not change.
func CanPaste(run workspace.Run) error {
	if run.Status != "waiting" || run.StaleContext {
		return memory.ErrConflict
	}
	return nil
}

// Paste finishes a run with a result that was given by hand. The result of
// a revision is checked against its document by the caller; each other
// result needs text.
func Paste(run *workspace.Run, output string, now time.Time) error {
	if err := CanPaste(*run); err != nil {
		return err
	}
	if run.Kind != "revise" && (strings.TrimSpace(output) == "" || len(output) > 1<<20) {
		return memory.ErrConflict
	}
	run.Error = ""
	run.Status = "done"
	run.Output = output
	run.FinishedAt = now.UTC().Format(time.RFC3339Nano)
	return nil
}

// Discard ends a run that the owner does not want. A run that is being
// generated cannot be discarded.
func Discard(run *workspace.Run, now time.Time) error {
	if run.Status == "running" {
		return memory.ErrConflict
	}
	run.Status = "failed"
	run.Error = "已弃用"
	run.FinishedAt = now.UTC().Format(time.RFC3339Nano)
	return nil
}

// RankMemories returns the memories from which the current state of a run
// is selected, in the order of the recall: structured matches, prepared
// memories, and the other recalled memories. Each needs the same version,
// and the agent must be allowed to get it.
func RankMemories(all []workspace.Memory, agent workspace.Agent, structured, prepared, recalled []memory.Ref) []workspace.Memory {
	byID := map[string]workspace.Memory{}
	for _, m := range all {
		byID[m.ID] = m
	}
	ranked := []workspace.Memory{}
	seen := map[string]bool{}
	for _, refs := range [][]memory.Ref{structured, prepared, recalled} {
		for _, ref := range refs {
			if m, ok := byID[string(ref.ID)]; ok && m.Version == ref.Version && !seen[m.ID] && contextbuilder.MemoryAllowed(m, agent) {
				ranked = append(ranked, m)
				seen[m.ID] = true
			}
		}
	}
	return ranked
}
