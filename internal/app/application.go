package app

import (
	"context"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/input/attachment"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall/stagecall"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// Store is the store with the configured models: what the application
// composes its services from. Storage reads the models through its own
// interface; the services call them through the gateway.
type Store struct {
	*postgres.Store
	// Models is nil when no model is configured.
	Models *ai.Registry
}

// Compose gives the store the configured models and returns what the
// application composes its services from.
func Compose(db *postgres.Store, models *ai.Registry) Store {
	if models != nil {
		db.SetModels(models)
	}
	return Store{Store: db, Models: models}
}

// Application is the store with the services that make model calls. A caller
// that needs the secretary or the deputy uses it in place of the store.
// Compose it after the store has its models.
type Application struct {
	Store
	secretary secretary.Service
	deputy    deputy.Steps
	recall    retrieval.Recaller
}

// New composes the application over the store.
func New(db Store) *Application {
	return &Application{Store: db, secretary: Secretary(db), deputy: DeputySteps(db), recall: Recall(db)}
}

// Stages makes the calls of the readers and the self-check.
func Stages(db Store) stagecall.Caller {
	return stagecall.Caller{Calls: Gateway(db), ErrorType: secretary.ErrorType}
}

// Reader reads groups of memory for the secretary and the deputy.
func Reader(db Store) reader.Service {
	return reader.Service{Store: db.ReaderStore(), Calls: Stages(db), ErrorType: secretary.ErrorType}
}

// Recall returns the service that searches memory. It requests the vector
// of a query from the model.
func Recall(db Store) retrieval.Recaller {
	recaller := retrieval.Recaller{Store: db, Calls: Gateway(db)}
	if models := db.Models; models != nil {
		recaller.Providers = models
	}
	return recaller
}

// Attachments parses original files with the configured models.
func Attachments(db Store) attachment.Reader {
	return attachment.Reader{Models: db.Models, Calls: Gateway(db), ErrorType: secretary.ErrorType}
}

// Attachment returns the handler of the job of an uploaded file.
func Attachment(db Store) worker.Handler {
	read := Attachments(db).Read
	return func(ctx context.Context, job worker.Job) error { return db.ProcessAttachmentWith(ctx, job, read) }
}

// Secretary returns the service that answers the owner's sentences.
func Secretary(db Store) secretary.Service {
	return secretary.Service{Store: db.DeskStore(), Calls: Gateway(db), Stages: Stages(db), Reader: Reader(db), Recall: Recall(db).Recall, ReadAttachment: Attachments(db).Read}
}

// DeputySteps are the steps of a deputy run that a domain service makes.
func DeputySteps(db Store) deputy.Steps {
	return deputy.Steps{Reader: Reader(db), Calls: deputy.Calls{Gateway: Gateway(db)}}
}

// DeskTurn conducts one turn of the owner with the secretary.
func (a *Application) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return a.secretary.DeskTurn(ctx, scope, request)
}

// Recall returns the memories and the sources for the request.
func (a *Application) Recall(ctx context.Context, scope memory.Scope, in memory.RecallRequest) (memory.RecallResult, error) {
	return a.recall.Recall(ctx, scope, in)
}

// Execute executes one workspace command.
func (a *Application) Execute(ctx context.Context, scope memory.Scope, in workspace.Command) (workspace.State, error) {
	return a.Store.ExecuteWith(ctx, scope, in, a.recall.Embed)
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
// workspace with the secretary's turn and the commands that search memory.
type Workspace struct {
	*postgres.Notifier
	application *Application
}

// NewWorkspace gives the notifier's workspace the application's secretary.
func NewWorkspace(notifier *postgres.Notifier, application *Application) Workspace {
	return Workspace{Notifier: notifier, application: application}
}

// DeskTurn conducts one turn of the owner with the secretary.
func (w Workspace) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return w.application.DeskTurn(ctx, scope, request)
}

// Execute executes one workspace command.
func (w Workspace) Execute(ctx context.Context, scope memory.Scope, in workspace.Command) (workspace.State, error) {
	return w.application.Execute(ctx, scope, in)
}
