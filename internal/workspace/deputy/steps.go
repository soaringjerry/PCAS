package deputy

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Reader reads groups of memory for a task.
type Reader interface {
	Read(ctx, persist context.Context, task reader.Task, u contextbuilder.UseContext) ([]workspace.Memory, []memory.Ref, []string)
}

// Steps are the steps of a run that a domain service makes. The order of a
// run is still in storage; storage calls each step at its place.
type Steps struct {
	// Reader reads memory for a heavy run.
	Reader Reader
}
