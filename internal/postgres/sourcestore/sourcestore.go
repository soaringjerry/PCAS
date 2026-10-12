// Package sourcestore is the storage of sources: the ingestion of a source version, the read of one source, and the invalidation of what was derived from a version.
package sourcestore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Sources works on the store's core.
type Sources struct{ *kernel.DB }
