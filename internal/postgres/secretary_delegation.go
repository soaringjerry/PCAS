package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type secretaryActionPlanKey struct{}

// IDs and context are assigned by the server before the commit transaction.
// Prospective N aliases support retrieval; only successful real creations bind
// the execution aliases. No prediction can turn a failed create into a target.
type secretaryActionPlan struct {
	ItemID   string
	TargetID string
	Projects map[string]string
	Prepared *preparedRunContext
	Error    error
}

func secretaryPlannedItemID(ctx context.Context) string {
	if p, ok := ctx.Value(secretaryActionPlanKey{}).(secretaryActionPlan); ok {
		return p.ItemID
	}
	return ""
}

func (s *Store) planSecretaryActions(ctx context.Context, scope memory.Scope, c secretaryContext, actions []secretaryAction) ([]secretaryActionPlan, error) {
	plans := make([]secretaryActionPlan, min(len(actions), 10))
	aliases := make(map[string]workspace.Item, len(c.Aliases)+10)
	for alias, item := range c.Aliases {
		aliases[alias] = item
	}
	prospective := map[int]workspace.Item{}
	projects := map[string]string{}
	// Read only local project identities. No owner gate, mutation or provider
	// call is held while the per-delegation retrieval below runs.
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for i := range plans {
			a := actions[i]
			p := &plans[i]
			p.Projects = map[string]string{}
			if a.parseErr != nil {
				continue
			}
			switch a.Op {
			case "create_task", "create_idea", "create_project":
				title := strings.TrimSpace(a.Title)
				if a.Op == "create_project" {
					title = strings.TrimSpace(a.Name)
				}
				if !validDeskTitle(title) {
					continue
				}
				p.ItemID = string(memory.NewID())
				kind := map[string]string{"create_task": "task", "create_idea": "idea", "create_project": "project"}[a.Op]
				item := newItem(kind, title)
				item.ID = p.ItemID
				if a.Project != nil {
					project, err := planSecretaryProjectTx(ctx, tx, scope, *a.Project, aliases, projects, p)
					if err != nil {
						continue
					}
					item.ProjectID = project
				}
				if a.Notes != nil {
					item.Notes = *a.Notes
				}
				aliases[fmt.Sprintf("N%d", i+1)] = item
				if kind == "project" {
					if _, exists := projects[strings.ToLower(title)]; !exists {
						projects[strings.ToLower(title)] = item.ID
					}
				}
			case "update":
				item, ok := aliases[a.Ref]
				if !ok {
					continue
				}
				if title, ok := textField(a.Set, "title"); ok {
					if !validDeskTitle(title) {
						continue
					}
					item.Title = strings.TrimSpace(title)
				}
				if project, ok := textField(a.Set, "project"); ok && item.Kind != "project" {
					id, err := planSecretaryProjectTx(ctx, tx, scope, project, aliases, projects, p)
					if err != nil {
						continue
					}
					item.ProjectID = id
				}
				if text, ok := textField(a.Set, "notesAppend"); ok && strings.TrimSpace(text) != "" {
					switch item.Kind {
					case "task":
						item.Notes += "\n" + text
					case "idea":
						item.Body += "\n" + text
					case "project":
						item.Goal += "\n" + text
					}
				}
				for alias, previous := range aliases {
					if previous.ID == item.ID {
						aliases[alias] = item
					}
				}
			case "delegate":
				if !oneOf(a.Kind, "plan", "draft", "breakdown", "summary", "ask") || strings.TrimSpace(a.Prompt) == "" {
					continue
				}
				item, ok := aliases[a.Ref]
				if a.Ref == "new" && validDeskTitle(a.Title) {
					p.ItemID = string(memory.NewID())
					item = newItem("task", strings.TrimSpace(a.Title))
					item.ID, item.Notes = p.ItemID, a.Prompt
					ok = true
				}
				if ok {
					p.TargetID = item.ID
					prospective[i] = item
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range plans {
		item, ok := prospective[i]
		if !ok {
			continue
		}
		a := actions[i]
		command := workspace.Command{Type: "requestRun", ThingID: item.ID, AgentID: c.Agent.ID, Kind: a.Kind, Prompt: a.Prompt}
		if a.Ref == "new" {
			command.Type, command.ID, command.Title = "delegateTask", item.ID, item.Title
		}
		prepCtx := context.WithValue(ctx, secretaryArtifactKey{}, secretaryArtifactContext{Task: c.Task, Dependencies: c.TypedDependencies})
		preparedCtx, err := s.prepareRunContextForItem(prepCtx, scope, command, &item, c.TypedDependencies)
		plans[i].Error = err
		if err == nil {
			prepared, ok := preparedCtx.Value(runContextKey{}).(preparedRunContext)
			if !ok {
				return nil, memory.ErrConflict
			}
			plans[i].Prepared = &prepared
		}
	}
	return plans, nil
}

func planSecretaryProjectTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, value string, aliases map[string]workspace.Item, projects map[string]string, plan *secretaryActionPlan) (string, error) {
	if value == "" || value == "none" {
		return "", nil
	}
	if item, ok := aliases[value]; ok && item.Kind == "project" {
		return item.ID, nil
	}
	if !strings.HasPrefix(value, "new:") {
		return "", memory.ErrInvalid
	}
	name := strings.TrimSpace(strings.TrimPrefix(value, "new:"))
	if !validDeskTitle(name) {
		return "", memory.ErrInvalid
	}
	var id string
	err := tx.QueryRow(ctx, "SELECT id::text FROM work_items WHERE owner_id=$1 AND kind='project' AND lower(btrim(title))=lower($2) ORDER BY created_at,id LIMIT 1", string(scope.OwnerID), name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id = projects[strings.ToLower(name)]
		if id == "" {
			id = string(memory.NewID())
			projects[strings.ToLower(name)] = id
		}
		plan.Projects[strings.ToLower(name)] = id
		return id, nil
	}
	return id, err
}
