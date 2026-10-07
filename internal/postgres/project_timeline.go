package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func refreshTaskPlansTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) error {
	items, err := queryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND document->>'estimatedHours' IS NOT NULL ORDER BY id`, string(scope.OwnerID))
	if err != nil {
		return err
	}
	for _, item := range items {
		before := string(asJSON(item))
		if err = maintainTaskPlanTx(ctx, tx, scope, &item); err != nil {
			return err
		}
		if before == string(asJSON(item)) {
			continue
		}
		item.Version++
		item.UpdatedAt = stamp()
		if err = saveItem(ctx, tx, scope, item); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) ReadProjectTimeline(ctx context.Context, scope memory.Scope, project string) (workspace.ProjectTimeline, error) {
	out := workspace.ProjectTimeline{ProjectID: project, DailyHours: dailyWorkHours, Items: []workspace.TimelineItem{}, WithoutDue: []workspace.Item{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(project).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		p, err := getItem(ctx, tx, scope, project)
		if err != nil {
			return err
		}
		if p.Kind != "project" {
			return memory.ErrInvalid
		}
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		items, err := queryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND project_id=$2 ORDER BY due_at NULLS LAST,created_at,id`, string(scope.OwnerID), project)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, item := range items {
			if item.Kind == "task" {
				item.StartDate, err = taskStartDate(item.Due, item.EstimatedHours, deskLocation(settings))
				if err != nil {
					return err
				}
			}
			if item.Due == "" {
				out.WithoutDue = append(out.WithoutDue, item)
				continue
			}
			due, err := time.Parse(time.RFC3339, item.Due)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, workspace.TimelineItem{ID: item.ID, Title: item.Title, StartDate: item.StartDate, Due: item.Due, Status: item.Status, EstimatedHours: item.EstimatedHours, Overdue: due.Before(now)})
		}
		return nil
	})
	return out, err
}
