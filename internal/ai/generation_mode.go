package ai

import (
	"encoding/json"
	"errors"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// GenerationMode specifies required capabilities, not fallback preferences.
// Empty mode preserves the existing ordinary generation request.
type GenerationMode struct {
	Search bool
	Schema json.RawMessage
}

var ErrUnsupportedCapability = errors.New("provider_capability_unsupported")

// CapabilityError contains no provider messages or private request contents.
type CapabilityError struct{ Capability string }

func (e *CapabilityError) Error() string { return ErrUnsupportedCapability.Error() }
func (e *CapabilityError) Unwrap() error { return ErrUnsupportedCapability }

// CheckGeneration checks the selected snapshot against implemented adapters.
// Legacy ID-based methods retain their behavior until their callers migrate.
func (r *Registry) CheckGeneration(p Provider, mode GenerationMode) error {
	if len(mode.Schema) > 0 {
		var schema map[string]json.RawMessage
		if json.Unmarshal(mode.Schema, &schema) != nil || schema == nil {
			return memory.ErrInvalid
		}
	}
	if !r.providerAvailable(p) || p.Embedding || p.Transcription {
		return memory.ErrUnavailable
	}
	if len(mode.Schema) > 0 && p.Protocol != "codex" {
		return &CapabilityError{Capability: "output_schema"}
	}
	if mode.Search && p.Protocol != "codex" {
		return &CapabilityError{Capability: "web_search"}
	}
	return nil
}
