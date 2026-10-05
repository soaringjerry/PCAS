package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Cards may contribute over a hundred dependencies. Hydrate and validate claims
// as one scoped set instead of issuing two round trips for each card item.
// Source visibility and duplicate redirection retain the canonical verifier.
func (s *Store) checkSecretaryUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c secretaryContext) error {
	if !c.Use.Ready {
		return s.checkDeskContextTx(ctx, tx, scope, c.Agent.ID, c.Dependencies, c.Items, true)
	}
	var at time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); err != nil {
		return err
	}
	tx = useClockTx{Tx: tx, at: at}
	agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.Agent.ID)
	if err != nil {
		return err
	}
	if !agent.Enabled {
		return memory.ErrForbidden
	}
	var item *workspace.Item
	if sent, ok := c.Aliases["THIS"]; ok {
		current, err := getItem(ctx, tx, scope, sent.ID)
		if err != nil {
			return err
		}
		item = &current
	}
	claimIDs := []string{}
	for _, ref := range c.Dependencies {
		if ref.Kind != memory.SourceKind {
			claimIDs = append(claimIDs, string(ref.ID))
		}
	}
	duplicates, err := queryDocuments[string](ctx, tx, "SELECT to_jsonb(id::text) FROM claims WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND retired='duplicate'", string(scope.OwnerID), claimIDs)
	if err != nil {
		return err
	}
	// Completion only needs version, access and policy fields. Rehydrating
	// evidence/mentions for every supplied card on each check is unnecessary.
	rows, err := tx.Query(ctx, `WITH applicable AS MATERIALIZED (
 SELECT claim_id,version FROM applicable_claim_versions($1,now(),now()) WHERE claim_id=ANY($2::uuid[]))
 SELECT c.claim_id::text,c.version,c.nature,c.acquisition,coalesce(c.scope->>'project_id','')
 FROM applicable a JOIN claim_revisions c ON c.owner_id=$1 AND (c.claim_id,c.version)=(a.claim_id,a.version)
 JOIN memory_records r ON (r.owner_id,r.id)=(c.owner_id,c.claim_id)
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
 JOIN claims active ON (active.owner_id,active.id)=(c.owner_id,c.claim_id)
 WHERE r.state='active' AND rv.state='active' AND active.retired='' AND claim_source_is_current($1,c.claim_id,c.version,now())
 AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=c.owner_id AND g.record_id=c.claim_id AND g.principal_id=$3)`, string(scope.OwnerID), claimIDs, agent.ID)
	if err != nil {
		return err
	}
	byID := map[string]workspace.Memory{}
	for rows.Next() {
		var m workspace.Memory
		if err := rows.Scan(&m.ID, &m.Version, &m.Kind, &m.Acquisition, &m.ProjectID); err != nil {
			rows.Close()
			return err
		}
		byID[m.ID] = m
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	excluded := []string{}
	project := ""
	if item != nil {
		project = item.ProjectID
		if item.Kind == "project" {
			project = item.ID
		}
		excluded, err = queryDocuments[string](ctx, tx, "SELECT to_jsonb(memory_id::text) FROM context_exclusions WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID)
		if err != nil {
			return err
		}
	}
	canonical := []memory.Ref{}
	for _, ref := range uniqueRefs(c.Dependencies) {
		if ref.Kind == memory.SourceKind || oneOf(string(ref.ID), duplicates...) {
			canonical = append(canonical, ref)
			continue
		}
		m, ok := byID[string(ref.ID)]
		if !ok || m.Version != ref.Version || !useMemoryAllowed(m, agent) {
			return memory.ErrConflict
		}
		if item != nil && (oneOf(m.ID, excluded...) || m.ProjectID != "" && m.ProjectID != project) {
			return memory.ErrConflict
		}
	}
	if err := verifyRunForItemTx(ctx, tx, scope, workspace.Run{AgentID: agent.ID, ContextVersions: canonical}, item); err != nil {
		return err
	}
	// Keep the original full document and adopted-artifact comparison for items.
	return s.checkDeskContextTx(ctx, tx, scope, agent.ID, nil, c.Items, true)
}

// PostgreSQL now() is frozen at transaction start. The ordered turn keeps its
// transaction across the model call, so completion checks must see revisions
// committed during generation. Only this read-only verifier uses wall time.
type useClockTx struct {
	pgx.Tx
	at time.Time
}

func (tx useClockTx) queryAt(sql string, args []any) (string, []any) {
	if !strings.Contains(sql, "now()") {
		return sql, args
	}
	args = append(append([]any{}, args...), tx.at)
	return strings.ReplaceAll(sql, "now()", fmt.Sprintf("$%d::timestamptz", len(args))), args
}
func (tx useClockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	sql, args = tx.queryAt(sql, args)
	return tx.Tx.Query(ctx, sql, args...)
}
func (tx useClockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	sql, args = tx.queryAt(sql, args)
	return tx.Tx.QueryRow(ctx, sql, args...)
}
