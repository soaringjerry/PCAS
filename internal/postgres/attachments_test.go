package postgres

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestAttachmentRetentionIdempotencyAndDeletion(t *testing.T) {
	s := testStore(t)
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	scope := owner()
	ctx := context.Background()
	in := memory.IngestRequest{Connector: "test", ExternalID: "file-1", ExternalVersion: "1", Title: "原图.png", MediaType: "image/png"}
	first, err := s.IngestAttachment(ctx, scope, in, strings.NewReader("test image payload"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.IngestAttachment(ctx, scope, in, strings.NewReader("test image payload"))
	if err != nil || !again.Duplicate || first.ID != again.ID {
		t.Fatal("attachment replay", err)
	}
	if err := s.CleanupBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	file, _, _, err := s.OpenAttachment(ctx, scope, first.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(file)
	file.Close()
	if string(data) != "test image payload" {
		t.Fatal("original changed")
	}
	metadata, err := s.GetSource(ctx, scope, first.ID, 1)
	if err != nil || !metadata.Source.HasAttachment || metadata.Source.Text != "" {
		t.Fatal("wrong attachment metadata", metadata, err)
	}
	derived := mustIngest(t, s, scope, memory.IngestRequest{Connector: "attachment-text", ExternalID: string(first.ID), ExternalVersion: "1", Title: "解析文本", Text: "OCR 内容", MediaType: "text/plain"})
	if _, err := s.pool.Exec(ctx, "UPDATE source_versions SET representation='ocr',derived_from_id=$3,derived_from_version=1 WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(derived.ID), string(first.ID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{first.Ref}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.OpenAttachment(ctx, scope, first.ID, 1); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("deleted original accessible", err)
	}
}
