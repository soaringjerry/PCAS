// Package workstore is the storage of the workspace that needs no command: the completion of a deadline, the signals of an item, the timeline of a project, and the text that a deputy wrote into an item.
package workstore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Work works on the store's core.
type Work struct{ *kernel.DB }
