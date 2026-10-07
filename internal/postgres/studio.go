package postgres

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var _ workspace.StudioAPI = (*Store)(nil)

func (s *Store) ReadProjectTimeline(context.Context, memory.Scope, string) (workspace.ProjectTimeline, error) {
	return workspace.ProjectTimeline{}, memory.ErrUnavailable
}
