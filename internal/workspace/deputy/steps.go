package deputy

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Reader reads groups of memory for a task.
type Reader interface {
	Read(ctx, persist context.Context, task reader.Task, u contextbuilder.UseContext) ([]workspace.Memory, []memory.Ref, []string)
}

// Steps are the model steps of a run. Conduct calls each step at its place.
type Steps struct {
	// Reader reads memory for a heavy run.
	Reader Reader
	// Calls makes the answer call and the self-check call of a run.
	Calls RunCalls
}

// RunCalls makes the two model calls of a run. Calls implements it with the
// gateway.
type RunCalls interface {
	Answer(ctx context.Context, request modelcall.Request) (*modelcall.PaidResult, error)
	Selfcheck(ctx context.Context, request modelcall.Request) (*modelcall.PaidResult, error)
}
