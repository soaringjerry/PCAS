package postgres

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"io"
)

var _ workspace.StudioAPI = (*Store)(nil)

// Skeleton only: never present an unavailable implementation as successful data.
func (s *Store) ReadProjectHandover(context.Context, memory.Scope, string) (workspace.ProjectHandover, error) {
	return workspace.ProjectHandover{}, memory.ErrUnavailable
}
func (s *Store) ListDocumentVersions(context.Context, memory.Scope, string) (workspace.DocumentVersionList, error) {
	return workspace.DocumentVersionList{}, memory.ErrUnavailable
}
func (s *Store) ReadDocumentVersion(context.Context, memory.Scope, string, int) (workspace.DocumentVersion, error) {
	return workspace.DocumentVersion{}, memory.ErrUnavailable
}
func (s *Store) ReadDocumentDiff(context.Context, memory.Scope, string, int, int) (workspace.DocumentDiff, error) {
	return workspace.DocumentDiff{}, memory.ErrUnavailable
}
func (s *Store) ListProjectFiles(context.Context, memory.Scope, string) (workspace.ProjectFileList, error) {
	return workspace.ProjectFileList{}, memory.ErrUnavailable
}
func (s *Store) UploadProjectFile(context.Context, memory.Scope, string, string, string, io.Reader) (workspace.ProjectFileUpload, error) {
	return workspace.ProjectFileUpload{}, memory.ErrUnavailable
}
func (s *Store) DeleteProjectFile(context.Context, memory.Scope, string, string, bool) error {
	return memory.ErrUnavailable
}
func (s *Store) ReadProjectTimeline(context.Context, memory.Scope, string) (workspace.ProjectTimeline, error) {
	return workspace.ProjectTimeline{}, memory.ErrUnavailable
}
