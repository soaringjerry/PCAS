// Package memoryread is the storage of the reads of memories: the list and
// one memory for the owner, the memories that a turn or a run is given, and
// the status of a statement.
package memoryread

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Reader reads memories from the store's core.
type Reader struct{ *kernel.DB }
