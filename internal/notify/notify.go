// Package notify delivers reminders through independent channels. A future
// mobile channel implements Channel without changing the dispatcher.
package notify

import (
	"context"
	"errors"
)

var ErrGone = errors.New("notification target permanently unavailable")
var ErrUnconfigured = errors.New("notification channel not configured")
var ErrDelivery = errors.New("notification delivery failed")

type Message struct {
	NoticeID string `json:"noticeId"`
	ThingID  string `json:"thingId"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	// Result: this reports how handed-off work ended; Body is the whole report,
	// not a time followed by a reason.
	Result  bool   `json:"result,omitempty"`
	OwnerID string `json:"-"` // isolates device subscriptions for each owner
}
type Channel interface {
	Name() string
	Send(context.Context, Message) error
}
