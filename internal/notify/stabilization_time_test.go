package notify

import (
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type stabilizationTimeSubscriptions struct {
	owner   string
	subs    []webpush.Subscription
	deleted []string
}

func (s *stabilizationTimeSubscriptions) Subscriptions(_ context.Context, owner string) ([]webpush.Subscription, error) {
	if owner != s.owner {
		return nil, ErrUnconfigured
	}
	return append([]webpush.Subscription(nil), s.subs...), nil
}
func (s *stabilizationTimeSubscriptions) DeleteSubscription(_ context.Context, owner, endpoint string) error {
	if owner != s.owner {
		return ErrUnconfigured
	}
	s.deleted = append(s.deleted, endpoint)
	for i, sub := range s.subs {
		if sub.Endpoint == endpoint {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			break
		}
	}
	return nil
}

// R11 transport coverage complements the PostgreSQL dispatcher test. Both
// responses are local HTTP fixtures, not evidence of a real device delivery.
func TestR11_ExpiredPushDoesNotBlockHealthySubscription(t *testing.T) {
	var expired, healthy atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/expired":
			expired.Add(1)
			w.WriteHeader(http.StatusGone)
		case "/healthy":
			healthy.Add(1)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected push endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	keys := webpush.Keys{P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)), Auth: base64.RawURLEncoding.EncodeToString(auth)}
	store := &stabilizationTimeSubscriptions{owner: "synthetic-T2-owner", subs: []webpush.Subscription{{Endpoint: server.URL + "/expired", Keys: keys}, {Endpoint: server.URL + "/healthy", Keys: keys}}}
	push := &WebPush{Settings: Settings{Path: filepath.Join(t.TempDir(), "notify.json")}, Store: store, PublicURL: "https://example.invalid", Client: server.Client()}
	message := Message{OwnerID: store.owner, Title: "合成 T2 提醒", ThingID: "synthetic-task"}
	if err := push.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if expired.Load() != 1 || healthy.Load() != 1 || len(store.deleted) != 1 || store.deleted[0] != server.URL+"/expired" || len(store.subs) != 1 || store.subs[0].Endpoint != server.URL+"/healthy" {
		t.Fatalf("410 handling: expired=%d healthy=%d deleted=%v remaining=%+v", expired.Load(), healthy.Load(), store.deleted, store.subs)
	}
	if err := push.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if expired.Load() != 1 || healthy.Load() != 2 || len(store.deleted) != 1 {
		t.Fatal("expired target survived removal or healthy target lost")
	}
}
