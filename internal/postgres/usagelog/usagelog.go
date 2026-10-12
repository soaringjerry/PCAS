// Package usagelog is the storage of the usage records of model calls and of the owner's view of them.
package usagelog

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Usage works on the store's core.
type Usage struct{ *kernel.DB }
