package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type SubscriptionStore interface {
	Subscriptions(context.Context, string) ([]webpush.Subscription, error)
	DeleteSubscription(context.Context, string, string) error
}
type WebPush struct {
	Settings  Settings
	Store     SubscriptionStore
	PublicURL string
	Client    *http.Client
}

func (*WebPush) Name() string { return "webpush" }
func (p *WebPush) Send(ctx context.Context, m Message) error {
	subs, err := p.Store.Subscriptions(ctx, m.OwnerID)
	if err != nil {
		return ErrDelivery
	}
	if len(subs) == 0 {
		return ErrUnconfigured
	}
	keys, err := p.Settings.EnsureVAPID()
	if err != nil {
		return ErrDelivery
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return ErrDelivery
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	var failed error
	sent := false
	for _, sub := range subs {
		response, err := webpush.SendNotificationWithContext(ctx, payload, &sub, &webpush.Options{HTTPClient: client, Subscriber: p.PublicURL, VAPIDPublicKey: keys.VAPIDPublic, VAPIDPrivateKey: keys.VAPIDPrivate, TTL: 86400})
		if err != nil {
			failed = ErrDelivery
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		response.Body.Close()
		switch {
		case response.StatusCode == 404 || response.StatusCode == 410:
			if err := p.Store.DeleteSubscription(ctx, m.OwnerID, sub.Endpoint); err != nil {
				failed = ErrDelivery
			}
		case response.StatusCode < 200 || response.StatusCode >= 300:
			failed = ErrDelivery
		default:
			sent = true
		}
	}
	if failed != nil {
		return failed
	}
	if !sent {
		return ErrUnconfigured
	}
	return nil
}
