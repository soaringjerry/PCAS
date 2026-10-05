package postgres_test

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type phase25B3Deadline struct {
	claim                             memory.ID
	kind, title, recurrence, timeNote string
	at                                *time.Time
}
type phase25B3CardOutput struct {
	fields        map[string][]memory.ID
	deadlines     []phase25B3Deadline
	applicability map[memory.ID]string
}
type phase25B3Adapter struct {
	encodeCard     func(phase25B3CardOutput, map[memory.ID]int) (string, error)
	encodeHandover func(string) (string, error)
	schedule       func(context.Context, time.Time) (int, error)
	process        func(context.Context, worker.Job) error
	setVersions    func(card, handover int) func()
}

func phase25B3ModelOutput(a phase25B3Adapter, result phase25B3CardOutput, numbers map[memory.ID]int) (string, error) {
	if a.encodeCard == nil {
		return "", phase25B234AwaitingProtocol
	}
	return a.encodeCard(result, numbers)
}

// Pending generation sequences: X3-1..5, rebuild halves of X3-6..8,
// X3-9..12; input cap, 25/60-item limits, unsupported field and repeated
// item rejection, handover headings/length/qualified facts, initial build order,
// 10-minute debounce, hourly total reservations, daily/6-hour handover budgets.
