package postgres

import (
	"context"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall/stagecall"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// The methods below exist in tests only. They compose the services over the
// store as internal/app does, at each call, because a test can set the models
// again after it opened the store. The application's own entries have the
// same names.

func (s *Store) stageCalls() stagecall.Caller {
	return stagecall.Caller{Calls: s.Gateway(), ErrorType: secretary.ErrorType}
}

func (s *Store) memoryReader() reader.Service {
	return reader.Service{Store: s.ReaderStore(), Calls: s.stageCalls(), ErrorType: secretary.ErrorType}
}

func (s *Store) deputySteps() deputy.Steps { return deputy.Steps{Reader: s.memoryReader()} }

// DeskTurn conducts one turn of the owner with the secretary.
func (s *Store) DeskTurn(ctx context.Context, scope memory.Scope, request workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error) {
	return secretary.Service{Store: s.DeskStore(), Calls: s.Gateway(), Stages: s.stageCalls(), Reader: s.memoryReader()}.DeskTurn(ctx, scope, request)
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
