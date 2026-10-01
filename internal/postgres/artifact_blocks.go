package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type artifactBlock = workspace.TextBlock

func blockText(blocks []artifactBlock) string { return workspace.BlockText(blocks) }
func editBlocks(blocks []artifactBlock, text string) []artifactBlock {
	return workspace.EditBlocks(blocks, text)
}
func fieldText(item workspace.Item, field string) string {
	switch field {
	case "notes":
		return item.Notes
	case "body":
		return item.Body
	case "progress":
		return item.Progress
	case "title":
		return item.Title
	}
	return ""
}
func setField(item *workspace.Item, field, text string) {
	switch field {
	case "notes":
		item.Notes = text
	case "body":
		item.Body = text
	case "progress":
		item.Progress = text
	case "title":
		item.Title = text
		item.Name = text
	}
}
func saveBlocksTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing, field string, blocks []artifactBlock) error {
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
	for _, f := range fields {
		if err := saveBlocksTx(ctx, tx, scope, item.ID, f.Field, editBlocks(f.Blocks, fieldText(item, f.Field))); err != nil {
			return err
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
	return saveBlocksTx(ctx, tx, scope, thing, field, blocks)
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
