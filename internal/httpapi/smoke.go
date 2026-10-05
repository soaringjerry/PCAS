package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func (s *Server) smokeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /v1/desk/smoke/{id}", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		// An owner browser session is deliberately insufficient for this API.
		if !scope.IsOwner || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			s.fail(w, memory.ErrForbidden)
			return
		}
		desk, ok := s.options.Workspace.(interface {
			CleanupSmoke(context.Context, memory.Scope, string) error
		})
		if !ok {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 110*time.Second)
		defer cancel()
		if err := desk.CleanupSmoke(ctx, scope, r.PathValue("id")); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"cleaned": true})
	}))
}
