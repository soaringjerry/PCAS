// Package app constructs the application's components and connects each
// domain service to its storage and model interfaces. The response table in
// responses.go names the handler of each queue stage.
package app

import (
	"github.com/soaringjerry/PCAS/internal/currentstate/datereview"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/postgres"
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
