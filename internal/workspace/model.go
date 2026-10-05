// Package workspace owns action state and presents memory-backed working views.
// It never becomes a second canonical store for facts or memory revisions.
package workspace

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/soaringjerry/PCAS/internal/memory"
)

var ErrNewerAction = errors.New("newer action")
var ErrChangedSince = errors.New("changed since action")
var ErrWorkStarted = errors.New("work started")
var ErrAlreadyUndone = errors.New("already undone")
var ErrExpired = errors.New("action expired")

var ErrBudget = errors.New("daily budget exceeded")

type SourceRef struct {
	SourceID string `json:"sourceId"`
	Version  int    `json:"version,omitempty"`
	Label    string `json:"label"`
	Excerpt  string `json:"excerpt,omitempty"`
	At       string `json:"at"`
}
type Revision struct {
	SourceID string `json:"sourceId,omitempty"`
	At       string `json:"at"`
	By       string `json:"by"`
	Summary  string `json:"summary"`
}
type Check struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}
type Trigger struct {
	Offset      string `json:"offset,omitempty"`
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Guard       string `json:"guard,omitempty"`
	NextAt      string `json:"nextAt,omitempty"`
	Active      bool   `json:"active"`
}
type Condition struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Description string     `json:"description"`
	DueAt       string     `json:"dueAt,omitempty"`
	Met         bool       `json:"met"`
	MetAt       string     `json:"metAt,omitempty"`
	MetBy       *SourceRef `json:"metBy,omitempty"`
}
type Wake struct {
	At           string `json:"at"`
	Reason       string `json:"reason"`
	ConditionID  string `json:"conditionId,omitempty"`
	SnoozedUntil string `json:"snoozedUntil,omitempty"`
}
type Owed struct {
	Who   string `json:"who"`
	Since string `json:"since"`
}

