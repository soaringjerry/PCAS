package workspace

// DeskTurnRequest is also the server-side entry point used by connectors.
type DeskTurnRequest struct {
	RequestID      string  `json:"requestId"`
	ConversationID *string `json:"conversationId"`
	ThingID        *string `json:"thingId"`
	Text           string  `json:"text"`
	AgentID        string  `json:"agentId"`
}
type DeskAsk struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}
type DeskReceipt struct {
	ActionID *string `json:"actionId"`
	Op       string  `json:"op"`
	Text     string  `json:"text"`
	ThingID  *string `json:"thingId"`
	Undoable bool    `json:"undoable"`
	Status   string  `json:"status"`
	Reason   string  `json:"reason,omitempty"`
}
type DeskCard struct {
	Kind  string `json:"kind"`
	Title string `json:"title,omitempty"`
	Items any    `json:"items"`
}
type SecretaryTurn struct {
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
	At       *string `json:"at"`
	Text     string  `json:"text"`
	Status   string  `json:"status"`
	MemoryID *string `json:"memoryId"`
	ThingID  *string `json:"thingId"`
}
type DeskTaskItem struct {
	ThingID string  `json:"thingId"`
	Title   string  `json:"title"`
	Due     *string `json:"due"`
	Project *string `json:"project"`
	Status  string  `json:"status"`
}
