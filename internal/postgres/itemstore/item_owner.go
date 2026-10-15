package itemstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

// GetItem reads one item of the owner.
func GetItem(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (workspace.Item, error) {
	if !memory.ID(id).Valid() {
		return workspace.Item{}, memory.ErrInvalid
	}
	return kernel.QueryDocument[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
}

// SaveItem is the one write of an item. It applies the plan rule of a task,
// keeps the item's text blocks in step, checks the item, and writes its row.
func SaveItem(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item) error {
	if err := MaintainTaskPlanTx(ctx, tx, scope, &item); err != nil {
		return err
	}
	if err := syncArtifactEditsTx(ctx, tx, scope, item); err != nil {
		return err
	}
	if !memory.ID(item.ID).Valid() || kernel.RequireText(item.Title) != nil || len(item.Title) > 2000 {
		return memory.ErrInvalid
	}
	if !kernel.OneOf(item.Kind, "task", "idea", "project") {
		return memory.ErrInvalid
	}
	if item.Kind == "task" && !kernel.OneOf(item.Status, "todo", "doing", "waiting", "done", "cancelled") || item.Kind == "idea" && !kernel.OneOf(item.Status, "active", "shelved", "awakened", "promoted", "dropped") || item.Kind == "project" && !kernel.OneOf(item.Status, "active", "paused", "done") {
		return memory.ErrInvalid
	}
	if item.ProjectID != "" {
		p, err := GetItem(ctx, tx, scope, item.ProjectID)
		if err != nil {
			return err
		}
		if p.Kind != "project" || item.Kind == "project" {
			return memory.ErrInvalid
		}
	}
	for _, d := range []string{item.Due, item.Scheduled} {
		if d != "" {
			if _, err := time.Parse(time.RFC3339, d); err != nil {
				return memory.ErrInvalid
			}
		}
	}
	for _, id := range item.DependsOn {
		dependency, err := GetItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if dependency.Kind != "task" || id == item.ID {
			return memory.ErrInvalid
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO work_items(owner_id,id,kind,title,status,project_id,due_at,scheduled_at,version,document,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(owner_id,id) DO UPDATE SET title=excluded.title,status=excluded.status,project_id=excluded.project_id,due_at=excluded.due_at,scheduled_at=excluded.scheduled_at,version=excluded.version,document=excluded.document,updated_at=excluded.updated_at`, string(scope.OwnerID), item.ID, item.Kind, item.Title, item.Status, kernel.NullString(item.ProjectID), kernel.NullString(item.Due), kernel.NullString(item.Scheduled), item.Version, kernel.JSON(item), item.CreatedAt, item.UpdatedAt)
	return err
}

// MaintainTaskPlanTx applies the workspace plan rule to a task with the
// owner's settings. Every item save applies it.
func MaintainTaskPlanTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item *workspace.Item) error {
	if item.Kind != "task" {
		return nil
	}
	settings, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
	if err != nil {
		return err
	}
	return effort.PlanTask(item, settings, settings.Location())
}

// DeleteItem removes one item row. The caller removes what refers to the item
// first.
func DeleteItem(ctx context.Context, tx pgx.Tx, owner memory.ID, id string) error {
	_, err := tx.Exec(ctx, "DELETE FROM work_items WHERE owner_id=$1 AND id=$2", string(owner), id)
	return err
}

// RemoveItemDependency takes the item out of the dependencies of each task
// that waits for it.
func RemoveItemDependency(ctx context.Context, tx pgx.Tx, owner memory.ID, id string) error {
	_, err := tx.Exec(ctx, `UPDATE work_items SET document=jsonb_set(document,'{dependsOn}',(document->'dependsOn')-$2)
		WHERE owner_id=$1 AND kind='task' AND document->'dependsOn' ? $2`, string(owner), id)
	return err
}

// DeleteUnusedCreatedItem removes an item that is still in its first version
// and that no item, document, or run refers to. It leaves every other item.
func DeleteUnusedCreatedItem(ctx context.Context, tx pgx.Tx, owner memory.ID, id string) error {
	_, err := tx.Exec(ctx, `DELETE FROM work_items w WHERE w.owner_id=$1 AND w.id=$2 AND w.version=1 AND NOT EXISTS(SELECT 1 FROM work_items c WHERE c.owner_id=w.owner_id AND c.project_id=w.id) AND NOT EXISTS(SELECT 1 FROM work_documents d WHERE d.owner_id=w.owner_id AND d.thing_id=w.id) AND NOT EXISTS(SELECT 1 FROM agent_runs r WHERE r.owner_id=w.owner_id AND r.thing_id=w.id)`, string(owner), id)
	return err
}
