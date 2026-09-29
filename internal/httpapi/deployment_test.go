package httpapi

import (
	"github.com/soaringjerry/PCAS/internal/memory"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReverseProxyOriginAndSecureSession(t *testing.T) {
	token := strings.Repeat("test-secret", 4)
	s := NewSessions(NewOwnerToken(token, memory.NewID()), "https://pcas.example")
	for _, test := range []struct {
		origin string
		want   int
	}{{"https://pcas.example", 200}, {"http://pcas.example", 403}, {"https://evil.example", 403}} {
		r := httptest.NewRequest("POST", "http://internal:8090/v1/session", strings.NewReader(`{"token":"`+token+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-Forwarded-Host", "evil.example")
		w := httptest.NewRecorder()
		s.Login(w, r)
		if w.Code != test.want {
			t.Fatal(test.origin, w.Code)
		}
		if w.Code == 200 {
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
				t.Fatal("insecure proxy cookie")
			}
		}
	}
}
