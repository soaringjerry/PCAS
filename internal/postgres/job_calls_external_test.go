package postgres_test

import (
	"github.com/soaringjerry/PCAS/internal/modelcall/jobcall"
	"github.com/soaringjerry/PCAS/internal/postgres"
)

// jobCalls composes the background call generator as the application does.
func jobCalls(s *postgres.Store) jobcall.Generator {
	return jobcall.Generator{Calls: s.Gateway(), Store: s.JobCallStore()}
}
