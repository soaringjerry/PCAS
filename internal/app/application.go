package app

import (
	"context"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall/stagecall"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// Application is the store with the services that make model calls. A caller
// that needs the secretary or the deputy uses it in place of the store.
// Compose it after the store has its models.
type Application struct {
	*postgres.Store
	secretary secretary.Service
	deputy    deputy.Steps
}

// New composes the application over the store.
func New(db *postgres.Store) *Application {
	return &Application{Store: db, secretary: Secretary(db), deputy: DeputySteps(db)}
}

// Stages makes the calls of the readers and the self-check.
func Stages(db *postgres.Store) stagecall.Caller {
	return stagecall.Caller{Calls: db.Gateway(), ErrorType: secretary.ErrorType}
}

// Reader reads groups of memory for the secretary and the deputy.
func Reader(db *postgres.Store) reader.Service {
	return reader.Service{Store: db.ReaderStore(), Calls: Stages(db), ErrorType: secretary.ErrorType}
}

// Secretary returns the service that answers the owner's sentences.
func Secretary(db *postgres.Store) secretary.Service {
	return secretary.Service{Store: db.DeskStore(), Calls: db.Gateway(), Stages: Stages(db), Reader: Reader(db)}
}

// DeputySteps are the steps of a deputy run that a domain service makes.
func DeputySteps(db *postgres.Store) deputy.Steps {
	return deputy.Steps{Reader: Reader(db)}
}

// DeskTurn conducts one turn of the owner with the secretary.
func (a *Application) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return a.secretary.DeskTurn(ctx, scope, request)
}

// RunAgents runs the deputy's queued work until ctx ends.
func (a *Application) RunAgents(ctx context.Context, logger *slog.Logger) error {
	return a.Store.RunAgentJobs(ctx, logger, a.deputy)
}

// RunDeputyOnce makes one pass of the deputy's worker.
func (a *Application) RunDeputyOnce(ctx context.Context) error {
	return a.Store.RunDeputy(ctx, a.deputy)
}

// Workspace is the workspace API of the HTTP server: the notifier's
// workspace with the secretary's turn.
type Workspace struct {
	*postgres.Notifier
	secretary secretary.Service
}

// NewWorkspace gives the notifier's workspace the application's secretary.
func NewWorkspace(notifier *postgres.Notifier, application *Application) Workspace {
	return Workspace{Notifier: notifier, secretary: application.secretary}
}

// DeskTurn conducts one turn of the owner with the secretary.
func (w Workspace) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return w.secretary.DeskTurn(ctx, scope, request)
}
