package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Server) libraryRoutes(mux *http.ServeMux) {
	reader, ok := s.options.Workspace.(workspace.LibraryReader)
	if !ok {
		return
	}
	ownerRead := func(read func(*http.Request, memory.Scope) (any, error)) http.HandlerFunc {
		return s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			out, err := read(r, scope)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, http.StatusOK, out)
		})
	}
	if health, ok := s.options.Workspace.(interface {
		BackgroundHealth(context.Context, memory.Scope) (json.RawMessage, error)
	}); ok {
		mux.HandleFunc("GET /v1/workspace/background", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
			return health.BackgroundHealth(r.Context(), scope)
		}))
	}
	if schedule, ok := s.options.Workspace.(workspace.ScheduleReader); ok {
		mux.HandleFunc("GET /v1/workspace/schedule", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
			return schedule.ListSchedule(r.Context(), scope, workspace.ScheduleQuery{From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to")})
		}))
		mux.HandleFunc("GET /v1/workspace/in-progress", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
			return schedule.ListInProgress(r.Context(), scope)
		}))
	}
	mux.HandleFunc("GET /v1/workspace/handover", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
		return reader.ReadHandover(r.Context(), scope)
	}))
	mux.HandleFunc("GET /v1/workspace/deadlines", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
		q := workspace.DeadlineQuery{}
		if values, present := r.URL.Query()["expired"]; present {
			if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
				return nil, memory.ErrInvalid
			}
			expired := values[0] == "true"
			q.Expired = &expired
		}
		return reader.ListDeadlines(r.Context(), scope, q)
	}))
	mux.HandleFunc("GET /v1/workspace/assistant-requirements", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
		return reader.ListAssistantRequirements(r.Context(), scope)
	}))
	mux.HandleFunc("GET /v1/workspace/memory-groups", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
		return reader.ListMemoryGroups(r.Context(), scope)
	}))
	mux.HandleFunc("GET /v1/workspace/memory-groups/{key}/memories", ownerRead(func(r *http.Request, scope memory.Scope) (any, error) {
		q := workspace.GroupMemoryQuery{Cursor: r.URL.Query().Get("cursor")}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				return nil, memory.ErrInvalid
			}
			q.Limit = n
		}
		return reader.ListGroupMemories(r.Context(), scope, r.PathValue("key"), q)
	}))
}
