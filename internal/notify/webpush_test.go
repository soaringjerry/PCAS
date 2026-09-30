package notify

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type subscriptionStub struct {
	subs    []webpush.Subscription
	removed []string
}

func (s *subscriptionStub) Subscriptions(context.Context, string) ([]webpush.Subscription, error) {
	return s.subs, nil
}
func (s *subscriptionStub) DeleteSubscription(_ context.Context, _ string, endpoint string) error {
	s.removed = append(s.removed, endpoint)
	return nil
}
func TestWebPushEncryptedRequestAndExpiredSubscription(t *testing.T) {
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y))
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts[r.URL.Path]++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("Authorization") == "" || len(body) != 4096 || bytes.Contains(body, []byte("private title")) {
			t.Error("request was not encrypted and VAPID authenticated")
		}
		if r.URL.Path == "/expired" {
			w.WriteHeader(410)
		} else {
			w.WriteHeader(201)
		}
	}))
	defer server.Close()
	store := &subscriptionStub{}
	for _, path := range []string{"/expired", "/active"} {
		store.subs = append(store.subs, webpush.Subscription{Endpoint: server.URL + path, Keys: webpush.Keys{P256dh: key, Auth: base64.RawURLEncoding.EncodeToString(auth)}})
	}
	push := &WebPush{Settings: Settings{Path: filepath.Join(t.TempDir(), "notify.json")}, Store: store, PublicURL: "https://example.com", Client: server.Client()}
	if err := push.Send(context.Background(), Message{Title: "private title"}); err != nil {
		t.Fatal(err)
	}
	if counts["/expired"] != 1 || counts["/active"] != 1 || len(store.removed) != 1 || store.removed[0] != server.URL+"/expired" {
		t.Fatal("expired target prevented remaining delivery or was not deleted")
	}
	store.subs = store.subs[:1]
	if err := push.Send(context.Background(), Message{Title: "private title"}); !errors.Is(err, ErrUnconfigured) {
		t.Fatal("all expired targets reported as delivered")
	}

}
