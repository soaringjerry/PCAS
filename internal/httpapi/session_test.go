package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestSessionLifecycleAndOrigin(t *testing.T) {
	a := NewSessions(NewOwnerToken(strings.Repeat("test-secret", 4), memory.NewID()))
	request := httptest.NewRequest("POST", "http://pcas.local/v1/session", strings.NewReader(`{"token":"`+strings.Repeat("test-secret", 4)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	a.Login(w, request)
	if w.Code != 403 {
		t.Fatal("cross-origin login accepted")
	}
	request = httptest.NewRequest("POST", "http://pcas.local/v1/session", strings.NewReader(`{"token":"`+strings.Repeat("test-secret", 4)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	a.Login(w, request)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	read := httptest.NewRequest("GET", "http://pcas.local/v1/workspace", nil)
	read.AddCookie(cookies[0])
	if scope, ok := a.Authenticate(read); !ok || !scope.IsOwner {
		t.Fatal("session rejected")
	}
	logout := httptest.NewRequest("DELETE", "http://pcas.local/v1/session", nil)
	logout.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	a.Logout(w, logout)
	if _, ok := a.Authenticate(read); ok {
		t.Fatal("logout did not revoke session")
	}
}
