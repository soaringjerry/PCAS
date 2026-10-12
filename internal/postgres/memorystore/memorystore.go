// Package memorystore is the storage of derived memory material that needs no other storage code: the summaries, the groups of sources, the graph around a record, the expansion of a reference, and the backfill jobs.
package memorystore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Memory works on the store's core.
type Memory struct{ *kernel.DB }
