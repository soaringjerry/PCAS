// Package deskstore is the storage of the secretary: the admission and the order of a turn, the turn in its transaction with its context, its actions and its cards, the attachments of a request, and the cleanup of a check-only turn.
package deskstore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/commandstore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
)

// Desk works on the store's core.
type Desk struct {
	*budgetstore.Budgets
	*kernel.DB
	*commandstore.Commands
	*intakestore.Intake
	*memoryread.Reader
	*sourcestore.Sources
	*usestore.Use
	*workspacestore.Workspace
}
