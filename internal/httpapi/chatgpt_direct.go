package httpapi

import (
	"net/http"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func (s *Server) directChatGPTRoutes(mux *http.ServeMux) {
	for _, route := range []string{"GET /v1/chatgpt/direct/account", "POST /v1/chatgpt/direct/login", "POST /v1/chatgpt/direct/callback", "POST /v1/chatgpt/direct/select", "POST /v1/chatgpt/direct/logout", "POST /v1/chatgpt/direct/refresh", "GET /v1/chatgpt/direct/models"} {
		mux.HandleFunc(route, s.authorize(s.directChatGPT))
	}
}
func (s *Server) directChatGPT(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	if s.options.Models == nil || s.options.Models.ChatGPT == nil {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	m := s.options.Models.ChatGPT
	var in struct {
		ClientID string `json:"client_id"`
		Consent  bool   `json:"consent"`
		Model    string `json:"model"`
		Resume   bool   `json:"resume"`
		URL      string `json:"url"`
	}
	if r.Method == "POST" && !decode(w, r, &in) {
		return
	}
	switch r.URL.Path {
	case "/v1/chatgpt/direct/account":
		out, err := m.Status(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	case "/v1/chatgpt/direct/login":
		out, err := m.Begin(r.Context(), in.ClientID, in.Consent, true)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	case "/v1/chatgpt/direct/callback":
		// The owner's browser could not reach this host's loopback callback, so
		// the owner pastes the address it ended on.
		if err := m.Complete(r.Context(), in.URL); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"connected": true})
	case "/v1/chatgpt/direct/select":
		if in.Model != "" {
			// Model changes apply only to the active registration, and must come
			// from the catalog returned for that account's bearer credential.
			status, err := m.Status(r.Context())
			if err != nil {
				s.fail(w, err)
				return
			}
			if in.ClientID != status.Active {
				s.fail(w, memory.ErrInvalid)
				return
			}
			models, err := m.Models(r.Context())
			if err != nil {
				s.fail(w, err)
				return
			}
			found := false
			for _, model := range models {
				if model.Slug == in.Model {
					found = true
				}
			}
			if !found {
				s.fail(w, memory.ErrInvalid)
				return
			}
		}
		if err := m.Select(r.Context(), in.ClientID, in.Model, in.Resume); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"selected": true})
	case "/v1/chatgpt/direct/refresh":
		if err := m.Refresh(r.Context()); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"refreshed": true})
	case "/v1/chatgpt/direct/logout":
		confirmed, err := m.Logout(r.Context(), in.ClientID)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"signed_out": true, "remote_revocation_confirmed": confirmed})
	case "/v1/chatgpt/direct/models":
		out, err := m.Models(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"models": out})
	}
}
