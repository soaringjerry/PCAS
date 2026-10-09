package modelcall

import (
	"context"
	"errors"

	"github.com/soaringjerry/PCAS/internal/ai"
)

// FailureDetails contains only safe machine metadata required by retry policy.
// Provider messages, URLs, and diagnostic causes are never serialized.
type FailureDetails struct {
	Cancellation   string         `json:"cancellation,omitempty"`
	Codex          *ai.CodexError `json:"codex,omitempty"`
	HTTPStatus     int            `json:"httpStatus,omitempty"`
	OutcomeUnknown bool           `json:"outcomeUnknown,omitempty"`
}

func failureDetails(err error) *FailureDetails {
	out := &FailureDetails{}
	if errors.Is(err, context.Canceled) {
		out.Cancellation = "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		out.Cancellation = "deadline"
	}
	var codex *ai.CodexError
	if errors.As(err, &codex) {
		out.Codex = codex.SafeMetadata()
	}
	var provider *ai.ProviderError
	if errors.As(err, &provider) {
		if provider.HTTPStatus >= 100 && provider.HTTPStatus <= 599 {
			out.HTTPStatus = provider.HTTPStatus
		}
		out.OutcomeUnknown = provider.OutcomeUnknown
	}
	return out
}

func (d *FailureDetails) cause() error {
	if d == nil {
		return nil
	}
	var cancellation error
	switch d.Cancellation {
	case "canceled":
		cancellation = context.Canceled
	case "deadline":
		cancellation = context.DeadlineExceeded
	}
	if d.Codex != nil {
		safe := d.Codex.SafeMetadata()
		safe.Cause = cancellation
		return safe
	}
	if d.HTTPStatus != 0 || d.OutcomeUnknown {
		return &ai.ProviderError{HTTPStatus: d.HTTPStatus, OutcomeUnknown: d.OutcomeUnknown, Cause: cancellation}
	}
	return cancellation
}
