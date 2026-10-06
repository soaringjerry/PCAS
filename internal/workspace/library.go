package workspace

import (
	"context"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// LibraryReader is the phase 2.6 read contract. Implementations must not mutate data.
type LibraryReader interface {
	ReadHandover(context.Context, memory.Scope) (Handover, error)
	ListDeadlines(context.Context, memory.Scope, DeadlineQuery) (DeadlineList, error)
	ListAssistantRequirements(context.Context, memory.Scope) (AssistantRequirementList, error)
	ListGroupMemories(context.Context, memory.Scope, string, GroupMemoryQuery) (MemoryPage, error)
	ListMemoryGroups(context.Context, memory.Scope) (MemoryGroupList, error)
}

type DeadlineQuery struct {
	Expired *bool
}

// DateStatus is upcoming, expired_unknown, recurring, or unclear.
type LibraryDeadline struct {
	Deadline
	Expired      bool   `json:"expired"`
	DateStatus   string `json:"dateStatus"`
	OriginalText string `json:"originalText"`
}

type DeadlineList struct {
	Items []LibraryDeadline `json:"items"`
}

// Scope is a model supplied explanation; Unrestricted means it applies every turn.
type AssistantRequirement struct {
	MemoryID     string `json:"memoryId"`
	Text         string `json:"text"`
	Unrestricted bool   `json:"unrestricted"`
	Scope        string `json:"scope"`
}

type AssistantRequirementList struct {
	Items []AssistantRequirement `json:"items"`
}

type GroupMemoryQuery struct {
	Cursor string
	Limit  int
}

// Key is opaque to clients and can be passed directly to ListGroupMemories.
// Kind is person, project, topic, area, or self. Self names describe a memory category.
type LibraryMemoryGroup struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type MemoryGroupList struct {
	Items []LibraryMemoryGroup `json:"items"`
}
