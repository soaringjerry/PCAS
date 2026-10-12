// Package claimstore is the storage of the write of a memory: the statement with its evidence, its record, and the entities that it names.
package claimstore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
)

// Claims works on the store's core.
type Claims struct {
	*kernel.DB
	*sourcestore.Sources
}