// Item is action-module data. Kind-specific input validation precedes writes;
// frequently queried fields have explicit SQL columns and constraints.
type Item struct {
	HasRetainedWriting bool   `json:"hasRetainedWriting,omitempty"`
	ID                 string `json:"id"`
	Kind               string `json:"itemKind"`
	Version            int    `json:"recordVersion"`
	Title              string `json:"title"`
	Name               string `json:"name"`
	Status             string `json:"status"`
	Notes              string `json:"notes,omitempty"`
	Body               string `json:"body"`
	Goal               string `json:"goal"`
	Progress           string `json:"progress"`
	ProjectID          string `json:"projectId,omitempty"`
	IdeaID             string `json:"ideaId,omitempty"`
	Due                string `json:"due,omitempty"`
	Scheduled          string `json:"scheduled,omitempty"`
	WaitingFor         string `json:"waitingFor,omitempty"`
	OwedTo             *Owed  `json:"owedTo,omitempty"`
	// Urgent marks a to-do the user said cannot wait; while it has no time it
	// leads the home timeline.
	Urgent        bool        `json:"urgent,omitempty"`
	DependsOn     []string    `json:"dependsOn"`
	Checklist     []Check     `json:"checklist"`
	Triggers      []Trigger   `json:"triggers"`
	Sources       []SourceRef `json:"sources"`
	History       []Revision  `json:"history"`
	Evolution     []Revision  `json:"evolution"`
	Conditions    []Condition `json:"conditions"`
	RemindersOn   bool        `json:"remindersOn"`
	ShelvedReason string      `json:"shelvedReason,omitempty"`
	Wake          *Wake       `json:"wake,omitempty"`
	NextSteps     []string    `json:"nextSteps"`
	CreatedAt     string      `json:"createdAt"`
	UpdatedAt     string      `json:"updatedAt"`
}
type Candidate struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Text         string    `json:"text"`
	MemoryKind   string    `json:"memoryKind,omitempty"`
	ProjectID    string    `json:"projectId,omitempty"`
	Due          string    `json:"due,omitempty"`
	Confidence   float64   `json:"confidence"`
	Source       SourceRef `json:"source"`
	State        string    `json:"state"`
	ResolvedInto string    `json:"resolvedInto,omitempty"`
	CreatedAt    string    `json:"createdAt"`
}
type MemoryVersion struct {
	At     string `json:"at"`
	By     string `json:"by"`
	Text   string `json:"text"`
	Reason string `json:"reason,omitempty"`
}
type MemoryMention struct {
	EntityID string `json:"entityId"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}
type MemoryGroup struct {
	EntityID string `json:"entityId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
}
type Memory struct {
	Trust              string          `json:"trust"`
	Retired            string          `json:"retired,omitempty"`
	RetiredBy          string          `json:"retiredBy,omitempty"`
	MergedFrom         int             `json:"mergedFrom,omitempty"`
	Category           string          `json:"category"`
	Durable            *bool           `json:"durable,omitempty"`
	Groups             []MemoryGroup   `json:"groups"`
	ContextDependent   bool            `json:"contextDependent,omitempty"`
	ExpressedAt        string          `json:"expressedAt,omitempty"`
	EventFrom          string          `json:"eventFrom,omitempty"`
	EventTo            string          `json:"eventTo,omitempty"`
	EventPrecision     string          `json:"eventPrecision,omitempty"`
	Mentions           []MemoryMention `json:"mentions"`
	HalfLifeDays       float64         `json:"halfLifeDays"`
	ReinforcementLimit float64         `json:"reinforcementLimit"`
	ID                 string          `json:"id"`
	Version            int             `json:"recordVersion"`
	Kind               string          `json:"kind"`
	Text               string          `json:"text"`
	Epistemic          string          `json:"epistemic"`
	Confirmation       string          `json:"confirmation"`
	Acquisition        string          `json:"acquisition"`
	ProjectID          string          `json:"projectId,omitempty"`
	Sources            []SourceRef     `json:"sources"`
	Versions           []MemoryVersion `json:"versions"`
	VisibleTo          []string        `json:"visibleTo"`
	Exposure           float64         `json:"exposure"`
	LastUsedAt         string          `json:"lastUsedAt"`
	Pinned             bool            `json:"pinned"`
}

// MarshalJSON keeps absent mentions and groups empty, including for older values.
func (m Memory) MarshalJSON() ([]byte, error) {
	type wire Memory
	if m.Mentions == nil {
		m.Mentions = []MemoryMention{}
	}
	if m.Groups == nil {
		m.Groups = []MemoryGroup{}
	}
	if m.Category == "" {
		m.Category = "unknown"
	}
	if m.Trust == "" {
		m.Trust = "stated"
	}
	m.ContextDependent = memory.ContextDependent(m.Text)
	return json.Marshal(wire(m))
}

