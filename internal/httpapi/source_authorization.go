package httpapi

import (
	"net/http"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func (s *Server) sourceAuthorizationAPI() (memory.SourceAuthorizer, bool) {
	api, ok := s.sources.(memory.SourceAuthorizer)
	return api, ok
}

func (s *Server) getSourceAuthorizations(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	api, ok := s.sourceAuthorizationAPI()
	if !ok {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	out, err := api.SourceAuthorizations(r.Context(), scope, memory.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authorizations": out})
}

func bindSourcePath(path string, ref *memory.Ref) error {
	id := memory.ID(path)
	if !id.Valid() || ref.Kind != memory.SourceKind || ref.Version < 1 || ref.ID != "" && ref.ID != id {
		return memory.ErrInvalid
	}
	ref.ID = id
	return nil
}

func (s *Server) setSourceAuthorization(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	api, ok := s.sourceAuthorizationAPI()
	if !ok {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	var in memory.SourceAuthorizationRequest
	if !decode(w, r, &in) {
		return
	}
	if err := bindSourcePath(r.PathValue("id"), &in.Source); err != nil {
		s.fail(w, err)
		return
	}
	if r.Method == http.MethodPost && in.Revoke {
		s.fail(w, memory.ErrInvalid)
		return
	}
	in.Revoke = r.Method == http.MethodDelete
	out, err := api.SetSourceAuthorization(r.Context(), scope, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) sourceScopeAPI() (memory.SourceScopeEditor, bool) {
	api, ok := s.sources.(memory.SourceScopeEditor)
	return api, ok
}

func (s *Server) getSourceScope(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	api, ok := s.sourceScopeAPI()
	if !ok {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	out, err := api.SourceScope(r.Context(), scope, memory.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setSourceScope(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	api, ok := s.sourceScopeAPI()
	if !ok {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	var in memory.SourceScopeRequest
	if !decode(w, r, &in) {
		return
	}
	if err := bindSourcePath(r.PathValue("id"), &in.Source); err != nil {
		s.fail(w, err)
		return
	}
	out, err := api.SetSourceScope(r.Context(), scope, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
