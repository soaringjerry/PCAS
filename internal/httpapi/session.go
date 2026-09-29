package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type session struct {
	Scope   memory.Scope
	Expires time.Time
}
type Sessions struct {
	Bearer       Authenticator
	mu           sync.Mutex
	values       map[[32]byte]session
	publicOrigin string
}

func NewSessions(bearer Authenticator, publicOrigin ...string) *Sessions {
	s := &Sessions{Bearer: bearer, values: map[[32]byte]session{}}
	if len(publicOrigin) > 0 {
		s.publicOrigin = publicOrigin[0]
	}
	return s
}

// The deployment origin is configured explicitly: forwarded headers are not
// trusted to select the cookie policy or authorize browser writes.
func (s *Sessions) SameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && s.publicOrigin != "" {
		return origin == s.publicOrigin
	}
	return sameOrigin(r)
}
func (s *Sessions) Authenticate(r *http.Request) (memory.Scope, bool) {
	if r.Header.Get("Authorization") != "" {
		return s.Bearer.Authenticate(r)
	}
	cookie, err := r.Cookie("pcas_session")
	if err != nil {
		return memory.Scope{}, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	if !ok || time.Now().After(v.Expires) {
		delete(s.values, key)
		return memory.Scope{}, false
	}
	return v.Scope, true
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host
	}
	return true
}
func (s *Sessions) Login(w http.ResponseWriter, r *http.Request) {
	if !s.SameOrigin(r) {
		writeJSON(w, 403, map[string]string{"error": "forbidden"})
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+in.Token)
	scope, ok := s.Bearer.Authenticate(copy)
	if !ok || !scope.IsOwner {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal_error"})
		return
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	s.mu.Lock()
	for k, v := range s.values {
		if time.Now().After(v.Expires) {
			delete(s.values, k)
		}
	}
	if len(s.values) >= 100 {
		s.mu.Unlock()
		writeJSON(w, 429, map[string]string{"error": "too_many_sessions"})
		return
	}
	s.values[sha256.Sum256([]byte(token))] = session{Scope: scope, Expires: time.Now().Add(7 * 24 * time.Hour)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "pcas_session", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.publicOrigin, "https://") || r.TLS != nil || r.Header.Get("Origin") == "https://"+r.Host, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]bool{"authenticated": true})
}
func (s *Sessions) Logout(w http.ResponseWriter, r *http.Request) {
	if !s.SameOrigin(r) {
		writeJSON(w, 403, map[string]string{"error": "forbidden"})
		return
	}
	if cookie, err := r.Cookie("pcas_session"); err == nil {
		s.mu.Lock()
		delete(s.values, sha256.Sum256([]byte(cookie.Value)))
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "pcas_session", Value: "", Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.publicOrigin, "https://") || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"authenticated": false})
}
