// Package memory defines PCAS's shared memory model and application contracts.
// It has no dependency on HTTP, PostgreSQL, AI providers or action/task modules.
package memory

import (
	"encoding/json"
	"time"
)

type Kind string

const (
	SourceKind   Kind = "source"
	ChunkKind    Kind = "chunk"
	EntityKind   Kind = "entity"
	EpisodeKind  Kind = "episode"
	ClaimKind    Kind = "claim"
	RelationKind Kind = "relation"
	SummaryKind  Kind = "summary"
)

// Ref always identifies a particular version, never an unversioned fact.
type Ref struct {
	ID      ID   `json:"id"`
	Version int  `json:"version"`
	Kind    Kind `json:"kind"`
}

// TimeRange permits unknown dates and open intervals; nil is not "now".
type TimeRange struct {
	From      *time.Time `json:"from,omitempty"`
	To        *time.Time `json:"to,omitempty"`
	Precision string     `json:"precision"` // unknown, instant, day, month, year, range
}

type Revision struct {
	Ref
	ValidTime   TimeRange  `json:"valid_time"`
	ExpressedAt *time.Time `json:"expressed_at,omitempty"`
	RecordedAt  time.Time  `json:"recorded_at"`
	State       string     `json:"state"` // active or withdrawn; independent of action status
}

type Source struct {
	Revision
	Connector       string `json:"connector"`
	ExternalID      string `json:"external_id"`
	ExternalVersion string `json:"external_version"`
	Title           string `json:"title"`
	Text            string `json:"text"`
	MediaType       string `json:"media_type"`
}

type Chunk struct {
	Ref
	Source    Ref    `json:"source"`
	Ordinal   int    `json:"ordinal"`
	StartRune int    `json:"start_rune"`
	EndRune   int    `json:"end_rune"`
	Text      string `json:"text"`
}

type Entity struct {
	Revision
	Type           string                     `json:"type"`
	Name           string                     `json:"name"`
	Aliases        []string                   `json:"aliases"`
	Disambiguation map[string]json.RawMessage `json:"disambiguation"`
}

type Episode struct {
	Revision
	Title   string `json:"title"`
	Members []Ref  `json:"members"`
}

type Claim struct {
	Revision
	SubjectID    ID                         `json:"subject_id"`
	Predicate    string                     `json:"predicate"`
	Value        json.RawMessage            `json:"value"`
	Scope        map[string]json.RawMessage `json:"scope"`
	Nature       string                     `json:"nature"`       // fact, preference, intention, plan, decision
	Acquisition  string                     `json:"acquisition"`  // direct, reported, inferred, execution
	Confirmation string                     `json:"confirmation"` // unknown, candidate, adopted, confirmed, disputed
	Evidence     []ID                       `json:"evidence"`
}

type Evidence struct {
	ID          ID                         `json:"id"`
	Source      Ref                        `json:"source"`
	Target      Ref                        `json:"target"`
	Locator     map[string]json.RawMessage `json:"locator"`
	Acquisition string                     `json:"acquisition"`
	Stance      string                     `json:"stance"` // supports or refutes
}

type Relation struct {
	Revision
	From     Ref    `json:"from"`
	To       Ref    `json:"to"`
	Type     string `json:"type"` // belongs_to, depends_on, causes, follows, replaces, corrects
	Evidence []ID   `json:"evidence"`
}

type DerivedView struct {
	Ref
	Text         string `json:"text"`
	Dependencies []Ref  `json:"dependencies"`
}

type Processing struct {
	ID        ID     `json:"id"`
	Stage     string `json:"stage"`
	State     string `json:"state"`
	Attempts  int    `json:"attempts"`
	ErrorCode string `json:"error_code,omitempty"`
}
