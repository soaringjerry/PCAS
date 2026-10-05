package workspace

type Handover struct {
	Body    string `json:"body"`
	BuiltAt string `json:"builtAt"`
	Stale   bool   `json:"stale"`
}

type StatusCardField struct {
	Field string   `json:"field"`
	Items []Memory `json:"items"`
}

type StatusCard struct {
	Key     string            `json:"key"`
	Kind    string            `json:"kind"`
	Name    string            `json:"name"`
	Count   int               `json:"count"`
	BuiltAt string            `json:"builtAt"`
	Stale   bool              `json:"stale"`
	Fields  []StatusCardField `json:"fields"`
}

type StatusCardRef struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
	BuiltAt string `json:"builtAt"`
	Stale   bool   `json:"stale"`
}

type Deadline struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	At         *string `json:"at"`
	Recurrence string  `json:"recurrence"`
	Title      string  `json:"title"`
	TimeNote   string  `json:"timeNote"`
	MemoryID   string  `json:"memoryId"`
}

type BuildingProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type About struct {
	Handover  Handover         `json:"handover"`
	Cards     []StatusCard     `json:"cards"`
	Deadlines []Deadline       `json:"deadlines"`
	Building  BuildingProgress `json:"building"`
}
