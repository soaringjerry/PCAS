package topicproject

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

type scheduleStore struct {
	inputs   map[string]Input
	answered map[string]bool
	queued   map[string]bool
	order    []string
	overflow map[string]int
}

func (s *scheduleStore) Topics(context.Context) ([]string, error) {
	return []string{"old", "gone", "answered", "new", "again"}, nil
}
func (s *scheduleStore) Input(_ context.Context, topic string) (Input, bool, error) {
	in, ok := s.inputs[topic]
	return in, ok, nil
}
func (s *scheduleStore) Answered(_ context.Context, topic, _ string) (bool, error) {
	return s.answered[topic], nil
}
func (s *scheduleStore) Queue(_ context.Context, topic string, _ Input, order int) (bool, error) {
	s.order = append(s.order, fmt.Sprint(order, topic))
	return !s.queued[topic], nil
}
func (s *scheduleStore) Overflow(_ context.Context, reason string, count int) error {
	s.overflow[reason] += count
	return nil
}

// "again" is queued already: it keeps its place and counts nothing.
func TestTheTopicSpokenOfLastIsJudgedFirstAndOnlyOncePerEvidence(t *testing.T) {
	now := time.Now()
	s := &scheduleStore{
		inputs: map[string]Input{
			"old":      {Latest: now.Add(-48 * time.Hour), Count: EvidenceLimit + 3, ClippedCharacters: 40},
			"answered": {Latest: now},
			"new":      {Latest: now.Add(-time.Hour)},
			"again":    {Latest: now.Add(-30 * time.Minute), Count: EvidenceLimit + 9},
		},
		answered: map[string]bool{"answered": true},
		queued:   map[string]bool{"again": true},
		overflow: map[string]int{},
	}
	n, err := Schedule(context.Background(), s)
	if err != nil || n != 2 || !slices.Equal(s.order, []string{"0again", "1new", "2old"}) {
		t.Fatalf("n = %d, order = %v, err = %v", n, s.order, err)
	}
	if s.overflow["topic_project_evidence_left_in_group"] != 3 || s.overflow["topic_project_characters_left_in_group"] != 40 {
		t.Fatalf("overflow = %v", s.overflow)
	}
}
