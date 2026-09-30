package httpapi

import (
	"context"
	"net/http"
	"strings"

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
		writeJSON(w, 200, map[string]any{"editable": models.SettingsPath != "", "text": models.ConnectionStatus("text"), "embedding": models.ConnectionStatus("embedding"), "decision": s.decisionStatus()})
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
	// The Jev key for desk routing. Saving runs one routing call so a wrong key
	// or an unexpected API shape shows up here rather than silently on the desk.
	mux.HandleFunc("POST /v1/models/decision", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		var in struct {
			APIKey string `json:"api_key"`
		}
		if !decode(w, r, &in) {
			return
		}
		if strings.TrimSpace(in.APIKey) == "" {
			s.fail(w, memory.ErrInvalid)
			return
		}
		if err := s.options.Models.SaveDecisionKey(in.APIKey); err != nil {
			s.fail(w, err)
			return
		}
		out := s.decisionStatus()
		out["working"] = false
		if s.options.Router != nil {
			_, err := s.options.Router.Route(r.Context(), "帮我写封请假邮件")
			if err != nil {
				s.logger.Warn("desk routing check failed", "error", err.Error())
			}
			out["working"] = err == nil
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("DELETE /v1/models/decision", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		if err := s.options.Models.SaveDecisionKey(""); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, s.decisionStatus())
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

// decisionStatus says whether desk routing has a key and whether it comes from
// Settings (removable here) or the server environment.
func (s *Server) decisionStatus() map[string]any {
	saved := s.options.Models.DecisionKey() != ""
	return map[string]any{"key_configured": s.options.Router.Configured(), "saved": saved}
}
