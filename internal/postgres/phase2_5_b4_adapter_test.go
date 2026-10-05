package postgres_test

import (
	"context"
	"encoding/json"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
)

type phase25B4Reply struct {
	text                  string
	used                  []string
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

var phase25B4TierContext = postgres.WithMemoryTier

func phase25B4DefaultAdapter() phase25B4Adapter {
	encode := func(result phase25B4Reply) (string, error) {
		actions := result.actions
		if actions == nil {
			actions = []json.RawMessage{}
		}
		used := result.used
		if used == nil {
			used = []string{}
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

// The coordinator has not supplied a valid deletion action for X4-8.
// Bind that public model format here; the test includes a positive control.
func phase25B4DeleteAction(ref string) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"op": "delete", "ref": ref})
	return data
}

// RunAgents asks for draft body text; secretary and secretary selfcheck use JSON.
func phase25B4DeputyReply(text string) phase25B234ModelReply {
	return phase25B234ModelReply{content: text}
}
