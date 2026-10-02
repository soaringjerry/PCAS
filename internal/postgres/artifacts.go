package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func artifactTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, runID, thingID, kind, id, body string) error {
	if err := adoptBlocksTx(ctx, tx, scope, runID, thingID, kind, body); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO adopted_artifacts(owner_id,run_id,thing_id,kind,artifact_id,body) VALUES($1,$2,$3,$4,$5,$6)", string(scope.OwnerID), runID, thingID, kind, id, body)
	return err
}

// Promoting an idea copies its body into the task's notes. Keep the same run
// dependencies on that new field, even if the owner already edited the body.
func promoteArtifactsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ideaID, taskID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO artifact_fields(owner_id,thing_id,field,blocks) SELECT owner_id,$3,'notes',blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field='body' ON CONFLICT DO NOTHING`, string(scope.OwnerID), ideaID, taskID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO adopted_artifacts(owner_id,run_id,thing_id,kind,artifact_id,body)
 SELECT owner_id,run_id,$3,'notes',artifact_id,body FROM adopted_artifacts
 WHERE owner_id=$1 AND thing_id=$2 AND kind='body' ON CONFLICT DO NOTHING`, string(scope.OwnerID), ideaID, taskID)
	return err
}

// An adopted result is still derived from its inputs. Excluding an input or
// revoking its grant must also exclude copies embedded in later work briefs.
// New fields carry ownership blocks through edits and promotions. Pre-block
// fields are conservatively protected until ownership can be reviewed.
func sanitizeItemTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, principal string, item workspace.Item) (workspace.Item, []memory.Ref, error) {
	// Older promotions retained ideaId but not artifact links. Repair that
	// lineage before using their copied notes in a new provider request.
	if item.Kind == "task" && item.IdeaID != "" {
		if err := promoteArtifactsTx(ctx, tx, scope, item.IdeaID, item.ID); err != nil {
			return item, nil, err
		}
	}
	dependencies := []memory.Ref{}
	type fieldBlocks struct {
		Field  string          `json:"field"`
		Blocks []artifactBlock `json:"blocks"`
	}
	fields, err := queryDocuments[fieldBlocks](ctx, tx, "SELECT jsonb_build_object('field',field,'blocks',blocks) FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID)
	if err != nil {
		return item, nil, err
	}
	owned := map[string][]artifactBlock{}
	for _, f := range fields {
		owned[f.Field] = f.Blocks
	}
	projectID := item.ProjectID
	if item.Kind == "project" {
		projectID = item.ID
	}
	rows, err := tx.Query(ctx, `SELECT a.kind,a.artifact_id,a.run_id::text, NOT EXISTS(
        SELECT 1 FROM run_dependencies d LEFT JOIN memory_records r ON (r.owner_id,r.id)=(d.owner_id,d.memory_id)
        WHERE (d.owner_id,d.run_id)=(a.owner_id,a.run_id) AND (r.state IS DISTINCT FROM 'active'
        OR EXISTS(SELECT 1 FROM context_exclusions x WHERE x.owner_id=d.owner_id AND x.thing_id=$2 AND x.memory_id=d.memory_id)
        OR CASE WHEN r.kind='source' THEN d.memory_version IS DISTINCT FROM r.version
        OR NOT (`+teamSourceVisibleSQL("$1", "d.memory_id", "$3", "$2")+`) ELSE (
        d.memory_version IS DISTINCT FROM (SELECT v.version FROM applicable_claim_versions($1,now(),now()) v WHERE v.claim_id=d.memory_id)
        OR NOT EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=d.owner_id AND g.record_id=d.memory_id AND g.principal_id=$3)
        OR NOT EXISTS(SELECT 1 FROM workspace_agents ag JOIN claim_revisions c ON c.owner_id=ag.owner_id WHERE ag.owner_id=$1 AND ag.id=$3 AND c.claim_id=d.memory_id AND c.version=d.memory_version AND ag.document->'memoryKinds' ? c.nature AND (coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=$4) AND (c.confirmation='confirmed' OR (c.confirmation='adopted' AND c.acquisition='direct') OR (ag.document->>'includeInferred')::boolean))
        ) END
        )),coalesce((SELECT jsonb_agg(jsonb_build_object('id',d.memory_id,'version',d.memory_version,'kind',r.kind)) FROM run_dependencies d JOIN memory_records r ON (r.owner_id,r.id)=(d.owner_id,d.memory_id) WHERE (d.owner_id,d.run_id)=(a.owner_id,a.run_id)),'[]'::jsonb)
        FROM adopted_artifacts a WHERE a.owner_id=$1 AND a.thing_id=$2`, string(scope.OwnerID), item.ID, principal, projectID)
	if err != nil {
		return item, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id, runID string
		var allowed bool
		var refs []memory.Ref
		if err := rows.Scan(&kind, &id, &runID, &allowed, &refs); err != nil {
			return item, nil, err
		}
		used := false
		field := kind
		if kind == "task" {
			field = "title"
		}
		if blocks, ok := owned[field]; ok {
			kept := []artifactBlock{}
			for _, block := range blocks {
				dependent := oneOf(runID, block.Runs...)
				used = used || dependent
				if allowed || !dependent {
					kept = append(kept, block)
				}
			}
			owned[field] = kept
			if used && allowed {
				dependencies = append(dependencies, refs...)
			}
			continue
		}
		switch kind {
		case "notes":
			used = item.Notes != ""
			if !allowed {
				item.Notes = ""
			}
		case "body":
			used = item.Body != ""
			if !allowed {
				item.Body = ""
			}
		case "progress":
			used = item.Progress != ""
			if !allowed {
				item.Progress = ""
			}
		case "check":
			checks := []workspace.Check{}
			for _, check := range item.Checklist {
				if check.ID == id {
					used = true
				}
				if check.ID != id || allowed {
					checks = append(checks, check)
				}
			}
			item.Checklist = checks
		case "task":
			used = true
			if !allowed {
				item.Title = "事项内容需要重新授权或核验"
			}
		}
		if used && allowed {
			dependencies = append(dependencies, refs...)
		}
	}
	for field, blocks := range owned {
		text := blockText(blocks)
		if field == "title" && text == "" {
			text = "事项内容需要重新授权或核验"
		}
		setField(&item, field, text)
	}
	return item, dependencies, rows.Err()
}
func purgeArtifactsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ids []string) ([]string, error) {
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
		item, err := getItem(ctx, tx, scope, a.Thing)
		if err != nil {
			return nil, err
		}
		field := a.Kind
		if field == "task" {
			field = "title"
		}
		var blocks []artifactBlock
		blockErr := tx.QueryRow(ctx, "SELECT blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field=$3", string(scope.OwnerID), item.ID, field).Scan(&blocks)
		if blockErr == nil {
			kept := []artifactBlock{}
			for _, block := range blocks {
				if !oneOf(a.Run, block.Runs...) {
					kept = append(kept, block)
				}
			}
			text := blockText(kept)
			if field == "title" && text == "" {
				text = "已删除的生成内容"
				kept = []artifactBlock{{Text: text, Runs: []string{}}}
			}
			setField(&item, field, text)
			if err := saveBlocksTx(ctx, tx, scope, item.ID, field, kept); err != nil {
				return nil, err
			}
		} else if blockErr != pgx.ErrNoRows {
			return nil, blockErr
		} else if oneOf(field, "notes", "body", "progress", "title") {
			text := fieldText(item, field)
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
			setField(&item, field, text)
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
		item.UpdatedAt = stamp()
		if err := saveItem(ctx, tx, scope, item); err != nil {
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
