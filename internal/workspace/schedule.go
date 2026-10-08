package workspace

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// ScheduleReader is the phase 3.5 read contract. Dates are inclusive civil dates
// in the owner's timezone. Implementations never materialize occurrences.
type ScheduleReader interface {
	ListSchedule(context.Context, memory.Scope, ScheduleQuery) (Schedule, error)
	ListInProgress(context.Context, memory.Scope) (InProgress, error)
}
type ScheduleQuery struct{ From, To string }
type Schedule struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	Timezone string          `json:"timezone"`
	Days     []ScheduleDay   `json:"days"`
	Unclear  []ScheduleEntry `json:"unclear"`
	Overdue  []ScheduleEntry `json:"overdue"`
}
type ScheduleDay struct {
	Date  string          `json:"date"`
	Items []ScheduleEntry `json:"items"`
}
type ScheduleEntry struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Kind         string         `json:"kind"` // deadline, appointment, recurring, task
	Date         string         `json:"date,omitempty"`
	At           *string        `json:"at"`
	DateOnly     bool           `json:"dateOnly"`
	TimeNote     string         `json:"timeNote"`
	OriginalText string         `json:"originalText"`
	Source       ScheduleSource `json:"source"`
}
type ScheduleSource struct {
	Kind       string `json:"kind"` // deadline or task
	MemoryID   string `json:"memoryId,omitempty"`
	ItemID     string `json:"itemId,omitempty"`
	DeadlineID string `json:"deadlineId,omitempty"`
}
type ItemCreation struct {
	By        string     `json:"by"` // secretary, background_extraction, background_topic
	Source    *SourceRef `json:"source,omitempty"`
	MemoryIDs []string   `json:"memoryIds,omitempty"`
	GroupKey  string     `json:"groupKey,omitempty"`
}
type InProgress struct {
	Items     []Item `json:"items"`
	Total     int    `json:"total"`
	Remaining int    `json:"remaining"`
}
