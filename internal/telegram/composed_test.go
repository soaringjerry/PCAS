package telegram

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/app"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// composed is the store with the secretary's turn. It composes the secretary
// as internal/app does, at each call, because a test sets the models after it
// gave the store to the poller.
type composed struct{ *postgres.Store }

func (c composed) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return app.Secretary(c.Store).DeskTurn(ctx, scope, request)
}

func (c composed) Execute(ctx context.Context, scope memory.Scope, in workspace.Command) (workspace.State, error) {
	return app.New(c.Store).Execute(ctx, scope, in)
}

func (c composed) Recall(ctx context.Context, scope memory.Scope, in memory.RecallRequest) (memory.RecallResult, error) {
	return app.New(c.Store).Recall(ctx, scope, in)
}
