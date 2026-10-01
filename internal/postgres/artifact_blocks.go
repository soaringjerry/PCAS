package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type artifactBlock = workspace.TextBlock

func blockText(blocks []artifactBlock) string { return workspace.BlockText(blocks) }
func editBlocks(blocks []artifactBlock, text string) []artifactBlock {
	return workspace.EditBlocks(blocks, text)
}

// Dynamic fields use stable server IDs for checks/conditions/source versions.
// Append-only revision indexes are local to the owning item.
func itemArtifactText(item workspace.Item) map[string]string {
	out := map[string]string{"title": item.Title, "name": item.Name, "notes": item.Notes, "body": item.Body, "goal": item.Goal, "progress": item.Progress, "waitingFor": item.WaitingFor}
	if item.OwedTo != nil {
		out["owedTo"] = item.OwedTo.Who
	}
	for _, check := range item.Checklist {
		out["check:"+check.ID] = check.Text
	}
	for _, condition := range item.Conditions {
		out["condition:"+condition.ID] = condition.Description
	}
	for i, revision := range item.History {
		out[fmt.Sprintf("history:%d", i)] = revision.Summary
	}
	for i, revision := range item.Evolution {
		out[fmt.Sprintf("evolution:%d", i)] = revision.Summary
	}
	for _, source := range item.Sources {
		out[fmt.Sprintf("source:%s:%d", source.SourceID, source.Version)] = source.Label
	}
	return out
}
func fieldText(item workspace.Item, field string) string { return itemArtifactText(item)[field] }
func setField(item *workspace.Item, field, text string) {
	switch field {
	case "notes":
		item.Notes = text
	case "body":
		item.Body = text
	case "goal":
		item.Goal = text
	case "progress":
		item.Progress = text
	case "title":
		item.Title = text
	case "name":
		item.Name = text
	case "waitingFor":
		item.WaitingFor = text
	case "owedTo":
		if item.OwedTo != nil {
			item.OwedTo.Who = text
			if text == "" {
				item.OwedTo = nil
			}
		}
	default:
		if strings.HasPrefix(field, "check:") {
			id := strings.TrimPrefix(field, "check:")
			kept := []workspace.Check{}
			for _, check := range item.Checklist {
				if check.ID == id {
					if text == "" {
						continue
					}
					check.Text = text
				}
				kept = append(kept, check)
			}
			item.Checklist = kept
		} else if strings.HasPrefix(field, "condition:") {
			id := strings.TrimPrefix(field, "condition:")
			kept := []workspace.Condition{}
			for _, condition := range item.Conditions {
				if condition.ID == id {
					if text == "" {
						continue
					}
					condition.Description = text
				}
				kept = append(kept, condition)
			}
			item.Conditions = kept
		} else if strings.HasPrefix(field, "history:") || strings.HasPrefix(field, "evolution:") {
			pieces := strings.SplitN(field, ":", 2)
			i, err := strconv.Atoi(pieces[1])
			if err != nil || i < 0 {
				return
			}
			if pieces[0] == "history" && i < len(item.History) {
				item.History[i].Summary = text
			}
			if pieces[0] == "evolution" && i < len(item.Evolution) {
				item.Evolution[i].Summary = text
			}
		} else if strings.HasPrefix(field, "source:") {
			for i, source := range item.Sources {
				if fmt.Sprintf("source:%s:%d", source.SourceID, source.Version) == field {
					item.Sources[i].Label = text
				}
			}
		}
	}
}
func saveBlocksTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing, field string, blocks []artifactBlock) error {
	if err := recordActionBlocksBeforeTx(ctx, tx, scope, thing); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO artifact_fields(owner_id,thing_id,field,blocks) VALUES($1,$2,$3,$4) ON CONFLICT(owner_id,thing_id,field) DO UPDATE SET blocks=excluded.blocks", string(scope.OwnerID), thing, field, asJSON(blocks))
	return err
}

func syncArtifactEditsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item) error {
	type fieldBlocks struct {
		Field  string          `json:"field"`
		Blocks []artifactBlock `json:"blocks"`
	}
	fields, err := queryDocuments[fieldBlocks](ctx, tx, "SELECT jsonb_build_object('field',field,'blocks',blocks) FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID)
	if err != nil {
		return err
	}
	sources := []artifactBlock{}
	byField := map[string][]artifactBlock{}
	for _, f := range fields {
		sources = append(sources, f.Blocks...)
		byField[f.Field] = f.Blocks
	}
	for field, text := range itemArtifactText(item) {
		blocks, exists := byField[field]
		if exists {
			blocks = editBlocks(blocks, text)
		} else {
			blocks = []artifactBlock{{Text: text, Runs: []string{}}}
		}
		blocks = workspace.CopyOrigins(blocks, sources)
		derived := false
		for _, block := range blocks {
			derived = derived || len(block.Runs) > 0 || len(block.DeskActions) > 0
		}
		if exists || derived {
			if err := saveBlocksTx(ctx, tx, scope, item.ID, field, blocks); err != nil {
				return err
			}
		}
	}
	// Removed dynamic fields keep empty blocks, so an undo can preserve their
	// original identities without projecting old text into a reused array slot.
	for _, f := range fields {
		if _, exists := itemArtifactText(item)[f.Field]; !exists {
			if err := saveBlocksTx(ctx, tx, scope, item.ID, f.Field, []artifactBlock{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func adoptBlocksTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run, thing, kind, body string) error {
	field := kind
	if kind == "task" {
		field = "title"
	}
	if !oneOf(field, "notes", "body", "progress", "title") {
		return nil
	}
	item, err := getItem(ctx, tx, scope, thing)
	if err != nil {
		return err
	}
	var blocks []artifactBlock
	err = tx.QueryRow(ctx, "SELECT blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field=$3", string(scope.OwnerID), thing, field).Scan(&blocks)
	if errors.Is(err, pgx.ErrNoRows) {
		legacyRuns, err := queryDocuments[string](ctx, tx, "SELECT to_jsonb(run_id::text) FROM adopted_artifacts WHERE owner_id=$1 AND thing_id=$2 AND kind=$3", string(scope.OwnerID), thing, kind)
		if err != nil {
			return err
		}
		// A subsequent adoption must not relabel pre-block derived text as manual.
		blocks = []artifactBlock{{Text: fieldText(item, field), Runs: legacyRuns}}
	} else if err != nil {
		return err
	}
	if kind == "progress" || kind == "task" {
		blocks = []artifactBlock{}
	} else {
		blocks = append(blocks, artifactBlock{Text: "\n", Runs: []string{}})
	}
	blocks = append(blocks, artifactBlock{Text: body, Runs: []string{run}})
	if err := saveBlocksTx(ctx, tx, scope, thing, field, blocks); err != nil {
		return err
	}
	if kind == "task" {
		// newItem initialized both canonical fields from this Run's line.
		return saveBlocksTx(ctx, tx, scope, thing, "name", blocks)
	}
	return nil
}

// Owner-only recovery for ambiguous pre-block fields. These are deliberately
// excluded from Recall, run briefs and normal export.
func (s *Store) RetainedWriting(ctx context.Context, scope memory.Scope, thing string) ([]map[string]string, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	if !memory.ID(thing).Valid() {
		return nil, memory.ErrInvalid
	}
	var out []map[string]string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out, err = queryDocuments[map[string]string](ctx, tx, "SELECT jsonb_build_object('field',field,'text',body,'reason',reason) FROM retained_artifact_writing WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), thing)
		return err
	})
	return out, err
}
