// Package statestore is the storage of the current state: the review of a remembered date.
package statestore

import (
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
)

// State works on the store's core.
type State struct {
	*kernel.DB
	*memoryread.Reader
}
