package postgres_test

import (
	"context"
	"errors"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

var phase25B234AwaitingProtocol = errors.New("awaiting coordinator model protocol and background entrypoints")

// Protocol-neutral scenarios refer to stable claim IDs; only the adapter maps
// them to the model's eventual numbering and wire representation. No guessed
// output schema or background entrypoint is used before coordinator handoff.
type phase25B2Duplicate struct {
	kept   memory.ID
	merged []memory.ID
}
type phase25B2Supersession struct{ old, next memory.ID }
type phase25B2Comparison struct {
	duplicates    []phase25B2Duplicate
	supersessions []phase25B2Supersession
	mergePeople   [][2]memory.ID
}
type phase25B2Adapter struct {
	encode     func(phase25B2Comparison, map[memory.ID]int) (string, error)
	schedule   func(context.Context, time.Time) (int, error)
	process    func(context.Context, worker.Job) error
	setVersion func(int) func()
	// The undo closure must invoke the actual action-log Undo boundary.
	restore func(context.Context, memory.Scope, memory.Ref) (undo func() error, err error)
}

func phase25B2ModelOutput(a phase25B2Adapter, result phase25B2Comparison, numbers map[memory.ID]int) (string, error) {
	if a.encode == nil {
		return "", phase25B234AwaitingProtocol
	}
	return a.encode(result, numbers)
}

// Claim must always obtain the actual queued Job. Adapters must never invent
// worker.Job IDs, lease tokens, stages, priority numbers, or numbering order.
// Pending end-to-end sequences: X2-1..8, X2-10..13, version rescheduling,
// chain normalization, protected manual corrections, input cap, shared-source
// evidence de-duplication, compare/organize exclusion, and provider retry limits.
