// Package app constructs the application's components and connects each
// domain service to its storage and model interfaces.
package app

import (
	"github.com/soaringjerry/PCAS/internal/currentstate/datereview"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
	"github.com/soaringjerry/PCAS/internal/workspace/projecthandover"
	"github.com/soaringjerry/PCAS/internal/workspace/topicproject"
)

// Effort connects the workspace effort service to storage and generation.
func Effort(db *postgres.Store) effort.Service {
	return effort.Service{Store: db.EffortStore(), Generator: db, Now: db.BusinessNow}
}

// TopicProject connects the topic project service to storage and generation.
func TopicProject(db *postgres.Store) topicproject.Service {
	return topicproject.Service{Store: db.TopicProjectStore(), Generator: db, Now: db.BusinessNow}
}

// DateReview connects the date review service to storage and generation.
func DateReview(db *postgres.Store) datereview.Service {
	return datereview.Service{Store: db.DateReviewStore(), Generator: db}
}

// ProjectHandover connects the project handover service to storage and generation.
func ProjectHandover(db *postgres.Store) projecthandover.Service {
	return projecthandover.Service{Store: db.ProjectHandoverStore(), Generator: db}
}

// Handover connects the global handover service to storage and generation.
func Handover(db *postgres.Store) handover.Service {
	return handover.Service{Store: db.HandoverStore(), Generator: db}
}

// Handlers gives the background worker one handler for each queue stage.
// A stage whose domain has left the storage package uses its service here.
// The other stages still call the storage package directly.
func Handlers(db *postgres.Store) map[string]worker.Handler {
	return map[string]worker.Handler{
		"memory.compare":           db.ProcessCompare,
		"memory.entity_compare":    db.ProcessEntityCompare,
		"memory.entity_candidates": db.ProcessEntityCandidates,
		handover.Stage:             Handover(db).Process,
		projecthandover.Stage:      ProjectHandover(db).Process,
		effort.Stage:               Effort(db).Process,
		topicproject.Stage:         TopicProject(db).Process,
		datereview.Stage:           DateReview(db).Process,
		"memory.card":              db.ProcessCard,
		"memory.organize":          db.ProcessOrganize,
		"memory.summary":           db.ProcessSummary,
		"source.parse":             db.ProcessAttachment,
		"source.chunk":             db.ProcessChunks,
		"source.tokenize":          db.ProcessIndex,
		"memory.index":             db.ProcessIndex,
		"source.extract":           db.ProcessExtraction,
		"source.embed":             db.ProcessEmbedding,
		"memory.embed":             db.ProcessEmbedding,
	}
}
