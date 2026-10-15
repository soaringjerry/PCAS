package workstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

func RefreshTaskPlansTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) error {
	items, err := kernel.QueryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND document->>'estimatedHours' IS NOT NULL ORDER BY id`, string(scope.OwnerID))
	if err != nil {
		return err
	}
	for _, item := range items {
		before := string(kernel.JSON(item))
		if err = itemstore.MaintainTaskPlanTx(ctx, tx, scope, &item); err != nil {
			return err
		}
		if before == string(kernel.JSON(item)) {
			continue
		}
		item.Version++
		item.UpdatedAt = kernel.Stamp()
		if err = itemstore.SaveItem(ctx, tx, scope, item); err != nil {
			return err
		}
	}
	return nil
}
func (s *Work) ReadProjectTimeline(ctx context.Context, scope memory.Scope, project string) (workspace.ProjectTimeline, error) {
	out := workspace.ProjectTimeline{ProjectID: project, DailyHours: effort.DailyHours, Items: []workspace.TimelineItem{}, WithoutDue: []workspace.Item{}}
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(project).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		p, err := itemstore.GetItem(ctx, tx, scope, project)
		if err != nil {
			return err
		}
		if p.Kind != "project" {
			return memory.ErrInvalid
		}
		settings, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		items, err := kernel.QueryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND project_id=$2 ORDER BY due_at NULLS LAST,created_at,id`, string(scope.OwnerID), project)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, item := range items {
			if item.Kind == "task" {
				item.StartDate, err = effort.StartDate(item.Due, item.EstimatedHours, settings.Location())
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
