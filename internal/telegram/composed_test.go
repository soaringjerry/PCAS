package telegram

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/app"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// composed is the store with the secretary's turn. It composes the secretary
// as internal/app does, at each call, because a test sets the models after it
// gave the store to the poller.
type composed struct{ *postgres.Store }

// app is the store with the provider registry that the test gave it.
func (c composed) app() app.Store {
	models, _ := c.Models.(*ai.Registry)
	return app.Store{Store: c.Store, Models: models}
}

func (c composed) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return app.Secretary(c.app()).DeskTurn(ctx, scope, request)
}

func (c composed) Execute(ctx context.Context, scope memory.Scope, in workspace.Command) (workspace.State, error) {
	return app.New(c.app()).Execute(ctx, scope, in)
}

func (c composed) Recall(ctx context.Context, scope memory.Scope, in memory.RecallRequest) (memory.RecallResult, error) {
	return app.New(c.app()).Recall(ctx, scope, in)
}
