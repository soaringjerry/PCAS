package projecthandover

import (
	"testing"
	"time"
)

func TestAProjectHandoverIsWrittenWhenTheOpenProjectsInputChanged(t *testing.T) {
	now := time.Now()
	recent := now.Add(-10 * time.Minute)
	due := Due([]Project{
		{ID: "new", Events: 1, WrittenEvents: -1},
		{ID: "same", Events: 3, WrittenEvents: 3},
		{ID: "done", Done: true, Events: 4, WrittenEvents: 3},
		{ID: "pending", Pending: true, Events: 4, WrittenEvents: 3},
		{ID: "recent", Events: 4, WrittenEvents: 3, WrittenAt: &recent},
	}, now)
	if len(due) != 2 || due[0] != (Queued{Project: "new", Due: now}) || due[1].Project != "recent" || !due[1].Due.Equal(recent.Add(MinimumInterval)) {
		t.Fatalf("due = %+v", due)
	}
}
