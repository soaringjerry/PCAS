package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// modelUsage mirrors model_usage. References contain identity only, never text.
type modelUsage struct {
	OwnerID      memory.ID
	ID           memory.ID
	At           time.Time
	Purpose      string
	AgentID      string
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         float64
	TurnID       string
	RunID        string
	JobID        string
	MemoryRefs   []memory.Ref
	Plan         json.RawMessage
}

func recordUsageTx(ctx context.Context, tx pgx.Tx, usage modelUsage) error {
	return nil
}
