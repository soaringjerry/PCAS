package itemcommand

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// Origin says who makes a new item.
type Origin struct {
	// Actor is the name in the first history entry.
	Actor string
	// SecretarySource is the owner's sentence that the secretary acted on, or
	// nil when the secretary did not make the item.
	SecretarySource *workspace.SourceRef
	// ActionID is the secretary's action that makes the item, or empty.
	ActionID string
	// Collected is true when the secretary collects the history of its action
	// and publishes one creation entry at the end.
	Collected bool
}

// New makes the task, idea, or project of a creation command. The caller
// gives the item its identity and saves it with the summary "创建".
func New(ctx context.Context, c workspace.Command, origin Origin, fx Effects) (workspace.Item, error) {
	kind := map[string]string{"addTask": "task", "addIdea": "idea", "addProject": "project"}[c.Type]
	title := c.Title
	if kind == "project" {
		title = c.Name
	}
	item := workspace.NewItem(kind, title)
	if origin.SecretarySource != nil {
		original := *origin.SecretarySource
		item.Creation = &workspace.ItemCreation{By: "secretary", Source: &original, ActionID: origin.ActionID}
		if original.SourceID != "" {
			item.Sources = append(item.Sources, original)
		}
	}
	item.History[0].By = origin.Actor
	item.Evolution[0].By = origin.Actor
	if origin.Collected {
		// The secretary publishes one creation revision after all fields
		// and reminders have been applied by its internal commands.
		item.History = []workspace.Revision{}
		item.Evolution = []workspace.Revision{}
	} else if item.Creation != nil {
		// A companion project has its own action, outside the task's history
		// collector. The save publishes its single creation revision.
		item.History = []workspace.Revision{}
	}
	if kind == "task" && c.Text != "" {
		if err := RequireText(c.Text); err != nil {
			return item, err
		}
		item.Notes = c.Text
	}
	item.ProjectID = c.ProjectID
	if kind == "task" && c.Due != "" {
		if _, err := time.Parse(time.RFC3339, c.Due); err != nil {
			return item, memory.ErrInvalid
		}
		item.Due = c.Due
		loc, err := fx.Location(ctx)
		if err != nil {
			return item, err
		}
		secretary.ApplyDueReminder(&item, "", loc)
	}
	return item, nil
}
