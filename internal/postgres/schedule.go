package postgres

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) ListSchedule(ctx context.Context, scope memory.Scope, q workspace.ScheduleQuery) (workspace.Schedule, error) {
	if err := requireOwner(scope); err != nil {
		return workspace.Schedule{}, err
	}
	return workspace.Schedule{From: q.From, To: q.To, Days: []workspace.ScheduleDay{}, Unclear: []workspace.ScheduleEntry{}, Overdue: []workspace.ScheduleEntry{}}, nil
}
func (s *Store) ListInProgress(ctx context.Context, scope memory.Scope) (workspace.InProgress, error) {
	if err := requireOwner(scope); err != nil {
		return workspace.InProgress{}, err
	}
	return workspace.InProgress{Items: []workspace.Item{}}, nil
}
