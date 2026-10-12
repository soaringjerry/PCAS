// Package commandstore is the storage of the workspace commands: the dispatch of one command, the undo of one action with the undo of a comparison, and the entry that executes a command.
package commandstore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/deputystore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
)

// Commands works on the store's core.
type Commands struct {
	*kernel.DB
	*claimstore.Claims
	*ownerstore.Owners
	*deputystore.Deputy
	*intakestore.Intake
	*sourcestore.Sources
	*workspacestore.Workspace
}
