package notify

import (
	"errors"
	"strings"
	"time"
)

// Window is how long a notice is sent after it was made. An older notice
// stays in the workspace and is not pushed.
const Window = 24 * time.Hour

// MaxAttempts is the number of sends of one notice on one channel. A target
// that is gone stops the channel at once.
const MaxAttempts = 5

// Delivery is the state of the sends of one notice: the channels it reached
// with their time, and the failed attempts with their next try.
type Delivery struct {
	Sent     map[string]bool      `json:"-"`
	Attempts map[string]int       `json:"attempts"`
	RetryAt  map[string]time.Time `json:"retryAt"`
}

// Due is true when the notice is to be sent on the channel at now: it has
// not reached the channel, has attempts left, and its next try has come.
func (d Delivery) Due(channel string, now time.Time) bool {
	return !d.Sent[channel] && d.Attempts[channel] < MaxAttempts && !now.Before(d.RetryAt[channel])
}

// Failed records a failed send and returns the attempts so far. A send is
// tried again after one minute, then five, then every thirty.
func (d *Delivery) Failed(channel string, err error, now time.Time) int {
	if d.Attempts == nil {
		d.Attempts = map[string]int{}
	}
	if d.RetryAt == nil {
		d.RetryAt = map[string]time.Time{}
	}
	attempts := d.Attempts[channel] + 1
	if errors.Is(err, ErrGone) {
		attempts = MaxAttempts
	}
	delay := time.Minute
	if attempts == 2 {
		delay = 5 * time.Minute
	}
	if attempts >= 3 {
		delay = 30 * time.Minute
	}
	d.Attempts[channel] = attempts
	d.RetryAt[channel] = now.Add(delay)
	return attempts
}

// Notice is what a notice says, read just before it is sent. Result is
// true for the notice of a finished run.
type Notice struct {
	Owner, ID, Thing, Title, Reason string
	Due                             time.Time
	Location                        *time.Location
	Result                          bool
}

// Compose is the message of a notice. A reminder says its time and its
// reason; the notice of a finished run says its result.
func Compose(n Notice, publicURL string) Message {
	m := Message{OwnerID: n.Owner, NoticeID: n.ID, ThingID: n.Thing, Title: n.Title, Body: n.Due.In(n.Location).Format("2006-01-02 15:04 MST") + " · " + n.Reason, URL: strings.TrimRight(publicURL, "/") + "/t/" + n.Thing}
	if n.Result {
		m.Result, m.Body = true, n.Reason
	}
	return m
}
