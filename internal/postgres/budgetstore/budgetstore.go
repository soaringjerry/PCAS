// Package budgetstore is the storage of the spending limits: the reservation of the cost of a call against the daily budget, the health of the background stages, and the timing record of an execution.
package budgetstore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
)

// Budgets works on the store's core.
type Budgets struct {
	*kernel.DB
	*ownerstore.Owners
}
