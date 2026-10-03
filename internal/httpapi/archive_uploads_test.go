package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// pieceConnectors records what the archive endpoints hand to the store.
type pieceConnectors struct {
	connectors.API
	previewed, imported []byte
	name, organize      string
}

func (c *pieceConnectors) PreviewArchive(_ context.Context, _ memory.Scope, name string, r io.Reader) (connectors.ArchivePreview, error) {
	c.previewed, _ = io.ReadAll(r)
	c.name = name
	return connectors.ArchivePreview{Name: name, Messages: 1}, nil
}
func (c *pieceConnectors) ImportArchiveReader(ctx context.Context, scope memory.Scope, name string, r io.Reader) (connectors.Result, error) {
	return c.ImportArchiveReaderWithOrganizing(ctx, scope, name, r, "")
}
func (c *pieceConnectors) ImportArchiveReaderWithOrganizing(_ context.Context, _ memory.Scope, name string, r io.Reader, organize string) (connectors.Result, error) {
	c.imported, _ = io.ReadAll(r)
	c.name, c.organize = name, organize
	return connectors.Result{Refs: []memory.Ref{}, Gaps: []string{}}, nil
}
func (c *pieceConnectors) ListImports(context.Context, memory.Scope) ([]connectors.ImportBatch, error) {
	return nil, nil
}
func (c *pieceConnectors) PauseImport(context.Context, memory.Scope, memory.ID) (connectors.ImportBatch, error) {
	return connectors.ImportBatch{}, nil
}
func (c *pieceConnectors) ResumeImport(context.Context, memory.Scope, memory.ID) (connectors.ImportBatch, error) {
	return connectors.ImportBatch{}, nil
}
func (c *pieceConnectors) OrganizeImport(context.Context, memory.Scope, memory.ID) (connectors.ImportBatch, error) {
	return connectors.ImportBatch{}, nil
}

// An archive sent in pieces is read and imported from what the server holds:
// in order, resumable after a lost reply, once, and only by its owner.
func TestArchiveSentInPieces(t *testing.T) {
	store := &pieceConnectors{}
	owner := memory.NewID()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serve := func(isOwner bool) http.Handler {
		return New(&sourceStub{}, nil, scopedAuth{memory.Scope{OwnerID: owner, PrincipalID: "test", IsOwner: isOwner}}, func(context.Context) error { return nil }, logger, Options{Connectors: store})
	}
	do := func(h http.Handler, method, path, media string, body []byte) (int, map[string]any) {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if media != "" {
			r.Header.Set("Content-Type", media)
		}
		r.Header.Set("Authorization", "Bearer test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	h := serve(true)
	data := []byte(strings.Repeat("0123456789", 30))
	open := `{"name":"chatgpt-export.zip","size":300}`
	if code, _ := do(serve(false), "POST", "/v1/connectors/archive/uploads", "application/json", []byte(open)); code != 403 {
		t.Fatalf("non-owner opened an upload: %d", code)
	}
	if code, out := do(h, "POST", "/v1/connectors/archive/uploads", "application/json", []byte(`{"name":"big.zip","size":99999999999}`)); code != 413 || out["error"] != "archive_too_large" {
		t.Fatalf("oversize: %d %v", code, out)
	}
	code, out := do(h, "POST", "/v1/connectors/archive/uploads", "application/json", []byte(open))
	id, _ := out["id"].(string)
	if code != 201 || id == "" || out["pieceBytes"] == nil {
		t.Fatalf("open: %d %v", code, out)
	}
	piece := "/v1/connectors/archive/uploads/" + id
	if code, out = do(h, "PUT", piece+"?offset=0", "application/octet-stream", data[:100]); code != 200 || out["received"] != float64(100) {
		t.Fatalf("first piece: %d %v", code, out)
	}
	// The same piece again, as after a lost reply: nothing is written twice.
	if code, out = do(h, "PUT", piece+"?offset=0", "application/octet-stream", data[:100]); code != 409 || out["received"] != float64(100) {
		t.Fatalf("repeated piece: %d %v", code, out)
	}
	// Not all of it has arrived yet.
	if code, _ = do(h, "POST", "/v1/connectors/archive/preview", "application/json", []byte(`{"upload":"`+id+`"}`)); code != 400 {
		t.Fatalf("preview of an incomplete upload: %d", code)
	}
	if code, _ = do(h, "PUT", piece+"?offset=100", "application/octet-stream", append(append([]byte{}, data[100:]...), 'x')); code != 400 {
		t.Fatalf("piece past the declared size: %d", code)
	}
	if code, out = do(h, "PUT", piece+"?offset=100", "application/octet-stream", data[100:]); code != 200 || out["received"] != float64(300) {
		t.Fatalf("last piece: %d %v", code, out)
	}
	if code, _ = do(serve(false), "POST", "/v1/connectors/archive/preview", "application/json", []byte(`{"upload":"`+id+`"}`)); code != 403 {
		t.Fatalf("non-owner preview: %d", code)
	}
	if code, out = do(h, "POST", "/v1/connectors/archive/preview", "application/json", []byte(`{"upload":"`+id+`"}`)); code != 200 || !bytes.Equal(store.previewed, data) || store.name != "chatgpt-export.zip" {
		t.Fatalf("preview: %d %v %d bytes", code, out, len(store.previewed))
	}
	if code, _ = do(h, "POST", "/v1/connectors/archive", "application/json", []byte(`{"upload":"`+id+`","organize":"sometime"}`)); code != 400 {
		t.Fatalf("bad organize: %d", code)
	}
	if code, out = do(h, "POST", "/v1/connectors/archive", "application/json", []byte(`{"upload":"`+id+`","organize":"later"}`)); code != 202 || !bytes.Equal(store.imported, data) || store.organize != "later" {
		t.Fatalf("import: %d %v", code, out)
	}
	// Once imported the pieces are gone.
	if code, _ = do(h, "POST", "/v1/connectors/archive/preview", "application/json", []byte(`{"upload":"`+id+`"}`)); code != 404 {
		t.Fatalf("pieces kept after import: %d", code)
	}
	if code, _ = do(h, "DELETE", piece, "", nil); code != 200 {
		t.Fatalf("discard: %d", code)
	}
}
