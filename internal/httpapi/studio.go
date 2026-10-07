package httpapi

import (
	"github.com/soaringjerry/PCAS/internal/memory"
	"net/http"
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
			s.fail(w, memory.ErrUnavailable)
		}))
	}
}
