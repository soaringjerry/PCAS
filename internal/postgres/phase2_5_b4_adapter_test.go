package postgres_test

import (
	"context"
	"encoding/json"
	"github.com/soaringjerry/PCAS/internal/memory"
	"testing"
)

type phase25B4Reply struct {
	text                  string
	used                  []memory.ID
	actions               []json.RawMessage
	missingKeyInformation bool
}
type phase25B4Adapter struct {
	encodeAnswer    func(phase25B4Reply) (string, error)
	encodeSelfcheck func(phase25B4Reply) (string, error)
	encodeSelection func([]string) (string, error)
	encodeReader    func([]memory.ID) (string, error)
	secretary       func(context.Context, string) error
	deputy          func(context.Context, string) error
}

// B4 is not merged on the current baseline. Set this at the coordinator's
// merge notification and bind postgres.WithMemoryTier in this one place.
// This is a prerequisite skip, not a passed test or a product finding.
var phase25B4Merged = false
var phase25B4TierContext func(context.Context, string) context.Context

func phase25B4RequireMerged(t *testing.T) {
	t.Helper()
	if !phase25B4Merged {
		t.Skip("awaiting batch 4 merge")
	}
}
func phase25B4DefaultAdapter() phase25B4Adapter {
	encode := func(result phase25B4Reply) (string, error) {
		actions := result.actions
		if actions == nil {
			actions = []json.RawMessage{}
		}
		used := result.used
		if used == nil {
			used = []memory.ID{}
		}
		// The secretary's existing reply/actions shape stays in one adapter.
		// The added contract field is never inferred
		// from the draft text: it is an explicit boolean from the fake model.
		data, err := json.Marshal(map[string]any{"reply": result.text, "used": used, "actions": actions, "missingKeyInfo": result.missingKeyInformation})
		return string(data), err
	}
	return phase25B4Adapter{encodeAnswer: encode, encodeSelfcheck: encode,
		encodeSelection: func(keys []string) (string, error) {
			if keys == nil {
				keys = []string{}
			}
			data, err := json.Marshal(map[string]any{"groups": keys})
			return string(data), err
		},
		encodeReader: func(ids []memory.ID) (string, error) {
			if ids == nil {
				ids = []memory.ID{}
			}
			data, err := json.Marshal(map[string]any{"used": ids})
			return string(data), err
		},
	}
}
func phase25B4ModelOutput(a phase25B4Adapter, result phase25B4Reply, checking bool) (string, error) {
	encode := a.encodeAnswer
	if checking {
		encode = a.encodeSelfcheck
	}
	if encode == nil {
		return "", phase25B234AwaitingProtocol
	}
	return encode(result)
}
