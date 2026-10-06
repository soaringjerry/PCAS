package postgres

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var _ workspace.LibraryReader = (*Store)(nil)

// Phase 2.6 skeletons deliberately return empty, non-null collections until B lands.
func (s *Store) ReadHandover(ctx context.Context, scope memory.Scope) (workspace.Handover, error) {
	return workspace.Handover{}, requireOwner(scope)
}

func (s *Store) ListDeadlines(ctx context.Context, scope memory.Scope, q workspace.DeadlineQuery) (workspace.DeadlineList, error) {
	return workspace.DeadlineList{Items: []workspace.LibraryDeadline{}}, requireOwner(scope)
}

func (s *Store) ListAssistantRequirements(ctx context.Context, scope memory.Scope) (workspace.AssistantRequirementList, error) {
	return workspace.AssistantRequirementList{Items: []workspace.AssistantRequirement{}}, requireOwner(scope)
}

func (s *Store) ListGroupMemories(ctx context.Context, scope memory.Scope, key string, q workspace.GroupMemoryQuery) (workspace.MemoryPage, error) {
	out := workspace.MemoryPage{Items: []workspace.Memory{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if key == "" || q.Limit < 0 || q.Limit > 100 {
		return out, memory.ErrInvalid
	}
	return out, nil
}

func (s *Store) ListMemoryGroups(ctx context.Context, scope memory.Scope) (workspace.MemoryGroupList, error) {
	return workspace.MemoryGroupList{Items: []workspace.LibraryMemoryGroup{}}, requireOwner(scope)
}
