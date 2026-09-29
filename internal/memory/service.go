package memory

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Repository guarantees atomic source/version/outbox writes and scoped reads.
type Repository interface{ Sources }

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Ingest(ctx context.Context, scope Scope, in IngestRequest) (IngestResult, error) {
	if !scope.Valid() || !scope.IsOwner {
		return IngestResult{}, ErrForbidden
	}
	if in.MediaType == "" {
		in.MediaType = "text/plain"
	}
	if !utf8.ValidString(in.Text) || strings.ContainsRune(in.Text+in.Title+in.Connector+in.ExternalID+in.ExternalVersion, '\x00') {
		return IngestResult{}, fmt.Errorf("%w: content must be UTF-8 without NUL", ErrInvalid)
	}
	if strings.TrimSpace(in.Connector) == "" || len(in.Connector) > 100 || strings.TrimSpace(in.ExternalID) == "" || len(in.ExternalID) > 1024 || strings.TrimSpace(in.ExternalVersion) == "" || len(in.ExternalVersion) > 256 {
		return IngestResult{}, fmt.Errorf("%w: connector, external_id and external_version are required and bounded", ErrInvalid)
	}
	if strings.TrimSpace(in.Text) == "" || len(in.Text) > 1<<20 || len(in.Title) > 4096 {
		return IngestResult{}, fmt.Errorf("%w: text is required (maximum 1 MiB); title maximum 4096 bytes", ErrInvalid)
	}
	if in.MediaType != "text/plain" && in.MediaType != "text/markdown" {
		return IngestResult{}, fmt.Errorf("%w: this adapter accepts plain text and Markdown; attachment ingestion is not configured", ErrInvalid)
	}
	return s.repo.Ingest(ctx, scope, in)
}

func (s *Service) GetSource(ctx context.Context, scope Scope, id ID, version int) (SourceResult, error) {
	if !scope.Valid() {
		return SourceResult{}, ErrForbidden
	}
	if !id.Valid() || version < 0 {
		return SourceResult{}, ErrInvalid
	}
	return s.repo.GetSource(ctx, scope, id, version)
}
