package httpapi

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type Options struct {
	Continuity  memory.Continuity
	Connectors  connectors.API
	Attachments memory.Attachments
	Writer      memory.Writer
	Workspace   workspace.API
	Editor      memory.Editor
	Activity    memory.Activity
	Models      *ai.Registry
	Router      *ai.Router
	WebDir      string
}

func (s *Server) workspaceRoutes(mux *http.ServeMux) {
	if reader, ok := s.options.Workspace.(interface {
		About(context.Context, memory.Scope, string) (workspace.About, error)
	}); ok {
		mux.HandleFunc("GET /v1/workspace/about", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			out, err := reader.About(r.Context(), scope, r.URL.Query().Get("key"))
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
	}
	if groups, ok := s.options.Workspace.(interface {
		SourceGroupItems(context.Context, memory.Scope, string, string, string, int) (workspace.SourceItems, error)
	}); ok {
		mux.HandleFunc("GET /v1/workspace/source-groups/{key}/items", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			out, err := groups.SourceGroupItems(r.Context(), scope, r.PathValue("key"), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), limit)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
	}
	if reader, ok := s.options.Workspace.(interface {
		ListMemories(context.Context, memory.Scope, workspace.MemoryQuery) (workspace.MemoryPage, error)
		GetMemory(context.Context, memory.Scope, string) (workspace.Memory, error)
		MemoryFacets(context.Context, memory.Scope) (workspace.MemoryFacets, error)
	}); ok {
		mux.HandleFunc("GET /v1/workspace/memories", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			values := r.URL.Query()
			q := workspace.MemoryQuery{Q: values.Get("q"), Entity: values.Get("entity"), Nature: values.Get("nature"), Group: values.Get("group"), Category: values.Get("category"), From: values.Get("from"), To: values.Get("to"), Project: values.Get("project"), Epistemic: values.Get("epistemic"), Agent: values.Get("agent"), Cursor: values.Get("cursor")}
			q.Retired = values.Get("retired") == "1"
			if raw := values.Get("limit"); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 1 {
					s.fail(w, memory.ErrInvalid)
					return
				}
				q.Limit = n
			}
			out, err := reader.ListMemories(r.Context(), scope, q)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
		mux.HandleFunc("GET /v1/workspace/memories/{id}", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			out, err := reader.GetMemory(r.Context(), scope, r.PathValue("id"))
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
		mux.HandleFunc("GET /v1/workspace/memory-facets", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			out, err := reader.MemoryFacets(r.Context(), scope)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
	}

	mux.HandleFunc("GET /v1/workspace/items/{id}/retained-writing", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		reader, ok := s.options.Workspace.(interface {
			RetainedWriting(context.Context, memory.Scope, string) ([]map[string]string, error)
		})
		if !ok {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		out, err := reader.RetainedWriting(r.Context(), scope, r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	if s.options.Attachments != nil {
		mux.HandleFunc("POST /v1/memory/attachments", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
			if err := r.ParseMultipartForm(1 << 20); err != nil {
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
			media := header.Header.Get("Content-Type")
			if v, _, err := mime.ParseMediaType(media); err == nil {
				media = v
			}
			externalID := r.FormValue("external_id")
			if externalID == "" {
				externalID = string(memory.NewID())
			}
			out, err := s.options.Attachments.IngestAttachment(r.Context(), scope, memory.IngestRequest{Connector: "file-import", ExternalID: externalID, ExternalVersion: "1", Title: header.Filename, MediaType: media}, file)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 201, out)
		}))
		mux.HandleFunc("GET /v1/memory/sources/{id}/attachment", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			id := memory.ID(r.PathValue("id"))
			if !id.Valid() {
				s.fail(w, memory.ErrInvalid)
				return
			}
			version := 0
			if raw := r.URL.Query().Get("version"); raw != "" {
				var err error
				version, err = strconv.Atoi(raw)
				if err != nil || version < 1 {
					s.fail(w, memory.ErrInvalid)
					return
				}
			}
			file, name, media, err := s.options.Attachments.OpenAttachment(r.Context(), scope, id, version)
			if err != nil {
				s.fail(w, err)
				return
			}
			defer file.Close()
			w.Header().Set("Content-Type", media)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
			_, _ = io.Copy(w, file)
		}))
	}
	if s.options.Writer != nil {
		mux.HandleFunc("POST /v1/memory/commit", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.CommitRequest
			if !decode(w, r, &in) {
				return
			}
			out, err := s.options.Writer.Commit(r.Context(), scope, in)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 201, map[string]any{"refs": out})
		}))
	}

	if sessions, ok := s.auth.(*Sessions); ok {
		mux.HandleFunc("POST /v1/session", sessions.Login)
		mux.HandleFunc("DELETE /v1/session", sessions.Logout)
	}
	if s.options.Workspace != nil {
		mux.HandleFunc("GET /v1/workspace", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			ctx := r.Context()
			if zone := r.Header.Get("X-PCAS-Timezone"); zone != "" {
				var err error
				ctx, err = workspace.WithInitialTimezone(ctx, zone)
				if err != nil {
					s.fail(w, err)
					return
				}
			}
			out, err := s.options.Workspace.Snapshot(ctx, scope)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
		mux.HandleFunc("POST /v1/workspace/commands", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in workspace.Command
			if !decode(w, r, &in) {
				return
			}
			out, err := s.options.Workspace.Execute(r.Context(), scope, in)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
		mux.HandleFunc("GET /v1/workspace/export", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			training := r.URL.Query().Get("training") == "true"
			data, err := s.options.Workspace.Export(r.Context(), scope, training, r.URL.Query().Get("confirmedOnly") == "true")
			if err != nil {
				s.fail(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			name := "pcas-export.json"
			if training {
				name = "pcas-training.jsonl"
				w.Header().Set("Content-Type", "application/x-ndjson")
			}
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			_, _ = w.Write(data)
		}))
	}
	if s.options.Editor != nil {
		mux.HandleFunc("POST /v1/memory/correct", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.CorrectRequest
			if !decode(w, r, &in) {
				return
			}
			out, err := s.options.Editor.Correct(r.Context(), scope, in)
			if err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
		mux.HandleFunc("POST /v1/memory/delete", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.DeleteRequest
			if !decode(w, r, &in) {
				return
			}
			if err := s.options.Editor.Delete(r.Context(), scope, in); err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, map[string]bool{"deleted": true})
		}))
	}
	if s.options.Activity != nil {
		mux.HandleFunc("POST /v1/memory/use", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			var in memory.UseEvent
			if !decode(w, r, &in) {
				return
			}
			if err := s.options.Activity.RecordUse(r.Context(), scope, in); err != nil {
				s.fail(w, err)
				return
			}
			writeJSON(w, 200, map[string]bool{"recorded": true})
		}))
	}
	mux.HandleFunc("GET /v1/models", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		out := []map[string]any{}
		if s.options.Models != nil {
			for _, p := range s.options.Models.Providers() {
				out = append(out, map[string]any{"id": p.ID, "name": p.Name, "protocol": p.Protocol, "model": p.Model, "available": s.options.Models.Available(p.ID), "embedding": p.Embedding, "inputPrice": p.InputPerMillion, "outputPrice": p.OutputPerMillion, "maxOutput": p.MaxOutput})
			}
		}
		writeJSON(w, 200, map[string]any{"providers": out, "chatgptEnabled": s.options.Models != nil && s.options.Models.Codex != nil, "chatgptDirectEnabled": s.options.Models != nil && s.options.Models.ChatGPT != nil})
	}))
	// The desk asks where an entry should go. 501 means no decision model is
	// configured and the page falls back to its own rule.
	mux.HandleFunc("POST /v1/desk/route", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		if !scope.IsOwner {
			s.fail(w, memory.ErrForbidden)
			return
		}
		var in struct {
			Text string `json:"text"`
		}
		if !decode(w, r, &in) {
			return
		}
		text := strings.TrimSpace(in.Text)
		if text == "" || len(text) > 8000 {
			s.fail(w, memory.ErrInvalid)
			return
		}
		if !s.options.Router.Configured() {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		out, err := s.options.Router.Route(r.Context(), text)
		if err != nil {
			s.logger.Warn("desk routing failed", "error", err.Error())
			s.fail(w, memory.ErrUnavailable)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/desk/turn", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		desk, ok := s.options.Workspace.(interface {
			DeskTurn(context.Context, memory.Scope, workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error)
		})
		if !ok {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		var in workspace.DeskTurnRequest
		if !decode(w, r, &in) {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 110*time.Second)
		defer cancel()
		out, err := desk.DeskTurn(ctx, scope, in)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /v1/desk/turns", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		desk, ok := s.options.Workspace.(interface {
			DeskTurns(context.Context, memory.Scope, string) (workspace.DeskTurnsResponse, error)
		})
		if !ok {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		out, err := desk.DeskTurns(r.Context(), scope, r.URL.Query().Get("conversationId"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	// Answering can take a model call, longer than the server's default write timeout.
	mux.HandleFunc("POST /v1/desk/answer", s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
		desk, ok := s.options.Workspace.(interface {
			AnswerDesk(context.Context, memory.Scope, string, string, []workspace.DeskTurn) (workspace.DeskAnswer, error)
		})
		if !ok {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		var in struct {
			Question string               `json:"question"`
			AgentID  string               `json:"agentId"`
			History  []workspace.DeskTurn `json:"history"`
		}
		if !decode(w, r, &in) {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
		out, err := desk.AnswerDesk(r.Context(), scope, in.AgentID, in.Question, in.History)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	for _, route := range []string{"GET /v1/chatgpt/account", "POST /v1/chatgpt/login", "POST /v1/chatgpt/logout", "GET /v1/chatgpt/limits", "GET /v1/chatgpt/models"} {
		mux.HandleFunc(route, s.authorize(s.chatgpt))
	}
	s.directChatGPTRoutes(mux)
	s.modelSettingsRoutes(mux)
	if s.options.WebDir != "" {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v1/") {
				http.NotFound(w, r)
				return
			}
			rel := filepath.Clean("/" + r.URL.Path)
			path := filepath.Join(s.options.WebDir, rel)
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				path = filepath.Join(s.options.WebDir, "index.html")
			}
			// The page names its scripts by content, so a fresh page is what picks
			// up a new release; it must be checked with the server every time.
			if filepath.Base(path) == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "same-origin")
			http.ServeFile(w, r, path)
		})
	}
}
func (s *Server) chatgpt(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	if !scope.IsOwner {
		s.fail(w, memory.ErrForbidden)
		return
	}
	if s.options.Models == nil || s.options.Models.Codex == nil {
		s.fail(w, memory.ErrUnavailable)
		return
	}
	c := s.options.Models.Codex
	switch r.URL.Path {
	case "/v1/chatgpt/account":
		result, err := c.Account(r.Context())
		if err != nil {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		writeJSON(w, 200, result)
	case "/v1/chatgpt/login":
		result, err := c.Login(r.Context())
		if err != nil {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		writeJSON(w, 200, result)
	case "/v1/chatgpt/logout":
		if err := c.Logout(r.Context()); err != nil {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		writeJSON(w, 200, map[string]bool{"signedOut": true})
	case "/v1/chatgpt/limits":
		result, err := c.Limits(r.Context())
		if err != nil {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		writeJSON(w, 200, result)
	case "/v1/chatgpt/models":
		result, err := c.Models(r.Context())
		if err != nil {
			s.fail(w, memory.ErrUnavailable)
			return
		}
		writeJSON(w, 200, result)
	}
}
