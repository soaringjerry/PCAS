package doing

// Metrics only: never put requests, memory text, replies, credentials or group
// names into the persisted preparation or per-turn usage ledger.
type TierUsage struct {
	Requested         string         `json:"requested"`
	Effective         string         `json:"effective"`
	Calls             map[string]int `json:"calls"`
	Failed            map[string]int `json:"failed,omitempty"`
	NotStarted        map[string]int `json:"not_started,omitempty"`
	Malformed         map[string]int `json:"malformed,omitempty"`
	InputChars        map[string]int `json:"input_chars"`
	SelectedGroups    int            `json:"selected_groups"`
	SelfcheckFallback bool           `json:"selfcheck_fallback"`
}

type PreparationStage struct {
	Stage        string         `json:"stage"`
	WallMS       float64        `json:"wall_ms"`
	ModelCalls   int            `json:"model_calls"`
	FailedCalls  int            `json:"failed_calls"`
	InputChars   int            `json:"input_chars"`
	PurposeCalls map[string]int `json:"purpose_calls"`
	JobsDone     int            `json:"jobs_done"`
}
type Preparation struct {
	StartedAt   string             `json:"started_at"`
	CompletedAt string             `json:"completed_at,omitempty"`
	WallMS      float64            `json:"wall_ms"`
	Stages      []PreparationStage `json:"stages"`
	Claims      int                `json:"claims"`
	Retired     int                `json:"retired"`
	Cards       int                `json:"cards"`
	Handover    bool               `json:"handover"`
	Complete    bool               `json:"complete"`
}
