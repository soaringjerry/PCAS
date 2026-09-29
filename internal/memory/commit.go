package memory

import "context"

// CommitRequest writes a graph whose IDs can be shared across applications.
// Every claim, episode and relationship must carry source evidence. Revisions
// of existing claims use Correct; this boundary creates new graph records.
type CommitRequest struct {
	RequestID ID         `json:"request_id"`
	Entities  []Entity   `json:"entities"`
	Episodes  []Episode  `json:"episodes"`
	Claims    []Claim    `json:"claims"`
	Relations []Relation `json:"relations"`
	Evidence  []Evidence `json:"evidence"`
}
type Writer interface {
	Commit(context.Context, Scope, CommitRequest) ([]Ref, error)
}
