package secretary

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Unit is one turn in its transaction. Storage admitted the request and
// placed the turn in its conversation.
type Unit struct {
	Session Session
	Ask     AskStore
	// Request is the context of the caller's request. A model call ends
	// with it.
	Request context.Context
	TurnID  string
	// Thing is the open item of the checked request, or nil.
	Thing *string
	// Conducting has no limits yet. The service sets them.
	Conducting Conducting
}

// TurnStore conducts turns in storage.
type TurnStore interface {
	// Turn checks and admits the request, reads its attachments, and places
	// the turn in its conversation. It calls conduct once in the transaction
	// of the turn, and saves what conduct returns as the turn. A repeated
	// request returns its saved turn without a call of conduct.
	Turn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest, conduct func(ctx context.Context, unit Unit) (Conducted, error)) (workspace.DeskTurnResponse, error)
}

// Service answers the owner's sentences.
type Service struct {
	Store  TurnStore
	Calls  *modelcall.Gateway
	Stages StageCalls
	Reader Reader
}

// DeskTurn conducts one turn of the owner with the secretary.
func (s Service) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return s.Store.Turn(ctx, scope, request, func(ctx context.Context, unit Unit) (Conducted, error) {
		asker := Asker{Store: unit.Ask, Calls: s.Calls, Stages: s.Stages, Reader: s.Reader, Request: unit.Request, Scope: scope, Thing: unit.Thing, TurnID: unit.TurnID}
		in := unit.Conducting
		in.Limits = Limits{Model: ModelTimeout, HeavyReader: reader.Budget, Total: TurnTimeout}
		return Conduct(ctx, unit.Session, asker, in)
	})
}
