// Package app constructs the application's components and connects each
// domain service to its storage and model interfaces.
package app

import (
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

// Effort connects the workspace effort service to storage and generation.
func Effort(db *postgres.Store) effort.Service {
	return effort.Service{Store: db.EffortStore(), Generator: db, Now: db.BusinessNow}
}

// Handlers gives the background worker one handler for each queue stage.
// A stage whose domain has left the storage package uses its service here.
// The other stages still call the storage package directly.
func Handlers(db *postgres.Store) map[string]worker.Handler {
	return map[string]worker.Handler{
		"memory.compare":           db.ProcessCompare,
		"memory.entity_compare":    db.ProcessEntityCompare,
		"memory.entity_candidates": db.ProcessEntityCandidates,
		"memory.handover":          db.ProcessHandover,
		"memory.project_handover":  db.ProcessProjectHandover,
		effort.Stage:               Effort(db).Process,
		"memory.topic_project":     db.ProcessTopicProject,
		"memory.date_tidy":         db.ProcessDateTidy,
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
