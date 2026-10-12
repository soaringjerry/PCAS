// Package deputystore is the storage of the deputy: the preparation of a run, the run with its leases, its recovery and the selection of its result, and the revision of a document.
package deputystore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/usagelog"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
)

// Deputy works on the store's core.
type Deputy struct {
	*kernel.DB
	*budgetstore.Budgets
	*intakestore.Intake
	*memoryread.Reader
	*usagelog.Usage
	*usestore.Use
	*workspacestore.Workspace
}
