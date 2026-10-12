// Package ownerstore is the storage of the first rows of an owner: the workspace settings and the agents of the configured models.
package ownerstore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Owners works on the store's core.
type Owners struct{ *kernel.DB }
