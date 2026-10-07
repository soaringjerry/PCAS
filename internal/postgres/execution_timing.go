package postgres

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type executionTimingKey struct{}

type executionTiming struct {
	Kind        string `json:"kind"`
	Tier        string `json:"tier"`
	PrepareMS   int64  `json:"prepareMs"`
	AnswerMS    int64  `json:"answerMs"`
	SelfcheckMS int64  `json:"selfcheckMs"`
	WritebackMS int64  `json:"writebackMs"`
	ModelMS     int64  `json:"modelMs"`
	TotalMS     int64  `json:"totalMs"`
	OtherMS     int64  `json:"otherMs"`
}

type executionTimer struct {
	mu sync.Mutex
	executionTiming
	started        time.Time
	prepareStarted time.Time
	writeStarted   time.Time
	id             string
}

func newExecutionTimer(ctx context.Context, kind string, started time.Time) (context.Context, *executionTimer) {
	timer := &executionTimer{executionTiming: executionTiming{Kind: kind}, started: started}
	ctx = context.WithValue(ctx, executionTimingKey{}, timer)
	return executionCallContext(ctx, "prepare"), timer
}

func executionCallContext(ctx context.Context, purpose string) context.Context {
	timer, ok := ctx.Value(executionTimingKey{}).(*executionTimer)
	if !ok {
		return ctx
	}
	return ai.WithInvocationObserver(ctx, func(ms int64) {
		timer.mu.Lock()
		defer timer.mu.Unlock()
		timer.ModelMS += ms
		switch purpose {
		case "answer":
			timer.AnswerMS += ms
		case "selfcheck":
			timer.SelfcheckMS += ms
		}
	})
}

func (timer *executionTimer) beginPrepare() {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	timer.prepareStarted = time.Now()
}
func (timer *executionTimer) finishPrepare() {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	if !timer.prepareStarted.IsZero() {
		timer.PrepareMS += time.Since(timer.prepareStarted).Milliseconds()
		timer.prepareStarted = time.Time{}
	}
}
func (timer *executionTimer) addPrepare(elapsed time.Duration) {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	timer.PrepareMS += elapsed.Milliseconds()
}
func (timer *executionTimer) beginWrite(tier string) {
	timer.finishPrepare()
	timer.mu.Lock()
	defer timer.mu.Unlock()
	timer.Tier = tier
	timer.writeStarted = time.Now()
}

// Store once after the business transaction has finished. Measurement failures
// are observable but cannot invalidate an already committed answer or action.
// Replays never assign a new timer ID and cannot replace a good measurement.
func (s *Store) finishExecutionTiming(ctx context.Context, owner memory.ID, timer *executionTimer) {
	if timer.id == "" {
		return
	}
	timer.finishPrepare()
	timer.mu.Lock()
	timer.TotalMS = time.Since(timer.started).Milliseconds()
	if !timer.writeStarted.IsZero() {
		timer.WritebackMS = time.Since(timer.writeStarted).Milliseconds()
	}
	timer.OtherMS = max(int64(0), timer.TotalMS-timer.PrepareMS-timer.AnswerMS-timer.SelfcheckMS-timer.WritebackMS)
	row := timer.executionTiming
	timer.mu.Unlock()
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(persist, `INSERT INTO execution_timings(owner_id,id,kind,tier,at,prepare_ms,answer_ms,selfcheck_ms,writeback_ms,model_ms,total_ms,other_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(owner_id,kind,id) DO NOTHING`, owner, timer.id, row.Kind, row.Tier, timer.started, row.PrepareMS, row.AnswerMS, row.SelfcheckMS, row.WritebackMS, row.ModelMS, row.TotalMS, row.OtherMS)
	if err != nil {
		slog.ErrorContext(persist, "execution measurement failed", "kind", row.Kind, "error_type", backgroundFailureReason(err))
	}
}
