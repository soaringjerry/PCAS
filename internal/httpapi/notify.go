package httpapi

import (
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
)

func (s *Server) notifyRoutes(mux *http.ServeMux) {
	register := func(pattern string, handler func(http.ResponseWriter, *http.Request, memory.Scope, workspace.NotifyAPI)) {
		mux.HandleFunc(pattern, s.authorize(func(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
			if !scope.IsOwner {
				s.fail(w, memory.ErrForbidden)
				return
			}
			api, ok := s.options.Workspace.(workspace.NotifyAPI)
			if !ok {
				s.fail(w, memory.ErrUnavailable)
				return
			}
			handler(w, r, scope, api)
		}))
	}
	register("GET /v1/notify/config", func(w http.ResponseWriter, r *http.Request, scope memory.Scope, api workspace.NotifyAPI) {
		out, err := api.NotifyConfig(r.Context(), scope)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	register("POST /v1/notify/push-subscriptions", func(w http.ResponseWriter, r *http.Request, scope memory.Scope, api workspace.NotifyAPI) {
		var in webpush.Subscription
		if !decode(w, r, &in) {
			return
		}
		if err := api.SavePushSubscription(r.Context(), scope, in); err != nil {
			s.fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	register("DELETE /v1/notify/push-subscriptions", func(w http.ResponseWriter, r *http.Request, scope memory.Scope, api workspace.NotifyAPI) {
		var in struct {
			Endpoint string `json:"endpoint"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := api.RemovePushSubscription(r.Context(), scope, in.Endpoint); err != nil {
			s.fail(w, err)
			return
		}
		w.WriteHeader(204)
	})
	register("PUT /v1/notify/telegram", func(w http.ResponseWriter, r *http.Request, scope memory.Scope, api workspace.NotifyAPI) {
		var in workspace.TelegramConfig
		if !decode(w, r, &in) {
			return
		}
		configured, err := api.SaveTelegram(r.Context(), scope, in)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"configured": configured})
	})
	register("POST /v1/notify/test", func(w http.ResponseWriter, r *http.Request, scope memory.Scope, api workspace.NotifyAPI) {
		sent, err := api.TestNotify(r.Context(), scope)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"sent": sent})
	})
	register("POST /v1/notify/notices/{id}/dismiss", func(w http.ResponseWriter, r *http.Request, scope memory.Scope, api workspace.NotifyAPI) {
		out, err := api.DismissNotice(r.Context(), scope, r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
}
