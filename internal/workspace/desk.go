package workspace

import (
	"encoding/json"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// DeskTurnRequest is also the server-side entry point used by connectors.
type DeskTurnRequest struct {
	Attachments    []memory.Ref `json:"attachments,omitempty"`
	RequestID      string       `json:"requestId"`
	ConversationID *string      `json:"conversationId"`
	ThingID        *string      `json:"thingId"`
	Text           string       `json:"text"`
	AgentID        string       `json:"agentId"`
}
type DeskAsk struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}
type DeskReceipt struct {
	SourceID      *string `json:"sourceId,omitempty"`
	SourceVersion int     `json:"sourceVersion,omitempty"`
	ActionID      *string `json:"actionId"`
	Op            string  `json:"op"`
	Text          string  `json:"text"`
	ThingID       *string `json:"thingId"`
	Undoable      bool    `json:"undoable"`
	Undone        bool    `json:"undone"`
	Status        string  `json:"status"`
	Reason        string  `json:"reason,omitempty"`
}
type DeskCard struct {
	Kind  string `json:"kind"`
	Title string `json:"title,omitempty"`
	Items any    `json:"items"`
}
type SecretaryTurn struct {
	Outdated  bool          `json:"outdated,omitempty"`
	ID        string        `json:"id"`
	Text      string        `json:"text"`
	Reply     string        `json:"reply"`
	Cards     []DeskCard    `json:"cards"`
	Receipts  []DeskReceipt `json:"receipts"`
	Ask       *DeskAsk      `json:"ask"`
	Agent     string        `json:"agent"`
	CreatedAt string        `json:"createdAt"`
}
type DeskTurnResponse struct {
	ConversationID string        `json:"conversationId"`
	Turn           SecretaryTurn `json:"turn"`
	State          State         `json:"state"`
}
type DeskTurnsResponse struct {
	ConversationID string          `json:"conversationId"`
	Turns          []SecretaryTurn `json:"turns"`
}
type DeskSourceItem struct {
	Kind          string  `json:"kind,omitempty"`
	MemoryID      string  `json:"memoryId"`
	Version       int     `json:"version"`
	Text          string  `json:"text"`
	SourceID      string  `json:"sourceId"`
	SourceVersion int     `json:"sourceVersion"`
	At            *string `json:"at"`
}
type DeskLinkItem struct {
	URL  string `json:"url"`
	Host string `json:"host"`
}
type DeskTimelineItem struct {
	EventFrom      string          `json:"eventFrom,omitempty"`
	EventTo        string          `json:"eventTo,omitempty"`
	EventPrecision string          `json:"eventPrecision,omitempty"`
	Mentions       []MemoryMention `json:"mentions"`
	At             *string         `json:"at"` // When the memory was expressed; nil when unknown.
	Text           string          `json:"text"`
	Status         string          `json:"status"` // open, done, dropped or changed.
	MemoryID       *string         `json:"memoryId"`
	ThingID        *string         `json:"thingId"`
}

// MarshalJSON keeps absent mentions an empty array in stored and new cards.
func (item DeskTimelineItem) MarshalJSON() ([]byte, error) {
	type wire DeskTimelineItem
	if item.Mentions == nil {
		item.Mentions = []MemoryMention{}
	}
	return json.Marshal(wire(item))
}

type DeskTaskItem struct {
	ThingID string  `json:"thingId"`
	Title   string  `json:"title"`
	Due     *string `json:"due"`
	Project *string `json:"project"`
	Status  string  `json:"status"`
}
