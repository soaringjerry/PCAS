// Package schedulestore is the storage of the owner's repeating schedules and of the rule that gives the next time of one.
package schedulestore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Schedules works on the store's core.
type Schedules struct{ *kernel.DB }
