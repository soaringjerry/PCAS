package httpapi

import (
	"context"
	"errors"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s *Server) connectorRoutes(mux *http.ServeMux) {
	if c := s.options.Continuity; c != nil {
		mux.HandleFunc("POST /v1/memory/summary", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.SummaryRequest
			if !decode(w, r, &in) {
				return
			}
			out, err := c.Summarize(r.Context(), scope, in)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
		mux.HandleFunc("POST /v1/memory/activity", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.ActivitySettings
			if !decode(w, r, &in) {
				return
			}
			if err := c.ConfigureActivity(r.Context(), scope, in); err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, map[string]bool{"saved": true})
		}))
	}
	api := s.options.Connectors
	if api == nil {
		return
	}
	mux.HandleFunc("GET /v1/connectors", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		out, err := api.ListConnections(r.Context(), scope)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/connectors", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		var in connectors.ConfigureRequest
		if !decode(w, r, &in) {
			return
		}
		out, err := api.ConfigureConnection(r.Context(), scope, in)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/connectors/{id}/sync", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		var in struct {
			Version int `json:"expected_version"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := api.SyncConnection(r.Context(), scope, memory.ID(r.PathValue("id")), in.Version); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 202, map[string]string{"status": "queued"})
	}))
	// A connector bearer can only append to its bound connector. It cannot read memory.
	mux.HandleFunc("POST /v1/connectors/{id}/records", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id := memory.ID(r.PathValue("id"))
		scope, err := api.AuthenticateConnection(r.Context(), id, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) { s.importBatch(w, r, scope, id) })(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		s.importBatch(w, r.WithContext(ctx), scope, id)
	})
	s.archiveRoutes(mux)

}
func (s *Server) importBatch(w http.ResponseWriter, r *http.Request, scope memory.Scope, id memory.ID) {
	var in connectors.Batch
	if !decode(w, r, &in) {
		return
	}
	out, err := s.options.Connectors.ImportBatch(r.Context(), scope, id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// archiveAPI keeps the original connector API compatible with existing adapters.
type archiveAPI interface {
	PreviewArchive(context.Context, memory.Scope, string, io.Reader) (connectors.ArchivePreview, error)
	ImportArchiveReader(context.Context, memory.Scope, string, io.Reader) (connectors.Result, error)
	ListImports(context.Context, memory.Scope) ([]connectors.ImportBatch, error)
	PauseImport(context.Context, memory.Scope, memory.ID) (connectors.ImportBatch, error)
	ResumeImport(context.Context, memory.Scope, memory.ID) (connectors.ImportBatch, error)
}

func (s *Server) archiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/connectors/archive/preview", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) { s.archiveUpload(w, r, scope, true) }))
	mux.HandleFunc("POST /v1/connectors/archive", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) { s.archiveUpload(w, r, scope, false) }))
	mux.HandleFunc("GET /v1/connectors/imports", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		api, ok := s.options.Connectors.(archiveAPI)
		if !ok {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		out, err := api.ListImports(r.Context(), scope)
		if err != nil {
			s.archiveFail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out})
	}))
	for _, operation := range []string{"pause", "resume"} {
		mux.HandleFunc("POST /v1/connectors/imports/{id}/"+operation, s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			api, ok := s.options.Connectors.(archiveAPI)
			if !ok {
				s.fail(w, memory.ErrUnavailable)
				return
			}
			id := memory.ID(r.PathValue("id"))
			var out connectors.ImportBatch
			var err error
			if operation == "pause" {
				out, err = api.PauseImport(r.Context(), scope, id)
			} else {
				out, err = api.ResumeImport(r.Context(), scope, id)
			}
			if err != nil {
				s.archiveFail(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, out)
		}))
	}
}
func (s *Server) archiveUpload(w http.ResponseWriter, r *http.Request, scope memory.Scope, preview bool) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	// Streaming a large export can outlive the common 25-second read deadline.
	// Authentication has already completed; bounded work keeps the upload and
	// its validation together, as with the durable desk-turn endpoint.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
	// Multipart overhead is separate from the file size checked by OpenArchive.
	r.Body = http.MaxBytesReader(w, r.Body, connectors.MaxUploadBytes+(1<<20))
	err := r.ParseMultipartForm(1 << 20)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			s.archiveFail(w, r, &connectors.ArchiveError{Code: "archive_too_large"})
		} else {
			s.fail(w, memory.ErrInvalid)
		}
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.fail(w, memory.ErrInvalid)
		return
	}
	defer file.Close()
	if header.Size > connectors.MaxUploadBytes {
		s.archiveFail(w, r, &connectors.ArchiveError{Code: "archive_too_large"})
		return
	}
	if api, ok := s.options.Connectors.(archiveAPI); ok {
		if preview {
			out, err := api.PreviewArchive(r.Context(), scope, header.Filename, file)
			if err != nil {
				s.archiveFail(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, out)
		} else {
			out, err := api.ImportArchiveReader(r.Context(), scope, header.Filename, file)
			if err != nil {
				s.archiveFail(w, r, err)
				return
			}
			writeJSON(w, http.StatusAccepted, out)
		}
		return
	}
	if preview {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	// Compatibility for connector implementations exposing the original port.
	data, err := io.ReadAll(io.LimitReader(file, connectors.MaxUploadBytes+1))
	if err != nil {
		s.archiveFail(w, r, err)
		return
	}
	if int64(len(data)) > connectors.MaxUploadBytes {
		s.archiveFail(w, r, &connectors.ArchiveError{Code: "archive_too_large"})
		return
	}
	out, err := s.options.Connectors.ImportArchive(r.Context(), scope, header.Filename, data)
	if err != nil {
		s.archiveFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}
func (s *Server) archiveFail(w http.ResponseWriter, r *http.Request, err error) {
	var failure *connectors.ArchiveError
	if !errors.As(err, &failure) {
		s.fail(w, err)
		return
	}
	status := http.StatusBadRequest
	if failure.Code == "archive_too_large" {
		status = http.StatusRequestEntityTooLarge
	}
	if failure.Code == "import_not_active" {
		status = http.StatusConflict
	}
	slog.WarnContext(r.Context(), "archive request failed", "stage", "archive_request", "error_type", failure.Code)
	writeJSON(w, status, map[string]string{"error": failure.Code, "message": failure.Message()})
}
