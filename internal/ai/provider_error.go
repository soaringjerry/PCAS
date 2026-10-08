package ai

import "fmt"

// ProviderError distinguishes a received HTTP failure from a lost response.
// Cause is diagnostic only; Error never exposes provider content or a URL.
type ProviderError struct {
	HTTPStatus     int
	OutcomeUnknown bool
	Cause          error
}

func (e *ProviderError) Error() string {
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("model provider HTTP %d", e.HTTPStatus)
	}
	return "model provider response unavailable"
}

func (e *ProviderError) Unwrap() error { return e.Cause }
