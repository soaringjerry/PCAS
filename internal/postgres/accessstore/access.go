// Package accessstore is the storage of the access checks of an agent: what a
// run may still use, which sources a team reader sees, and what an item shows
// to an agent.
package accessstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Promoting an idea copies its body into the task's notes. Keep the same run
// dependencies on that new field, even if the owner already edited the body.
func PromoteArtifactsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ideaID, taskID string) error {
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
func SanitizeItemTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, principal string, item workspace.Item) (workspace.Item, []memory.Ref, error) {
	// Older promotions retained ideaId but not artifact links. Repair that
	// lineage before using their copied notes in a new provider request.
	if item.Kind == "task" && item.IdeaID != "" {
		if err := PromoteArtifactsTx(ctx, tx, scope, item.IdeaID, item.ID); err != nil {
			return item, nil, err
		}
	}
	dependencies := []memory.Ref{}
	type fieldBlocks struct {
		Field  string                    `json:"field"`
		Blocks []itemstore.ArtifactBlock `json:"blocks"`
	}
	fields, err := kernel.QueryDocuments[fieldBlocks](ctx, tx, "SELECT jsonb_build_object('field',field,'blocks',blocks) FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID)
	if err != nil {
		return item, nil, err
	}
	owned := map[string][]itemstore.ArtifactBlock{}
	for _, f := range fields {
		owned[f.Field] = f.Blocks
	}
	projectID := item.ProjectID
	if item.Kind == "project" {
		projectID = item.ID
	}
	// Resolve current versions only for this item's actual dependencies, once
	// for the statement. Materializing every claim here repeated a user-wide
	// scan for each adopted item and each before/after permission check.
	// Source dependencies must still resolve their evidenced claims separately:
	// those claim identities are not direct run dependencies in this CTE.
	const applicable = "applicable_claim_versions($1,now(),now())"
	rows, err := tx.Query(ctx, "WITH applicable AS MATERIALIZED (SELECT claim_id,version FROM "+applicable+" WHERE claim_id=ANY(ARRAY(SELECT d.memory_id FROM run_dependencies d JOIN adopted_artifacts adopted ON (adopted.owner_id,adopted.run_id)=(d.owner_id,d.run_id) WHERE adopted.owner_id=$1 AND adopted.thing_id=$2))) "+`SELECT a.kind,a.artifact_id,a.run_id::text, NOT EXISTS(
        SELECT 1 FROM run_dependencies d LEFT JOIN memory_records r ON (r.owner_id,r.id)=(d.owner_id,d.memory_id)
        WHERE (d.owner_id,d.run_id)=(a.owner_id,a.run_id) AND (r.state IS DISTINCT FROM 'active'
        OR EXISTS(SELECT 1 FROM context_exclusions x WHERE x.owner_id=d.owner_id AND x.thing_id=$2 AND x.memory_id=d.memory_id)
        OR CASE WHEN r.kind='source' THEN d.memory_version IS DISTINCT FROM r.version
        OR NOT (`+TeamSourceVisibleSQL("$1", "d.memory_id", "$3", "$2")+`) ELSE (
        d.memory_version IS DISTINCT FROM (SELECT v.version FROM applicable v WHERE v.claim_id=d.memory_id)
        OR NOT (`+memoryread.DependencyMemorySQL("d.owner_id", "d.memory_id")+`)
        OR NOT EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=d.owner_id AND g.record_id=d.memory_id AND g.principal_id=$3)
        OR NOT EXISTS(SELECT 1 FROM workspace_agents ag JOIN claim_revisions c ON c.owner_id=ag.owner_id WHERE ag.owner_id=$1 AND ag.id=$3 AND c.claim_id=d.memory_id AND c.version=d.memory_version AND ag.document->'memoryKinds' ? c.nature AND (coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=$4) AND (`+memoryread.HumanMemorySQL("c")+` OR (ag.document->>'includeInferred')::boolean))
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
			kept := []itemstore.ArtifactBlock{}
			for _, block := range blocks {
				dependent := kernel.OneOf(runID, block.Runs...)
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
				item.Title = workspace.ReauthorizeTitle
			}
		}
		if used && allowed {
			dependencies = append(dependencies, refs...)
		}
	}
	for field, blocks := range owned {
		text := itemstore.BlockText(blocks)
		if field == "title" && text == "" {
			text = workspace.ReauthorizeTitle
		}
		itemstore.SetField(&item, field, text)
	}
	return item, dependencies, rows.Err()
}

// teamSourceVisibleSQL follows explicit visibility and item exclusions on any
// currently applicable claim evidenced by the source. Its arguments are trusted
// SQL expressions, never request values; category and project filters do not apply.
// Keep the temporal lookup correlated to this source's evidence targets.
// Pulling it into a user-wide join repeated all current claims for each window
// and each source dependency verification. OFFSET 0 preserves the bounded plan.
func TeamSourceVisibleSQL(ownerID, sourceID, principalID, thingID string) string {
	return fmt.Sprintf(`NOT EXISTS (
 SELECT 1 FROM evidence source_evidence
 JOIN LATERAL (
 SELECT current_claim.claim_id,current_claim.version
 FROM applicable_claim_versions(%[1]s,now(),now()) current_claim
 WHERE current_claim.claim_id=source_evidence.target_id OFFSET 0
 ) source_claim ON source_claim.version=source_evidence.target_version
 WHERE source_evidence.owner_id=%[1]s AND source_evidence.source_id=%[2]s
 AND (NOT EXISTS (SELECT 1 FROM record_grants source_grant
  WHERE source_grant.owner_id=source_evidence.owner_id AND source_grant.record_id=source_evidence.target_id AND source_grant.principal_id=%[3]s)
 OR EXISTS (SELECT 1 FROM context_exclusions source_exclusion
  WHERE source_exclusion.owner_id=source_evidence.owner_id AND source_exclusion.memory_id=source_evidence.target_id AND source_exclusion.thing_id=%[4]s::uuid))
)`, ownerID, sourceID, principalID, thingID)
}

func VerifyRunTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run) error {
	var item *workspace.Item
	if run.ThingID != "" {
		current, err := itemstore.GetItem(ctx, tx, scope, run.ThingID)
		if err != nil {
			return err
		}
		item = &current
	}
	return VerifyRunForItemTx(ctx, tx, scope, run, item)
}

// Derived answers obey the destination item's current context policy too.
// Checking only the agent grant would let a previous output reintroduce an
// excluded memory, or a memory from the item's former project.
func VerifyRunForItemTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run, item *workspace.Item) error {
	agent, err := kernel.QueryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.AgentID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	var projectID string
	var excluded []string
	if item != nil {
		projectID = item.ProjectID
		if item.Kind == "project" {
			projectID = item.ID
		}
		excluded, err = kernel.QueryDocuments[string](ctx, tx, "SELECT to_jsonb(memory_id::text) FROM context_exclusions WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID)
		if err != nil {
			return err
		}
	}
	claimIDs, sourceIDs := []string{}, []string{}
	for _, ref := range kernel.UniqueRefs(run.ContextVersions) {
		if ref.Kind == memory.SourceKind {
			sourceIDs = append(sourceIDs, string(ref.ID))
		} else {
			claimIDs = append(claimIDs, string(ref.ID))
		}
	}
	statuses, err := memoryread.ClaimStatusesTx(ctx, tx, scope.OwnerID, claimIDs)
	if err != nil {
		return err
	}
	type dependency struct {
		ID                  string
		Version             int
		Nature, Acquisition string
		Scope               map[string]json.RawMessage
	}
	claims := map[string]dependency{}
	if len(claimIDs) > 0 {
		// Resolve applicable versions once. Preserve the original readClaim grant,
		// revision, nature, inference, and project checks for every supplied identity.
		rows, err := kernel.QueryDocuments[dependency](ctx, tx, `WITH applicable AS MATERIALIZED (
 SELECT claim_id,version FROM applicable_claim_versions($1,now(),now()) WHERE claim_id=ANY($2::uuid[])
) SELECT jsonb_build_object('id',r.id,'version',c.version,'nature',c.nature,'acquisition',c.acquisition,'scope',c.scope)
 FROM applicable a JOIN memory_records r ON r.owner_id=$1 AND r.id=a.claim_id
 JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,a.version)
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
 WHERE r.state='active' AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$3)`, string(scope.OwnerID), claimIDs, run.AgentID)
		if err != nil {
			return err
		}
		for _, claim := range rows {
			claims[claim.ID] = claim
		}
	}
	sources := map[string]int{}
	if len(sourceIDs) > 0 {
		var thingID *string
		if item != nil && item.ID != "" {
			thingID = &item.ID
		}
		rows, err := kernel.QueryDocuments[dependency](ctx, tx, `SELECT jsonb_build_object('id',r.id,'version',r.version)
 FROM memory_records r JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=$1 AND r.id=ANY($2::uuid[]) AND r.kind='source' AND r.state='active' AND v.state='active' AND `+TeamSourceVisibleSQL("$1", "r.id", "$3", "$4"), string(scope.OwnerID), sourceIDs, run.AgentID, thingID)
		if err != nil {
			return err
		}
		for _, source := range rows {
			sources[source.ID] = source.Version
		}
	}
	for _, ref := range kernel.UniqueRefs(run.ContextVersions) {
		if ref.Kind == memory.SourceKind {
			version, ok := sources[string(ref.ID)]
			if !ok || version != ref.Version {
				return memory.ErrConflict
			}
			continue
		}
		if !memoryread.RetirementDependencyAllowed(ctx, statuses[string(ref.ID)]) {
			return memory.ErrConflict
		}
		claim, ok := claims[string(ref.ID)]
		if !ok || claim.Version != ref.Version || !kernel.OneOf(claim.Nature, agent.MemoryKinds...) || !agent.IncludeInferred && (claim.Acquisition == "inferred" || statuses[string(ref.ID)].AI) {
			return memory.ErrConflict
		}
		if item != nil {
			var claimProject string
			if raw, ok := claim.Scope["project_id"]; ok && modelcall.DecodeStrict(raw, &claimProject) != nil {
				return memory.ErrConflict
			}
			if kernel.OneOf(string(ref.ID), excluded...) || claimProject != "" && claimProject != projectID {
				return memory.ErrConflict
			}
		}
	}
	return nil
}

// A version change alone makes an answer outdated. Recheck every dependency
// against its current version before deciding whether old content is safe to
// retain; a removed grant or deleted dependency must dominate any correction.
func VerifyRunAccessTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run workspace.Run) error {
	ctx = context.WithValue(ctx, memoryread.RetirementAccessKey{}, true)
	run.ContextVersions = append([]memory.Ref(nil), run.ContextVersions...)
	ids := make([]string, len(run.ContextVersions))
	for i, ref := range run.ContextVersions {
		ids[i] = string(ref.ID)
	}
	rows, err := kernel.QueryDocuments[struct {
		ID      string
		Version int
	}](ctx, tx, `SELECT jsonb_build_object('id',id,'version',version) FROM memory_records WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND state='active'`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	versions := map[string]int{}
	for _, row := range rows {
		versions[row.ID] = row.Version
	}
	for i, ref := range run.ContextVersions {
		version, ok := versions[string(ref.ID)]
		if !ok {
			return memory.ErrConflict
		}
		run.ContextVersions[i].Version = version
	}
	return VerifyRunTx(ctx, tx, scope, run)
}
