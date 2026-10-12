// Package app constructs the application's components and connects each
// domain service to its storage and model interfaces. The response table in
// responses.go names the handler of each queue stage.
package app

import (
	"github.com/soaringjerry/PCAS/internal/currentstate/datereview"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/input/voice"
	"github.com/soaringjerry/PCAS/internal/interpretation/aliasscan"
	"github.com/soaringjerry/PCAS/internal/interpretation/comparison"
	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/modelcall/jobcall"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
	"github.com/soaringjerry/PCAS/internal/workspace/projecthandover"
	"github.com/soaringjerry/PCAS/internal/workspace/topicproject"
)

// JobCalls makes the model calls of background jobs through the gateway. The
// store keeps their paid results.
func JobCalls(db *postgres.Store) jobcall.Generator {
	return jobcall.Generator{Calls: Gateway(db), Store: db.JobCallStore()}
}

// Effort connects the workspace effort service to storage and generation.
func Effort(db *postgres.Store) effort.Service {
	return effort.Service{Store: db.EffortStore(), Generator: JobCalls(db), Now: db.BusinessNow}
}

// TopicProject connects the topic project service to storage and generation.
func TopicProject(db *postgres.Store) topicproject.Service {
	return topicproject.Service{Store: db.TopicProjectStore(), Generator: JobCalls(db), Now: db.BusinessNow}
}

// DateReview connects the date review service to storage and generation.
func DateReview(db *postgres.Store) datereview.Service {
	return datereview.Service{Store: db.DateReviewStore(), Generator: JobCalls(db)}
}

// ProjectHandover connects the project handover service to storage and generation.
func ProjectHandover(db *postgres.Store) projecthandover.Service {
	return projecthandover.Service{Store: db.ProjectHandoverStore(), Generator: JobCalls(db)}
}

// Handover connects the global handover service to storage and generation.
func Handover(db *postgres.Store) handover.Service {
	return handover.Service{Store: db.HandoverStore(), Generator: JobCalls(db)}
}

// AliasScan connects the alias scan service to storage and generation.
func AliasScan(db *postgres.Store) aliasscan.Service {
	return aliasscan.Service{Store: db.AliasScanStore(), Generator: JobCalls(db)}
}

// Compare connects the statement comparison service to storage and generation.
func Compare(db *postgres.Store) comparison.Service {
	return comparison.Service{Store: db.CompareStore(), Generator: JobCalls(db), Version: kernel.CompareVersion}
}

// Extraction connects the extraction service to storage and generation.
func Extraction(db *postgres.Store) extraction.Service {
	return extraction.Service{Store: db.ExtractionStore(), Generator: JobCalls(db), MaxAttempts: postgres.MaxJobAttempts}
}

// Organize connects the organize service to storage and generation.
func Organize(db *postgres.Store) organize.Service {
	return organize.Service{Store: db.OrganizeStore(), Generator: JobCalls(db), Version: kernel.OrganizeVersion}
}

// EntityCompare connects the entity comparison service to storage and generation.
func EntityCompare(db *postgres.Store) comparison.EntityService {
	return comparison.EntityService{Store: db.EntityCompareStore(), Generator: JobCalls(db), Version: kernel.EntityCompareVersion}
}

// Embedding returns the service of the vector index stages.
func Embedding(db *postgres.Store) retrieval.Indexer {
	indexer := retrieval.Indexer{Store: db.IndexStore(), Calls: Gateway(db)}
	if models := db.Models; models != nil {
		indexer.Providers = models
	}
	return indexer
}

// Voice transcribes the voice messages of a channel.
func Voice(db *postgres.Store) voice.Transcriber {
	return voice.Transcriber{Models: db.Models, Calls: Gateway(db)}
}
