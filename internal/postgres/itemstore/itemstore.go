// Package itemstore is the owner of the work items: every write of the items table is here, with the blocks of the text that a deputy wrote into an item.
package itemstore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Items works on the store's core.
type Items struct{ *kernel.DB }
