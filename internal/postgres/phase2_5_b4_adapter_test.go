package postgres_test

import (
	"context"
	"encoding/json"

	"github.com/soaringjerry/PCAS/internal/memory"
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
	// Invoke the actual secretary/deputy public boundary; tier selection must
	// be made by the product from the request, not forced by this adapter.
	secretary func(context.Context, string) error
	deputy    func(context.Context, string) error
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

// Pending model-boundary sequences: X4-1..11, including actual fallback
// answers and dependency persistence. Extras: mandatory alias group, section
// order, per-section limits, rule applicability, same-data selfcheck, no new
// actions, usage purposes/tiers/plans, parallel readers, failures and deadline.
// Initial random sequence tests the fallback retriever. Extend it to captured
// prompts and tier transitions once the protocol/entrypoint adapter is supplied.
