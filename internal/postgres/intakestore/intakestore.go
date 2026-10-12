// Package intakestore is the storage of what comes in and of its upkeep: attachments, connectors, archive imports, the processing jobs of a source, the edits and deletions of memory, and the recall.
package intakestore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/callstore"
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
)

// Intake works on the store's core.
type Intake struct {
	*kernel.DB
	*sourcestore.Sources
	*ownerstore.Owners
	*callstore.Calls
	*claimstore.Claims
	*memoryread.Reader
	*workstore.Work
}
