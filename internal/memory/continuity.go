package memory

import (
	"context"
	"time"
)

type ConversationRequest struct {
	ID      ID
	Version int
	Before  ID
	After   ID
	Limit   int
}

type ConversationMessage struct {
	Ref
	Text        string     `json:"text"`
	Role        string     `json:"role"`
	ExpressedAt *time.Time `json:"expressed_at,omitempty"`
	RecordedAt  time.Time  `json:"recorded_at"`
	Anchor      bool       `json:"anchor"`
}

type ConversationResult struct {
	Anchor   Ref                   `json:"anchor"`
	Title    string                `json:"title"`
	Messages []ConversationMessage `json:"messages"`
	Before   ID                    `json:"before,omitempty"`
	After    ID                    `json:"after,omitempty"`
	Gaps     []string              `json:"gaps"`
	// Permission proofs for assistant parents outside the displayed window.
	ProofRefs []Ref `json:"-"`
}

// Optional source capability. Reading context never mutates memories or usage.
type ConversationReader interface {
	SourceConversation(context.Context, Scope, ConversationRequest) (ConversationResult, error)
}

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
