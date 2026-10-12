package postgres

import (
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

// Internal commands still persist in order for validation and undo collection.
// Publish history and its memory source only after the whole secretary action,
// including reminder changes, has reached its final state.
type secretaryHistoryKey struct{}

type secretaryCreationKey struct{}

// These stages keep paid snapshots and retry until success or changed inputs.
func persistentBackgroundStage(stage string) bool {
	_, ok := kernel.HourlyBudgets[kernel.BaseStage(stage)]
	return ok
}

const maxAttempts = 5

// MaxJobAttempts is the attempt limit of one queue job.
const MaxJobAttempts = maxAttempts

type delegateHistoryKey struct{}

// runNoticePrefix marks a notice that reports how handed-off work ended,
// as opposed to a reminder that came due.
const runNoticePrefix = "run:"

type cardMemory struct {
	AppliesTo   string     `json:"appliesTo,omitempty"`
	N           int        `json:"n"`
	Text        string     `json:"text"`
	ExpressedAt string     `json:"expressedAt"`
	Category    string     `json:"category"`
	Durable     *bool      `json:"durable,omitempty"`
	Trust       string     `json:"trust"`
	Ref         memory.Ref `json:"-"`
}

type cardDeadline = organize.Deadline
