package httpapi

import (
	"context"
	"errors"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// Phase 3 paths are frozen here; business implementations follow in H/D/S/F/P.
func (s *Server) studioRoutes(mux *http.ServeMux) {
	for _, route := range []string{
		"GET /v1/workspace/projects/{id}/handover",
		"GET /v1/workspace/documents/{id}/versions",
		"GET /v1/workspace/documents/{id}/versions/{version}",
		"GET /v1/workspace/documents/{id}/diff",
		"GET /v1/workspace/items/{id}/files",
		"POST /v1/workspace/items/{id}/files",
		"DELETE /v1/workspace/items/{id}/files/{sourceId}",
		"GET /v1/workspace/projects/{id}/timeline",
	} {
		mux.HandleFunc(route, s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/handover") {
				reader, ok := s.options.Workspace.(interface {
					ReadProjectHandover(context.Context, memory.Scope, string) (workspace.ProjectHandover, error)
				})
				if !ok {
					s.fail(w, memory.ErrUnavailable)
					return
				}
				out, err := reader.ReadProjectHandover(r.Context(), scope, r.PathValue("id"))
				if err != nil {
					s.fail(w, err)
					return
				}
				writeJSON(w, 200, out)
				return
			}

			if r.Method == "GET" && strings.Contains(r.URL.Path, "/documents/") {
				reader, ok := s.options.Workspace.(workspace.StudioAPI)
				if !ok {
					s.fail(w, memory.ErrUnavailable)
					return
				}
				var out any
				var err error
				if strings.HasSuffix(r.URL.Path, "/diff") {
					from, to := 0, 0
					values := r.URL.Query()
					if values.Has("from") || values.Has("to") {
						from, err = strconv.Atoi(values.Get("from"))
						if err != nil || from < 1 {
							s.fail(w, memory.ErrInvalid)
							return
						}
						to, err = strconv.Atoi(values.Get("to"))
						if err != nil || to < 1 {
							s.fail(w, memory.ErrInvalid)
							return
						}
					}
					out, err = reader.ReadDocumentDiff(r.Context(), scope, r.PathValue("id"), from, to)
				} else if value := r.PathValue("version"); value != "" {
					version, e := strconv.Atoi(value)
					if e != nil || version < 1 {
						s.fail(w, memory.ErrInvalid)
						return
					}
					out, err = reader.ReadDocumentVersion(r.Context(), scope, r.PathValue("id"), version)
				} else {
					out, err = reader.ListDocumentVersions(r.Context(), scope, r.PathValue("id"))
				}
				if err != nil {
					s.fail(w, err)
					return
				}
				writeJSON(w, 200, out)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/files") || r.PathValue("sourceId") != "" {
				reader, ok := s.options.Workspace.(workspace.StudioAPI)
				if !ok {
					s.fail(w, memory.ErrUnavailable)
					return
				}
				var out any
				var err error
				status := http.StatusOK
				switch r.Method {
				case "GET":
					out, err = reader.ListProjectFiles(r.Context(), scope, r.PathValue("id"))
				case "POST":
					r.Body = http.MaxBytesReader(w, r.Body, blob.MaxBytes+(1<<20))
					if err = r.ParseMultipartForm(1 << 20); err != nil {
						var tooLarge *http.MaxBytesError
						if errors.As(err, &tooLarge) {
							s.fail(w, workspace.ErrFileTooLarge)
						} else {
							s.fail(w, memory.ErrInvalid)
						}
						return
					}
					defer r.MultipartForm.RemoveAll()
					if len(r.MultipartForm.File["file"]) != 1 {
						writeJSON(w, http.StatusBadRequest, map[string]any{"error": "single_file_required", "message": "一次请求上传一个文件，请逐个上传"})
						return
					}
					f, h, e := r.FormFile("file")
					if e != nil {
						s.fail(w, memory.ErrInvalid)
						return
					}
					defer f.Close()
					if h.Size > blob.MaxBytes {
						s.fail(w, workspace.ErrFileTooLarge)
						return
					}
					media := h.Header.Get("Content-Type")
					if v, _, e := mime.ParseMediaType(media); e == nil {
						media = v
					}
					var uploaded workspace.ProjectFileUpload
					uploaded, err = reader.UploadProjectFile(r.Context(), scope, r.PathValue("id"), h.Filename, media, f)
					out = uploaded
					if !uploaded.AlreadyExists {
						status = http.StatusCreated
					}
				case "DELETE":
					err = reader.DeleteProjectFile(r.Context(), scope, r.PathValue("id"), r.PathValue("sourceId"), r.URL.Query().Get("confirmed") == "true")
					if err == nil {
						w.WriteHeader(http.StatusNoContent)
						return
					}
				}
				if err != nil {
					s.fail(w, err)
					return
				}
				writeJSON(w, status, out)
				return
			}
			s.fail(w, memory.ErrUnavailable)
		}))
	}
}
