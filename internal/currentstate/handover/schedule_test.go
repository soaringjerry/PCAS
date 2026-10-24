package handover

import (
	"context"
	"testing"
	"time"
)

type scheduleStore struct {
	written Written
	members bool
	due     []time.Time
}

func (s *scheduleStore) Written(context.Context) (Written, error) { return s.written, nil }
func (s *scheduleStore) HasMembers(context.Context) (bool, error) { return s.members, nil }
func (s *scheduleStore) Queue(_ context.Context, due time.Time) error {
	s.due = append(s.due, due)
	return nil
}

func TestAHandoverIsWrittenAgainOnlyWhenItsInputChanged(t *testing.T) {
	now := time.Now()
	old, current := "old", "new"
	recent, long := now.Add(-10*time.Minute), now.Add(-2*time.Hour)
	for name, c := range map[string]struct {
		written Written
		members bool
		due     *time.Time
	}{
		"first, with members":    {Written{Hash: current}, true, &now},
		"first, nothing to say":  {Written{Hash: current}, false, nil},
		"unchanged":              {Written{Hash: current, InputHash: &current, BuiltAt: &long}, true, nil},
		"being written":          {Written{Hash: current, InputHash: &old, Pending: true}, true, nil},
		"changed long after":     {Written{Hash: current, InputHash: &old, BuiltAt: &long}, false, &now},
		"changed soon after one": {Written{Hash: current, InputHash: &old, BuiltAt: &recent}, true, ptr(recent.Add(MinimumInterval))},
	} {
		s := &scheduleStore{written: c.written, members: c.members}
		queued, err := Schedule(context.Background(), s, now)
		if err != nil || queued != (c.due != nil) || c.due != nil && !s.due[0].Equal(*c.due) {
			t.Errorf("%s: queued = %v, due = %v, err = %v", name, queued, s.due, err)
		}
	}
}

func ptr(t time.Time) *time.Time { return &t }
