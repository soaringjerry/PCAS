package callstore

import "github.com/soaringjerry/PCAS/internal/modelcall"

// CallRecords are the records of the gateway's calls in storage.
type CallRecords interface {
	modelcall.Accounting
	modelcall.Results
	modelcall.Journal
}

// Journal returns the unified call journal for background jobs and interactive
// executions.
func (s *Calls) Journal() Journal { return Journal{Store: s} }

// CallRecords gives the gateway its reservations, its paid results, and its
// unified call journal.
func (s *Calls) CallRecords() CallRecords { return s.Journal() }

// DirectCallRecords gives the gateway the journal of its embedding, image,
// and audio calls.
func (s *Calls) DirectCallRecords() modelcall.DirectJournal { return DirectCalls{Store: s} }
