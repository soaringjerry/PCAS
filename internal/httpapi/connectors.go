package httpapi

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) connectorRoutes(mux *http.ServeMux) {
	if c := s.options.Continuity; c != nil {
		mux.HandleFunc("POST /v1/memory/summary", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.SummaryRequest
			if !decode(w, r, &in) {
				return
			}
			out, err := c.Summarize(r.Context(), scope, in)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
		mux.HandleFunc("POST /v1/memory/activity", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.ActivitySettings
			if !decode(w, r, &in) {
				return
			}
			if err := c.ConfigureActivity(r.Context(), scope, in); err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, map[string]bool{"saved": true})
		}))
	}
	api := s.options.Connectors
	if api == nil {
		return
	}
	mux.HandleFunc("GET /v1/connectors", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		out, err := api.ListConnections(r.Context(), scope)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/connectors", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		var in connectors.ConfigureRequest
		if !decode(w, r, &in) {
			return
		}
		out, err := api.ConfigureConnection(r.Context(), scope, in)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/connectors/{id}/sync", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		var in struct {
			Version int `json:"expected_version"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := api.SyncConnection(r.Context(), scope, memory.ID(r.PathValue("id")), in.Version); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 202, map[string]string{"status": "queued"})
	}))
	// A connector bearer can only append to its bound connector. It cannot read memory.
	mux.HandleFunc("POST /v1/connectors/{id}/records", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id := memory.ID(r.PathValue("id"))
		scope, err := api.AuthenticateConnection(r.Context(), id, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) { s.importBatch(w, r, scope, id) })(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		s.importBatch(w, r.WithContext(ctx), scope, id)
	})
	mux.HandleFunc("POST /v1/connectors/archive", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
		if r.ParseMultipartForm(1<<20) != nil {
			s.fail(w, memory.ErrInvalid)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("file")
		if err != nil {
			s.fail(w, memory.ErrInvalid)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 20<<20+1))
		if err != nil || len(data) > 20<<20 {
			s.fail(w, memory.ErrInvalid)
			return
		}
		out, err := api.ImportArchive(r.Context(), scope, header.Filename, data)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 202, out)
	}))
}
func (s *Server) importBatch(w http.ResponseWriter, r *http.Request, scope memory.Scope, id memory.ID) {
	var in connectors.Batch
	if !decode(w, r, &in) {
		return
	}
	out, err := s.options.Connectors.ImportBatch(r.Context(), scope, id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}
