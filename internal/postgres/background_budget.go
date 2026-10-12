package postgres

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// StageHourlyLimits returns the number of calls that each limited stage can
// reserve in one hour. A stage that is absent has no stage limit.
func StageHourlyLimits() map[string]int {
	out := make(map[string]int, len(kernel.HourlyBudgets))
	for stage, limit := range kernel.HourlyBudgets {
		out[stage] = limit
	}
	return out
}
