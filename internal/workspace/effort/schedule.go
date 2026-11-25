package effort

// Project is a project as the effort scheduler sees it.
type Project struct {
	ID string
	// NeedsEstimate is true when the project has at least one task with a due
	// time, no estimated hours, and no user effort source.
	NeedsEstimate bool
	// Pending is true while an effort estimation job of the project waits or
	// runs.
	Pending bool
}

// Due returns the IDs of projects that need an effort estimation job, in their
// existing order.
func Due(projects []Project) []string {
	out := []string{}
	for _, p := range projects {
		if p.NeedsEstimate && !p.Pending {
			out = append(out, p.ID)
		}
	}
	return out
}
