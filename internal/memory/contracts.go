package memory

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid input")
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("version conflict")
	ErrForbidden   = errors.New("forbidden")
	ErrBlocked     = errors.New("reimport blocked")
	ErrUnavailable = errors.New("capability not configured")
)

// Scope is bound by trusted authentication, not decoded from request JSON.
// Non-owner principals require explicit grants on memory records.
type Scope struct {
	OwnerID     ID
	PrincipalID string
	IsOwner     bool
}

func (s Scope) Valid() bool { return s.OwnerID.Valid() && s.PrincipalID != "" }

type IngestRequest struct {
	Connector       string     `json:"connector"`
	ExternalID      string     `json:"external_id"`
	ExternalVersion string     `json:"external_version"`
	Title           string     `json:"title"`
	Text            string     `json:"text"`
	MediaType       string     `json:"media_type"`
	ExpressedAt     *time.Time `json:"expressed_at,omitempty"`
}

type IngestResult struct {
	Ref
	Duplicate bool `json:"duplicate"`
}

type SourceResult struct {
	Source     Source       `json:"source"`
	Processing []Processing `json:"processing"`
}

type Sources interface {
	Ingest(context.Context, Scope, IngestRequest) (IngestResult, error)
	GetSource(context.Context, Scope, ID, int) (SourceResult, error)
}

type RecallMode string

const (
	Continue RecallMode = "continue"
	Remember RecallMode = "remember"
	History  RecallMode = "history"
)

type Budget struct {
	Candidates int `json:"candidates"`
	Edges      int `json:"edges"`
	Tokens     int `json:"tokens"`
	Hops       int `json:"hops"`
}

type WorkingContext struct {
	Text    string     `json:"text"`
	Objects []ID       `json:"objects"`
	ValidAt *time.Time `json:"valid_at,omitempty"`
	KnownAt *time.Time `json:"known_at,omitempty"`
}

type RecallRequest struct {
	Query   string         `json:"query"`
	Context WorkingContext `json:"context"`
	Mode    RecallMode     `json:"mode"`
	Budget  Budget         `json:"budget"`
	Cursor  string         `json:"cursor,omitempty"`
}

type Coverage struct {
	Complete       bool     `json:"complete"`
	Gaps           []string `json:"gaps"`
	PendingSources []ID     `json:"pending_sources"`
	NextCursor     string   `json:"next_cursor,omitempty"`
}

type RecallResult struct {
	Summary    string     `json:"summary"`
	Memories   []Ref      `json:"memories"`
	Evidence   []Evidence `json:"evidence"`
	Unresolved []string   `json:"unresolved"`
	Coverage   Coverage   `json:"coverage"`
	FollowUps  []string   `json:"follow_ups"`
}

type ExpandRequest struct {
	Refs     []Ref  `json:"refs"`
	Evidence bool   `json:"evidence"`
	History  bool   `json:"history"`
	Budget   Budget `json:"budget"`
	Cursor   string `json:"cursor,omitempty"`
}

type ExpandResult struct {
	Sources   []Source   `json:"sources"`
	Claims    []Claim    `json:"claims"`
	Relations []Relation `json:"relations"`
	Evidence  []Evidence `json:"evidence"`
	Coverage  Coverage   `json:"coverage"`
}

type Retriever interface {
	Recall(context.Context, Scope, RecallRequest) (RecallResult, error)
	Expand(context.Context, Scope, ExpandRequest) (ExpandResult, error)
}

type CorrectRequest struct {
	Target      Ref    `json:"target"` // expected version; reject concurrent changes
	Replacement Claim  `json:"replacement"`
	Reason      string `json:"reason"`
	Evidence    []ID   `json:"evidence"`
}

type DeleteRequest struct {
	Targets        []Ref `json:"targets"`
	IncludeSources bool  `json:"include_sources"`
	BlockReimport  bool  `json:"block_reimport"`
}

type Editor interface {
	Correct(context.Context, Scope, CorrectRequest) (Ref, error)
	Delete(context.Context, Scope, DeleteRequest) error
}

type UseEvent struct {
	Ref     Ref       `json:"ref"`
	EventID string    `json:"event_id"`
	Kind    string    `json:"kind"` // user_mention, confirmation, adoption; never retrieval/display
	At      time.Time `json:"at"`
}

type Activity interface {
	RecordUse(context.Context, Scope, UseEvent) error
}

// API is the eventual unified boundary used by dialogue, actions, handoffs and
// training export. The scaffold wires Sources; other capabilities stay explicit.
type API interface {
	Sources
	Retriever
	Editor
	Activity
}

// Provider ports keep model choice and attachment storage out of canonical data.
type Extraction struct {
	Entities  []Entity
	Episodes  []Episode
	Claims    []Claim
	Relations []Relation
	Evidence  []Evidence
}
type Extractor interface {
	Extract(context.Context, []Source) (Extraction, error)
}
type Embedding struct {
	Model  string
	Values []float32
}
type Embedder interface {
	Embed(context.Context, []string) ([]Embedding, error)
}
type BlobStore interface {
	Put(context.Context, Scope, io.Reader) (string, error)
	Open(context.Context, Scope, string) (io.ReadCloser, error)
	Delete(context.Context, Scope, string) error
}
