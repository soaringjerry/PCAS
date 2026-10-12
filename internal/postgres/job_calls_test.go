package postgres

import (
	"github.com/soaringjerry/PCAS/internal/modelcall/jobcall"
	"github.com/soaringjerry/PCAS/internal/retrieval"
)

// jobCalls composes the background call generator as the application does.
func jobCalls(s *Store) jobcall.Generator {
	return jobcall.Generator{Calls: s.Gateway(), Store: s.JobCallStore()}
}

// indexer composes the vector indexer as the application does.
func indexer(s *Store) retrieval.Indexer {
	out := retrieval.Indexer{Store: s.IndexStore(), Calls: s.Gateway()}
	if models := s.Models(); models != nil {
		out.Providers = models
	}
	return out
}
