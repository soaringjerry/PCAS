// Package activitystore is the storage of the query that gives the activity of one day.
package activitystore

import "github.com/soaringjerry/PCAS/internal/postgres/kernel"

// Activity works on the store's core.
type Activity struct{ *kernel.DB }
