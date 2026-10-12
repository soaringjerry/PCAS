package kernel

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// DB is what every storage package works on: the connection pool, the
// configured models, the file store, and the business clock. A storage
// package reads the models' configuration and availability. It makes no
// model call.
type DB struct {
	Pool *pgxpool.Pool
	// Models is nil until the application gives the configured models.
	Models *ai.Registry
	// Blobs is nil when no file store is configured.
	Blobs         memory.BlobStore
	businessClock func() time.Time
	// Slots limits the flows that hold a connection across a model call: a
	// secretary turn and the reading of an attachment. One connection stays
	// free for the reads and the reservations inside such a flow.
	Slots chan struct{}
}

// SetBusinessClock sets business input, dates, retrieval decay, and deadline display time.
// Configure it before operations start. Nil restores actual time. Lease expiry,
// access checks, retries, operational quotas, and accounting use actual time.
func (d *DB) SetBusinessClock(now func() time.Time) { d.businessClock = now }

// HasBusinessClock reports whether a business clock replaces actual time.
func (d *DB) HasBusinessClock() bool { return d.businessClock != nil }

// BusinessNow is the business clock that model input uses.
func (d *DB) BusinessNow() time.Time {
	if d.businessClock != nil {
		return d.businessClock()
	}
	return time.Now()
}
