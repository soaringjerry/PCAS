package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type usageReader interface {
	UsageSummary(context.Context, memory.Scope, string, string) (json.RawMessage, error)
	UsageCalls(context.Context, memory.Scope, int, string) (json.RawMessage, error)
}

func (s *Server) usageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/workspace/usage", s.authorize(s.usageSummary))
	mux.HandleFunc("GET /v1/workspace/usage/calls", s.authorize(s.usageCalls))
}

func (s *Server) usageReader(w http.ResponseWriter, scope memory.Scope) (usageReader, bool) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return nil, false
	}
	reader, ok := s.options.Workspace.(usageReader)
	if !ok {
		s.fail(w, memory.ErrUnavailable)
	}
	return reader, ok
}

func (s *Server) usageSummary(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	reader, ok := s.usageReader(w, scope)
	if !ok {
		return
	}
	out, err := reader.UsageSummary(r.Context(), scope, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) usageCalls(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	reader, ok := s.usageReader(w, scope)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 {
			s.fail(w, memory.ErrInvalid)
			return
		}
	}
	if limit > 100 {
		limit = 100
	}
	out, err := reader.UsageCalls(r.Context(), scope, limit, r.URL.Query().Get("before"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
