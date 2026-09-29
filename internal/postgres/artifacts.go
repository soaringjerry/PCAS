package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func artifactTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, runID, thingID, kind, id, body string) error {
	_, err := tx.Exec(ctx, "INSERT INTO adopted_artifacts(owner_id,run_id,thing_id,kind,artifact_id,body) VALUES($1,$2,$3,$4,$5,$6)", string(scope.OwnerID), runID, thingID, kind, id, body)
	return err
}

// An adopted result is still derived from its inputs. Excluding an input or
// revoking its grant must also exclude copies embedded in later work briefs.
func sanitizeItemTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, principal string, item workspace.Item) (workspace.Item, []memory.Ref, error) {
	dependencies := []memory.Ref{}
	rows, err := tx.Query(ctx, `SELECT a.kind,a.artifact_id,a.body, NOT EXISTS(
        SELECT 1 FROM run_dependencies d LEFT JOIN memory_records r ON (r.owner_id,r.id)=(d.owner_id,d.memory_id)
        WHERE (d.owner_id,d.run_id)=(a.owner_id,a.run_id) AND (r.state IS DISTINCT FROM 'active'
        OR d.memory_version IS DISTINCT FROM (SELECT v.version FROM applicable_claim_versions($1,now(),now()) v WHERE v.claim_id=d.memory_id)
        OR NOT EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=d.owner_id AND g.record_id=d.memory_id AND g.principal_id=$3)
        OR EXISTS(SELECT 1 FROM context_exclusions x WHERE x.owner_id=d.owner_id AND x.thing_id=$2 AND x.memory_id=d.memory_id)
        OR NOT EXISTS(SELECT 1 FROM workspace_agents ag JOIN claim_revisions c ON c.owner_id=ag.owner_id WHERE ag.owner_id=$1 AND ag.id=$3 AND c.claim_id=d.memory_id AND c.version=d.memory_version AND ag.document->'memoryKinds' ? c.nature AND (c.confirmation='confirmed' OR (ag.document->>'includeInferred')::boolean))
        )),coalesce((SELECT jsonb_agg(jsonb_build_object('id',d.memory_id,'version',d.memory_version,'kind','claim')) FROM run_dependencies d WHERE (d.owner_id,d.run_id)=(a.owner_id,a.run_id)),'[]'::jsonb)
        FROM adopted_artifacts a WHERE a.owner_id=$1 AND a.thing_id=$2`, string(scope.OwnerID), item.ID, principal)
	if err != nil {
		return item, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id, body string
		var allowed bool
		var refs []memory.Ref
		if err := rows.Scan(&kind, &id, &body, &allowed, &refs); err != nil {
			return item, nil, err
		}
		used := false
		switch kind {
		case "notes":
			used = body != "" && strings.Contains(item.Notes, body)
			if !allowed {
				item.Notes = strings.ReplaceAll(item.Notes, body, "")
			}
		case "body":
			used = body != "" && strings.Contains(item.Body, body)
			if !allowed {
				item.Body = strings.ReplaceAll(item.Body, body, "")
			}
		case "progress":
			used = body != "" && strings.Contains(item.Progress, body)
			if !allowed {
				item.Progress = strings.ReplaceAll(item.Progress, body, "")
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
				item.Notes = ""
				item.Body = ""
				item.Checklist = []workspace.Check{}
			}
		}
		if used && allowed {
			dependencies = append(dependencies, refs...)
		}
	}
	return item, dependencies, rows.Err()
}
func purgeArtifactsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ids []string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT thing_id::text,kind,artifact_id,body FROM adopted_artifacts WHERE owner_id=$1 AND run_id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`, string(scope.OwnerID), ids)
	if err != nil {
		return nil, err
	}
	type artifact struct{ Thing, Kind, ID, Body string }
	artifacts := []artifact{}
	for rows.Next() {
		var a artifact
		if err := rows.Scan(&a.Thing, &a.Kind, &a.ID, &a.Body); err != nil {
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
		switch a.Kind {
		case "notes":
			item.Notes = strings.ReplaceAll(item.Notes, a.Body, "")
		case "body":
			item.Body = strings.ReplaceAll(item.Body, a.Body, "")
		case "progress":
			item.Progress = strings.ReplaceAll(item.Progress, a.Body, "")
		case "task":
			item.Title = "已删除的生成内容"
			item.Name = item.Title
			item.Notes = ""
			item.Body = ""
			item.Status = "cancelled"
			item.Checklist = []workspace.Check{}
			item.History = []workspace.Revision{}
		case "check":
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
