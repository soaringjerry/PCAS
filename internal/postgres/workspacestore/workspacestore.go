// Package workspacestore is the storage of the workspace: the snapshot, the action log with its undo helpers, the documents and their versions, the reminders, the projects made from topics, and the handover of a project.
package workspacestore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/postgres/statestore"
)

// Workspace works on the store's core.
type Workspace struct {
	*kernel.DB
	*claimstore.Claims
	*intakestore.Intake
	*memoryread.Reader
	*ownerstore.Owners
	*sourcestore.Sources
	*statestore.State
}
