// Package interpstore is the storage of interpretation: the organizing of statements, the scan for aliases, and the comparison of statements and of entities, each with its batches, its writes, and its schedule.
package interpstore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
)

// Interp works on the store's core.
type Interp struct {
	*kernel.DB
	*budgetstore.Budgets
	*ownerstore.Owners
}
