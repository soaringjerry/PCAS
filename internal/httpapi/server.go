package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type Server struct {
	sources   memory.Sources
	retriever memory.Retriever
	auth      Authenticator
	ready     func(context.Context) error
	logger    *slog.Logger
	options   Options
}

func New(sources memory.Sources, retriever memory.Retriever, auth Authenticator, ready func(context.Context) error, logger *slog.Logger, options ...Options) http.Handler {
	s := &Server{sources: sources, retriever: retriever, auth: auth, ready: ready, logger: logger}
	if len(options) > 0 {
		s.options = options[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.ready(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /v1/memory/capabilities", s.authorize(s.capabilities))
	mux.HandleFunc("POST /v1/memory/sources", s.authorize(s.ingest))
	mux.HandleFunc("GET /v1/memory/sources/{id}", s.authorize(s.getSource))
	mux.HandleFunc("POST /v1/memory/recall", s.authorize(s.recall))
	mux.HandleFunc("POST /v1/memory/expand", s.authorize(s.expand))
	s.workspaceRoutes(mux)
	return mux
}

type endpoint func(http.ResponseWriter, *http.Request, memory.Scope)

func (s *Server) authorize(next endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		scope, ok := s.auth.Authenticate(r)
		if !ok || !scope.Valid() {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		checkOrigin := sameOrigin
		if sessions, ok := s.auth.(interface{ SameOrigin(*http.Request) bool }); ok {
			checkOrigin = sessions.SameOrigin
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Authorization") == "" && !checkOrigin(r) {
			s.fail(w, memory.ErrForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx), scope)
	}
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	available := func(on bool) string {
		if on {
			return "available"
		}
		return "not_configured"
	}
	extraction, embedding := false, false
	if s.options.Models != nil {
		extraction = s.options.Models.Available(s.options.Models.Config.Extraction)
		embedding = s.options.Models.Available(s.options.Models.Config.Embedding)
	}
	writeJSON(w, http.StatusOK, map[string]any{"architecture": "1.0", "stage": "service", "capabilities": map[string]string{
		"text_ingestion": "available", "source_versions": "available", "source_read": "available", "transactional_queue": "available", "text_chunking": "available",
		"recall": available(s.retriever != nil), "expand": available(s.retriever != nil), "structured_write": available(s.options.Writer != nil), "extraction": available(extraction), "embedding": available(embedding), "tokenization": available(s.retriever != nil), "attachments": available(s.options.Attachments != nil),
		"correction": available(s.options.Editor != nil), "deletion": available(s.options.Editor != nil), "activity": available(s.options.Activity != nil), "wakeups": available(s.options.Workspace != nil), "agent_credentials": "available",
	}})
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	var in memory.IngestRequest
	if !decode(w, r, &in) {
		return
	}
	result, err := s.sources.Ingest(r.Context(), scope, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	version := 0
	if raw := r.URL.Query().Get("version"); raw != "" {
		var err error
		version, err = strconv.Atoi(raw)
		if err != nil || version < 1 {
			s.fail(w, memory.ErrInvalid)
			return
		}
	}
	result, err := s.sources.GetSource(r.Context(), scope, memory.ID(r.PathValue("id")), version)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) recall(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if s.retriever == nil {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	var in memory.RecallRequest
	if !decode(w, r, &in) {
		return
	}
	result, err := s.retriever.Recall(r.Context(), scope, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) expand(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if s.retriever == nil {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	var in memory.ExpandRequest
	if !decode(w, r, &in) {
		return
	}
	result, err := s.retriever.Expand(r.Context(), scope, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "application/json required"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(out)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = memory.ErrInvalid
		}
	}
	if err != nil {
		status := http.StatusBadRequest
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]string{"error": "invalid_json"})
		return false
	}
	return true
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	switch {
	case errors.Is(err, memory.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_input"
	case errors.Is(err, memory.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, memory.ErrConflict):
		status, code = http.StatusConflict, "version_conflict"
	case errors.Is(err, memory.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, memory.ErrBlocked):
		status, code = http.StatusGone, "reimport_blocked"
	case errors.Is(err, memory.ErrUnavailable):
		status, code = http.StatusNotImplemented, "capability_not_configured"
	case errors.Is(err, workspace.ErrBudget):
		status, code = http.StatusPaymentRequired, "daily_budget_exceeded"
	default:
		s.logger.Error("memory request failed") // no source text, token or SQL details
	}
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
