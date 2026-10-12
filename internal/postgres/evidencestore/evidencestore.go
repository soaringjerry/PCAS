// Package evidencestore is the storage of the evidence of a memory: the source text around a statement that a turn or a run is given.
package evidencestore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Evidence works on the store's core.
type Evidence struct{ *kernel.DB }
