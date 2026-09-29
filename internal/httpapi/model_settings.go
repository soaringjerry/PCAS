package httpapi

import (
	"context"
	"net/http"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func (s *Server) modelSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models/openai", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		models := s.options.Models
		if models == nil {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		writeJSON(w, 200, map[string]any{"editable": models.SettingsPath != "", "text": models.ConnectionStatus("text"), "embedding": models.ConnectionStatus("embedding")})
	}))
	mux.HandleFunc("POST /v1/models/openai/{role}", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		var in ai.Connection
		if !decode(w, r, &in) {
			return
		}
		if err := s.options.Models.SaveConnection(r.PathValue("role"), in); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, s.options.Models.ConnectionStatus(r.PathValue("role")))
	}))
	mux.HandleFunc("POST /v1/models/embeddings/rebuild", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		queue, ok := s.options.Workspace.(interface {
			QueueEmbeddingBackfill(context.Context, memory.Scope) (int64, error)
		})
		if !ok {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		count, err := queue.QueueEmbeddingBackfill(r.Context(), scope)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]int64{"queued": count})
	}))
}
