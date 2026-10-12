package workstore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func ArtifactTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, runID, thingID, kind, id, body string) error {
	if err := itemstore.AdoptBlocksTx(ctx, tx, scope, runID, thingID, kind, body); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO adopted_artifacts(owner_id,run_id,thing_id,kind,artifact_id,body) VALUES($1,$2,$3,$4,$5,$6)", string(scope.OwnerID), runID, thingID, kind, id, body)
	return err
}

func PurgeArtifactsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ids []string) ([]string, error) {
	// Deletion must also cover pre-fix promotions that have not been used in
	// a new run (and therefore have not passed through the read repair above).
	if _, err := tx.Exec(ctx, `INSERT INTO adopted_artifacts(owner_id,run_id,thing_id,kind,artifact_id,body)
 SELECT a.owner_id,a.run_id,t.id,'notes',a.artifact_id,a.body
 FROM adopted_artifacts a JOIN work_items t ON t.owner_id=a.owner_id AND t.document->>'ideaId'=a.thing_id::text
 WHERE a.owner_id=$1 AND a.kind='body' AND t.kind='task' ON CONFLICT DO NOTHING`, string(scope.OwnerID)); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT thing_id::text,kind,artifact_id,run_id::text,body FROM adopted_artifacts WHERE owner_id=$1 AND run_id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`, string(scope.OwnerID), ids)
	if err != nil {
		return nil, err
	}
	type artifact struct{ Thing, Kind, ID, Run, Body string }
	artifacts := []artifact{}
	for rows.Next() {
		var a artifact
		if err := rows.Scan(&a.Thing, &a.Kind, &a.ID, &a.Run, &a.Body); err != nil {
			rows.Close()
			return nil, err
		}
		artifacts = append(artifacts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	things := []string{}
	for _, a := range artifacts {
		item, err := itemstore.GetItem(ctx, tx, scope, a.Thing)
		if err != nil {
			return nil, err
		}
		field := a.Kind
		if field == "task" {
			field = "title"
		}
		var blocks []itemstore.ArtifactBlock
		blockErr := tx.QueryRow(ctx, "SELECT blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field=$3", string(scope.OwnerID), item.ID, field).Scan(&blocks)
		if blockErr == nil {
			kept := []itemstore.ArtifactBlock{}
			for _, block := range blocks {
				if !kernel.OneOf(a.Run, block.Runs...) {
					kept = append(kept, block)
				}
			}
			text := itemstore.BlockText(kept)
			if field == "title" && text == "" {
				text = "已删除的生成内容"
				kept = []itemstore.ArtifactBlock{{Text: text, Runs: []string{}}}
			}
			itemstore.SetField(&item, field, text)
			if err := itemstore.SaveBlocksTx(ctx, tx, scope, item.ID, field, kept); err != nil {
				return nil, err
			}
		} else if blockErr != pgx.ErrNoRows {
			return nil, blockErr
		} else if kernel.OneOf(field, "notes", "body", "progress", "title") {
			text := itemstore.FieldText(item, field)
			if text != "" && text != a.Body && text != "\n"+a.Body {
				// Ownership cannot be reconstructed after historical freeform edits.
				// Quarantine for explicit owner review; never feed it to a model.
				if _, err := tx.Exec(ctx, "INSERT INTO retained_artifact_writing(owner_id,thing_id,field,body,reason) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", string(scope.OwnerID), item.ID, field, text, "旧版文字混有生成内容，已隔离；请检查并单独保存自己的文字。"); err != nil {
					return nil, err
				}
				item.HasRetainedWriting = true
			}
			text = ""
			if field == "title" {
				text = "已删除的生成内容"
			}
			itemstore.SetField(&item, field, text)
		} else if a.Kind == "check" {
			kept := []workspace.Check{}
			for _, check := range item.Checklist {
				if check.ID != a.ID {
					kept = append(kept, check)
				}
			}
			item.Checklist = kept
		}
		item.Version++
		item.UpdatedAt = kernel.Stamp()
		if err := itemstore.SaveItem(ctx, tx, scope, item); err != nil {
			return nil, err
		}
		things = append(things, item.ID)
	}
	rows, err = tx.Query(ctx, `SELECT id::text FROM sources WHERE owner_id=$1 AND ((connector='actions' AND external_id=ANY($2::text[])) OR (connector='corrections' AND external_id=ANY($3::text[])))`, string(scope.OwnerID), things, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
