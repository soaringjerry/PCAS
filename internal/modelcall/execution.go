package modelcall

import (
	"context"
	"encoding/json"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Usage is the usage record of one model call. References contain identity
// only, never text.
type Usage struct {
	DurationMS      *int64
	Tier            string
	OwnerID         memory.ID
	ID              memory.ID
	At              time.Time
	Purpose         string
	AgentID         string
	Model           string
	InputTokens     int
	OutputTokens    int
	InputEstimated  bool
	OutputEstimated bool
	CostEstimated   bool
	Cost            float64
	TurnID          string
	RunID           string
	JobID           string
	MemoryRefs      []memory.Ref
	Plan            json.RawMessage
}

// InteractivePolicy is the execution policy of the calls of an admitted
// secretary turn or a leased deputy run. The storage of interactive calls
// reads it to reserve, record, and settle each call.
type InteractivePolicy struct {
	Kind        string
	Token       string
	Origin      string
	RequestHash []byte
	AgentID     string
	ThingID     string
	Usage       Usage
	RetryOf     memory.ID
	InputPolicy string
	BudgetOwner string
}

// CallPolicy marks an admitted execution as the execution policy of a model call.
func (InteractivePolicy) CallPolicy() {}

type executionKey struct{}

// WithExecution gives ctx the identity of an admitted execution. An admitted
// request or a committed deputy lease supplies it. Child stages inherit it;
// they cannot create an execution from a turn ID.
func WithExecution(ctx context.Context, execution Request) context.Context {
	return context.WithValue(ctx, executionKey{}, execution)
}

// Execution returns the admitted execution of ctx.
func Execution(ctx context.Context) (Request, bool) {
	execution, ok := ctx.Value(executionKey{}).(Request)
	return execution, ok
}

// Timer collects the model time of one execution by the purpose of each call.
type Timer interface {
	AddModelTime(purpose string, ms int64)
}

type timerKey struct{}

// WithTimer gives ctx the timer of its execution.
func WithTimer(ctx context.Context, timer Timer) context.Context {
	return context.WithValue(ctx, timerKey{}, timer)
}

// Timed returns a context whose provider invocations add their time to the
// execution's timer under purpose. Without a timer it returns ctx.
func Timed(ctx context.Context, purpose string) context.Context {
	timer, ok := ctx.Value(timerKey{}).(Timer)
	if !ok {
		return ctx
	}
	return ai.WithInvocationObserver(ctx, func(ms int64) { timer.AddModelTime(purpose, ms) })
}
