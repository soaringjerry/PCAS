package kernel

import "github.com/soaringjerry/PCAS/internal/memory"

// Internal commands still persist in order for validation and undo collection.
// Publish history and its memory source only after the whole secretary action,
// including reminder changes, has reached its final state.
type SecretaryHistoryKey struct{}

type SecretaryCreationKey struct{}

const MaxAttempts = 5

// MaxJobAttempts is the attempt limit of one queue job.
const MaxJobAttempts = MaxAttempts

type DelegateHistoryKey struct{}

// runNoticePrefix marks a notice that reports how handed-off work ended,
// as opposed to a reminder that came due.
const RunNoticePrefix = "run:"

type CardMemory struct {
	AppliesTo   string     `json:"appliesTo,omitempty"`
	N           int        `json:"n"`
	Text        string     `json:"text"`
	ExpressedAt string     `json:"expressedAt"`
	Category    string     `json:"category"`
	Durable     *bool      `json:"durable,omitempty"`
	Trust       string     `json:"trust"`
	Ref         memory.Ref `json:"-"`
}
