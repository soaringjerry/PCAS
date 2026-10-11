package deputy

import (
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const (
	// subtaskLimit is the count of subtasks that one adoption makes.
	subtaskLimit = 100
	// subtaskLength is the longest subtask, in bytes.
	subtaskLength = 2000
)

// CheckAdoption checks that the result of a run can be adopted in the given
// way. Only a finished run whose context is still current is adopted, and
// only once. A revision is adopted as a document.
func CheckAdoption(run workspace.Run, as, text string) error {
	if run.Status != "done" || run.Adopted != nil || run.StaleContext {
		return memory.ErrConflict
	}
	if strings.TrimSpace(text) == "" || len(text) > 1<<20 {
		return memory.ErrInvalid
	}
	switch as {
	case "doc", "subtasks", "progress":
	default:
		return memory.ErrInvalid
	}
	if run.Kind == "revise" && as != "doc" {
		return memory.ErrInvalid
	}
	return nil
}

// Subtasks returns the subtasks that a text becomes. A text gives one
// subtask at least and one hundred at most.
func Subtasks(text string) ([]string, error) {
	out := []string{}
	for _, line := range AdoptionLines(text) {
		if line == "" {
			continue
		}
		if len(line) > subtaskLength || len(out) == subtaskLimit {
			return nil, memory.ErrInvalid
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, memory.ErrInvalid
	}
	return out, nil
}