type Agent struct {
	MemoryInitialized bool     `json:"memoryInitialized,omitempty"`
	Default           bool     `json:"default,omitempty"`
	Protocol          string   `json:"protocol,omitempty"`
	Available         bool     `json:"available"`
	InputPrice        float64  `json:"inputPrice"`
	OutputPrice       float64  `json:"outputPrice"`
	MaxOutput         int      `json:"maxOutput"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Channel           string   `json:"channel"`
	Note              string   `json:"note"`
	Enabled           bool     `json:"enabled"`
	MemoryKinds       []string `json:"memoryKinds"`
	IncludeInferred   bool     `json:"includeInferred"`
}
type Settings struct {
	AutoAccept    bool    `json:"autoAccept"`
	WakeIdeas     bool    `json:"wakeIdeas"`
	FollowUps     bool    `json:"followUps"`
	DailyReviewAt string  `json:"dailyReviewAt"`
	DailyBudget   float64 `json:"dailyBudget"`
	Timezone      string  `json:"timezone"`
	// City is where the owner usually is, for weather and "nearby" questions.
	City string `json:"city,omitempty"`
}
type Doc struct {
	ID        string `json:"id"`
	ThingID   string `json:"thingId"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	By        string `json:"by"`
	RunID     string `json:"runId,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}
type Adoption struct {
	ActionID string `json:"actionId,omitempty"`
	Auto     bool   `json:"auto,omitempty"`
	As       string `json:"as"`
	At       string `json:"at"`
	Edited   bool   `json:"edited"`
}
type Run struct {
	SmokeID          string       `json:"smokeId,omitempty"`
	Outdated         bool         `json:"outdated,omitempty"`
	ID               string       `json:"id"`
	ThingID          string       `json:"thingId"`
	AgentID          string       `json:"agentId"`
	Kind             string       `json:"kind"`
	Prompt           string       `json:"prompt"`
	Brief            string       `json:"brief"`
	ContextMemoryIDs []string     `json:"contextMemoryIds"`
	ContextVersions  []memory.Ref `json:"contextVersions"`
	Status           string       `json:"status"`
	Output           string       `json:"output,omitempty"`
	// Searches are the web searches the agent made for this result.
	Searches      []string        `json:"searches,omitempty"`
	Error         string          `json:"error,omitempty"`
	ProviderError json.RawMessage `json:"providerError,omitempty"`
	Adopted       *Adoption       `json:"adopted,omitempty"`
	StaleContext  bool            `json:"staleContext"`
	Cost          float64         `json:"cost"`
	CreatedAt     string          `json:"createdAt"`
	FinishedAt    string          `json:"finishedAt,omitempty"`
}
type Origin struct {
	Label    string `json:"label"`
	RunID    string `json:"runId,omitempty"`
	MemoryID string `json:"memoryId,omitempty"`
}
type Sample struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Prompt    string `json:"prompt"`
	Response  string `json:"response"`
	Origin    Origin `json:"origin"`
	Version   int    `json:"version"`
	State     string `json:"state"`
	Epistemic string `json:"epistemic"`
	Stale     bool   `json:"stale"`
	CreatedAt string `json:"createdAt"`
}

// Source is one entry in the library's list of where material came from. A
// single entry is one stored original, opened by ID; any other entry stands
// for many originals, read through SourceItems with ID as the key.
type Source struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Single     bool   `json:"single"`
	Status     string `json:"status"`
	Note       string `json:"note"`
	ItemCount  int    `json:"itemCount"`
	LastSyncAt string `json:"lastSyncAt,omitempty"`
}
type SourceItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
	Role    string `json:"role,omitempty"`
	At      string `json:"at"`
}
type SourceItems struct {
	Items []SourceItem `json:"items"`
	Next  string       `json:"next"`
}
type Job struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Trigger   string `json:"trigger"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	Recovery  string `json:"recovery,omitempty"`
	CreatedAt string `json:"createdAt"`
	NextRunAt string `json:"nextRunAt,omitempty"`
}

