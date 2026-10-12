package app

import (
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/postgres"
)

// Gateway builds the model gateway over the store's call records and the
// store's models. Every model call of the application goes through it. A
// store without models still gets a gateway: a job applies a result that was
// paid before, and only a new call fails as not configured.
func Gateway(db *postgres.Store) *modelcall.Gateway {
	models := db.Models
	var providers modelcall.Providers
	if models != nil {
		providers = models
	}
	records := db.CallRecords()
	gateway := modelcall.New(providers, records, records, records)
	if models != nil {
		gateway.WithDirectCalls(models, db.DirectCallRecords())
	}
	return gateway
}
