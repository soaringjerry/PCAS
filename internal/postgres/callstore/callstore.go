// Package callstore is the storage of the records of model calls: the reservations, the paid results, the call journals of background, interactive, and direct calls, and the recovery of their accounting.
package callstore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/usagelog"
)

// Calls works on the store's core.
type Calls struct {
	*kernel.DB
	*usagelog.Usage
	*budgetstore.Budgets
	CallPending
}
