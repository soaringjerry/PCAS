// Package usestore is the storage of the memory that a turn or a run is given: the context of use, its checks, and the reads of the deputy before its readers.
package usestore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
	"github.com/soaringjerry/PCAS/internal/postgres/statestore"
)

// Use works on the store's core.
type Use struct {
	*kernel.DB
	*memoryread.Reader
	*ownerstore.Owners
	*statestore.State
}
