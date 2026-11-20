package projecthandover

import "time"

// Project is a project as the scheduler sees it.
type Project struct {
	ID   string
	Done bool
	// Events is the count of the project's input events; WrittenEvents is the
	// count its handover was written from, -1 when there is none.
	Events        int
	WrittenEvents int
	WrittenAt     *time.Time
	// Pending is true while a handover job of the project waits or runs.
	Pending bool
}

// Queued is a project handover to write, and when.
type Queued struct {
	Project string
	Due     time.Time
}

// Due are the handovers to write, in the projects' order: of each open
// project whose input changed since its handover and that has none being
// written, an hour after the last at the earliest.
func Due(projects []Project, now time.Time) []Queued {
	out := []Queued{}
	for _, p := range projects {
		if p.Done || p.Pending || p.WrittenEvents == p.Events {
			continue
		}
		due := now
		if p.WrittenAt != nil && p.WrittenAt.Add(MinimumInterval).After(due) {
			due = p.WrittenAt.Add(MinimumInterval)
		}
		out = append(out, Queued{Project: p.ID, Due: due})
	}
	return out
}
