package notify

import (
	"errors"
	"testing"
	"time"
)

func TestAFailedSendIsTriedAgainLaterAndStopsAfterFiveAttempts(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var d Delivery
	for attempt, delay := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		if !d.Due("push", now) {
			t.Fatalf("attempt %d is not due", attempt+1)
		}
		d.Failed("push", ErrDelivery, now)
		if d.Due("push", now.Add(delay-time.Second)) || !d.Due("push", now.Add(delay)) {
			t.Fatalf("attempt %d waits other than %v", attempt+1, delay)
		}
		now = now.Add(delay)
	}
	if d.Failed("push", ErrDelivery, now) != MaxAttempts || d.Due("push", now.Add(time.Hour)) {
		t.Fatal("a sixth attempt is due")
	}
}

func TestAGoneTargetStopsAtOnceAndASentChannelIsNotSentAgain(t *testing.T) {
	now := time.Now()
	var d Delivery
	if d.Failed("push", errors.Join(ErrGone), now) != MaxAttempts || d.Due("push", now.Add(time.Hour)) {
		t.Fatal("a gone target is tried again")
	}
	d.Sent = map[string]bool{"telegram": true}
	if d.Due("telegram", now) || !d.Due("mail", now) {
		t.Fatal("the sent channels are confused")
	}
}

func TestAResultNoticeSaysItsResultAndAReminderItsTime(t *testing.T) {
	n := Notice{ID: "n", Thing: "t", Title: "周报", Reason: "该交了", Due: time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC), Location: time.UTC}
	if m := Compose(n, "https://pcas.example/"); m.Body != "2026-03-01 01:00 UTC · 该交了" || m.URL != "https://pcas.example/t/t" || m.Result {
		t.Fatalf("%+v", m)
	}
	n.Result = true
	if m := Compose(n, ""); m.Body != "该交了" || !m.Result {
		t.Fatalf("%+v", m)
	}
}
