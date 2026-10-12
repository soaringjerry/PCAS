package postgres

import (
	"context"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/input/attachment"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/modelcall/stagecall"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// The methods below exist in tests only. They compose the services over the
// store as internal/app does, at each call, because a test can set the models
// again after it opened the store. The application's own entries have the
// same names.

// Gateway builds the model gateway over the store's records and models.
func (s *Store) Gateway() *modelcall.Gateway {
	var providers modelcall.Providers
	if s.Models != nil {
		providers = s.Models
	}
	records := s.CallRecords()
	gateway := modelcall.New(providers, records, records, records)
	if s.Models != nil {
		gateway.WithDirectCalls(s.Models, s.DirectCallRecords())
	}
	return gateway
}

func (s *Store) stageCalls() stagecall.Caller {
	return stagecall.Caller{Calls: s.Gateway(), ErrorType: secretary.ErrorType}
}

func (s *Store) memoryReader() reader.Service {
	return reader.Service{Store: s.ReaderStore(), Calls: s.stageCalls(), ErrorType: secretary.ErrorType}
}

func (s *Store) deputySteps() deputy.Steps {
	return deputy.Steps{Reader: s.memoryReader(), Calls: deputy.Calls{Gateway: s.Gateway()}}
}

func (s *Store) recaller() retrieval.Recaller {
	recaller := retrieval.Recaller{Store: s, Calls: s.Gateway()}
	if models := s.Models; models != nil {
		recaller.Providers = models
	}
	return recaller
}

// Recall returns the memories and the sources for the request.
func (s *Store) Recall(ctx context.Context, scope memory.Scope, in memory.RecallRequest) (memory.RecallResult, error) {
	return s.recaller().Recall(ctx, scope, in)
}

// Execute executes one workspace command.
func (s *Store) Execute(ctx context.Context, scope memory.Scope, in workspace.Command) (workspace.State, error) {
	return s.ExecuteWith(ctx, scope, in, s.recaller().Embed)
}

// DeskTurn conducts one turn of the owner with the secretary.
func (s *Store) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return secretary.Service{Store: s.DeskStore(), Calls: s.Gateway(), Stages: s.stageCalls(), Reader: s.memoryReader(), Recall: s.Recall, ReadAttachment: s.attachmentReader().Read}.DeskTurn(ctx, scope, request)
}

// RunAgents runs the deputy's queued work until ctx ends.
func (s *Store) RunAgents(ctx context.Context, logger *slog.Logger) error {
	return s.RunAgentJobs(ctx, logger, s.deputySteps())
}

// RunDeputyOnce makes one pass of the deputy's worker.
func (s *Store) RunDeputyOnce(ctx context.Context) error { return s.RunDeputy(ctx, s.deputySteps()) }

func (s *Store) runAgentOnce(ctx context.Context) error { return s.runAgent(ctx, s.deputySteps()) }

func (s *Store) heavyUse(ctx, persist context.Context, scope memory.Scope, agent workspace.Agent, thing *string, text string, u useContext, turn, run, phase string) ([]workspace.Memory, []memory.Ref, []string) {
	return s.memoryReader().Read(ctx, persist, reader.Task{Scope: scope, Agent: agent, Thing: thing, Text: text, Turn: turn, Run: run, Phase: phase}, u)
}

func (s *Store) attachmentReader() attachment.Reader {
	return attachment.Reader{Models: s.Models, Calls: s.Gateway(), ErrorType: secretary.ErrorType}
}

// ProcessAttachment handles the job of one uploaded file.
func (s *Store) ProcessAttachment(ctx context.Context, j worker.Job) error {
	return s.ProcessAttachmentWith(ctx, j, s.attachmentReader().Read)
}

func (s *Store) readAttachment(ctx context.Context, scope memory.Scope, ref memory.Ref, job *worker.Job) (intakestore.ParsedAttachment, error) {
	return s.ReadAttachmentWith(ctx, scope, ref, job, s.attachmentReader().Read)
}
