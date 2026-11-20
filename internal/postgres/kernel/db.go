package kernel

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Models is what storage reads of the configured models. The provider
// registry implements it. Storage makes no call through it.
type Models interface {
	// ExtractionID is the default text model. It is empty when none is set.
	ExtractionID() string
	// EmbeddingID is the embedding model. It is empty when none is set.
	EmbeddingID() string
	// Available reports whether the model can be called now.
	Available(id string) bool
	Describe(id string) (memory.ModelInfo, bool)
	List() []memory.ModelInfo
	// Estimate is the cost that a call of the model with the input reserves.
	Estimate(id, input string) float64
	// DirectDefault reports whether the direct ChatGPT connection is ready
	// to be the default text model.
	DirectDefault() bool
	// EndJob closes a login that the worker reloads for each job.
	EndJob()
}

// DB is what every storage package works on: the connection pool, the
// configured models, the file store, and the business clock. A storage
// package reads the models' configuration and availability. It makes no
// model call.
type DB struct {
	Pool *pgxpool.Pool
	// Models is nil until the application gives the configured models.
	Models Models
	// Blobs is nil when no file store is configured.
	Blobs         memory.BlobStore
	businessClock func() time.Time
	// Slots limits the flows that hold a connection across a model call: a
	// secretary turn and the reading of an attachment. One connection stays
	// free for the reads and the reservations inside such a flow.
	Slots chan struct{}
	// hourly is the number of calls that each limited background stage can
	// reserve in one hour.
	hourly map[string]int
}

// NewDB returns the storage over the pool. hourly gives the number of calls
// that each limited background stage can reserve in one hour; a stage that is
// absent has no stage limit.
func NewDB(pool *pgxpool.Pool, hourly map[string]int) *DB {
	d := &DB{Pool: pool, hourly: map[string]int{}}
	for stage, limit := range hourly {
		d.hourly[stage] = limit
	}
	if pool != nil {
		d.Slots = make(chan struct{}, pool.Config().MaxConns-1)
	}
	return d
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

// Model returns the configuration of a model. It is false when no model is
// configured or the model is unknown.
func (d *DB) Model(id string) (memory.ModelInfo, bool) {
	if d.Models == nil {
		return memory.ModelInfo{}, false
	}
	return d.Models.Describe(id)
}

// TextModel returns the default text model. It is false when none is
// configured or the default is an embedding or a transcription model.
func (d *DB) TextModel() (memory.ModelInfo, bool) {
	if d.Models == nil {
		return memory.ModelInfo{}, false
	}
	p, ok := d.Models.Describe(d.Models.ExtractionID())
	return p, ok && !p.Embedding && !p.Transcription
}

// ModelAvailable reports whether the model can be called now.
func (d *DB) ModelAvailable(id string) bool {
	return d.Models != nil && d.Models.Available(id)
}
