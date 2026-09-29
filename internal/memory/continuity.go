package memory

import "context"

type SourceContext struct {
	Conversation string   `json:"conversation,omitempty"`
	Parent       string   `json:"parent,omitempty"`
	Role         string   `json:"role,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Gaps         []string `json:"gaps"`
}
type SummaryRequest struct {
	ID      ID  `json:"id"`
	Version int `json:"version,omitempty"`
	Tokens  int `json:"tokens,omitempty"`
}
type SummaryResult struct {
	Ref          Ref      `json:"ref"`
	Root         Ref      `json:"root"`
	Text         string   `json:"text"`
	Dependencies []Ref    `json:"dependencies"`
	Coverage     Coverage `json:"coverage"`
	Cached       bool     `json:"cached"`
}
type ActivitySettings struct {
	Ref                Ref     `json:"ref"`
	HalfLifeDays       float64 `json:"half_life_days"`
	ReinforcementLimit float64 `json:"reinforcement_limit"`
	Pinned             bool    `json:"pinned"`
}
type Continuity interface {
	Summarize(context.Context, Scope, SummaryRequest) (SummaryResult, error)
	ConfigureActivity(context.Context, Scope, ActivitySettings) error
}
