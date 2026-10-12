package callstore

import "github.com/soaringjerry/PCAS/internal/modelcall"

// CallRecords are the records of the gateway's calls in storage.
type CallRecords interface {
	modelcall.Accounting
	modelcall.Results
	modelcall.Journal
}

// CallRecords gives the gateway its reservations, its paid results, and its
// call journal.
func (s *Calls) CallRecords() CallRecords { return s.records() }

// DirectCallRecords gives the gateway the journal of its embedding, image,
// and audio calls.
func (s *Calls) DirectCallRecords() modelcall.DirectJournal { return DirectCalls{Store: s} }

func (s *Calls) records() ModelCallStorage {
	return ModelCallStorage{BackgroundCalls{Store: s}, InteractiveCalls{Store: s}}
}
