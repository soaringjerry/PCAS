package httpapi

import (
	"context"
	"net/http"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func (s *Server) manualRunPackage(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	provider, ok := s.options.Workspace.(interface {
		ManualRunPackage(context.Context, memory.Scope, string) (map[string]any, error)
	})
	if !ok {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	out, err := provider.ManualRunPackage(r.Context(), scope, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