// Activity is one line of "what the background did", already in the user's words.
type Activity struct {
	ID     string `json:"id"`
	At     string `json:"at"`
	Text   string `json:"text"`
	To     string `json:"to,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}
type Organize struct {
	Done    int `json:"done"`
	Total   int `json:"total"`
	Version int `json:"version"`
}
type State struct {
	Organize         Organize            `json:"organize"`
	MemoryTotal      int                 `json:"memoryTotal"`
	Notices          []Notice            `json:"notices"`
	BudgetUsage      float64             `json:"budgetUsage"`
	Version          int                 `json:"version"`
	Revision         int64               `json:"revision"`
	Settings         Settings            `json:"settings"`
	Tasks            []Item              `json:"tasks"`
	Ideas            []Item              `json:"ideas"`
	Projects         []Item              `json:"projects"`
	Memories         []Memory            `json:"memories"`
	Candidates       []Candidate         `json:"candidates"`
	Agents           []Agent             `json:"agents"`
	Docs             []Doc               `json:"docs"`
	Runs             []Run               `json:"runs"`
	Samples          []Sample            `json:"samples"`
	Sources          []Source            `json:"sources"`
	Jobs             []Job               `json:"jobs"`
	Activity         []Activity          `json:"activity"`
	ExcludedMemories map[string][]string `json:"excludedMemories"`
}

// Commands are validated on the server; callers never submit an entire state.
type Command struct {
	DeskTurnIDs      []string        `json:"deskTurnIds,omitempty"`
	IncludeSources   bool            `json:"includeSources,omitempty"`
	RequestID        string          `json:"requestId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Type             string          `json:"type"`
	ID               string          `json:"id,omitempty"`
	IDs              []string        `json:"ids,omitempty"`
	Text             string          `json:"text,omitempty"`
	Title            string          `json:"title,omitempty"`
	Name             string          `json:"name,omitempty"`
	Note             string          `json:"note,omitempty"`
	Kind             string          `json:"kind,omitempty"`
	MemoryKind       string          `json:"memoryKind,omitempty"`
	ProjectID        string          `json:"projectId,omitempty"`
	Due              string          `json:"due,omitempty"`
	Status           string          `json:"status,omitempty"`
	State            string          `json:"state,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Summary          string          `json:"summary,omitempty"`
	TaskID           string          `json:"taskId,omitempty"`
	IdeaID           string          `json:"ideaId,omitempty"`
	ThingID          string          `json:"thingId,omitempty"`
	MemoryID         string          `json:"memoryId,omitempty"`
	ItemID           string          `json:"itemId,omitempty"`
	TriggerID        string          `json:"triggerId,omitempty"`
	ConditionID      string          `json:"conditionId,omitempty"`
	TargetID         string          `json:"targetId,omitempty"`
	Condition        string          `json:"condition,omitempty"`
	Description      string          `json:"description,omitempty"`
	Days             int             `json:"days,omitempty"`
	AgentIDs         []string        `json:"agentIds,omitempty"`
	AgentID          string          `json:"agentId,omitempty"`
	Prompt           string          `json:"prompt,omitempty"`
	Output           string          `json:"output,omitempty"`
	As               string          `json:"as,omitempty"`
	Patch            json.RawMessage `json:"patch,omitempty"`
	Doc              *Doc            `json:"doc,omitempty"`
}
type API interface {
	Snapshot(context.Context, memory.Scope) (State, error)
	Execute(context.Context, memory.Scope, Command) (State, error)
	Export(context.Context, memory.Scope, bool, bool) ([]byte, error)
}

// DeskAnswer is the assistant's reply to a desk question, with only the
// records it says it used.
type DeskAnswer struct {
	ID       string       `json:"id"`
	Answer   string       `json:"answer"`
	Agent    string       `json:"agent"`
	Used     []DeskSource `json:"used"`
	Searches []string     `json:"searches"`
	Links    []string     `json:"links"`
}

// DeskTurn is an earlier exchange on the same answer card.
type DeskTurn struct {
	ID       string `json:"id,omitempty"`
	Question string `json:"q"`
	Answer   string `json:"a"`
}
type DeskSource struct {
	Ref  memory.Ref `json:"ref"`
	Text string     `json:"text"`
}

// MemoryQuery combines list filters; cursors keep a stable insertion boundary.
type MemoryQuery struct {
	Retired   bool
	Group     string
	Category  string
	Q         string
	Entity    string
	Nature    string
	From      string
	To        string
	Project   string
	Epistemic string
	Agent     string
	Cursor    string
	Limit     int
}
type MemoryPage struct {
	Items []Memory `json:"items"`
	Next  string   `json:"next"`
	Total int      `json:"total"`
}
type MemoryFacet struct {
	EntityID string `json:"entityId"`
	Name     string `json:"name"`
	Count    int    `json:"count"`
}
type MemoryGroupFacet struct {
	EntityID string `json:"entityId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Count    int    `json:"count"`
}
type MemoryFacets struct {
	Groups []MemoryGroupFacet `json:"groups"`
	People []MemoryFacet      `json:"people"`
	Places []MemoryFacet      `json:"places"`
}
