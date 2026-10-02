package postgres

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) memoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, effective ...bool) ([]workspace.Memory, error) {
	result := []workspace.Memory{}
	currentOnly := len(effective) > 0 && effective[0]
	rows, err := tx.Query(ctx, `SELECT r.id::text,c.version,c.nature,c.value #>> '{}',c.confirmation,c.acquisition,coalesce(c.scope->>'project_id',''),
		coalesce(a.last_effective_use_at,r.created_at),coalesce(a.stability,1),coalesce(a.half_life_seconds,2592000),coalesce(a.pinned,false),coalesce(a.reinforcement_limit,8)
		FROM memory_records r JOIN claim_revisions c ON (c.owner_id,c.claim_id)=(r.owner_id,r.id) AND c.version=CASE WHEN $4 THEN (SELECT v.version FROM applicable_claim_versions($1,now(),now()) v WHERE v.claim_id=r.id) ELSE r.version END
		LEFT JOIN activity a ON (a.owner_id,a.record_id)=(r.owner_id,r.id)
		WHERE r.owner_id=$1 AND r.state='active' AND claim_source_is_current($1,c.claim_id,c.version,now()) AND ($2 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$3))
		ORDER BY r.updated_at DESC,r.id`, string(scope.OwnerID), scope.IsOwner, scope.PrincipalID, currentOnly)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m workspace.Memory
		var last time.Time
		var stability, halfLife float64
		if err := rows.Scan(&m.ID, &m.Version, &m.Kind, &m.Text, &m.Confirmation, &m.Acquisition, &m.ProjectID, &last, &stability, &halfLife, &m.Pinned, &m.ReinforcementLimit); err != nil {
			rows.Close()
			return nil, err
		}
		m.Epistemic = "inferred"
		if m.Confirmation == "confirmed" {
			m.Epistemic = "confirmed"
		} else if m.Confirmation == "adopted" && m.Acquisition == "direct" {
			m.Epistemic = "sourced"
		}
		m.HalfLifeDays = halfLife / 86400
		m.LastUsedAt = last.UTC().Format(time.RFC3339Nano)
		m.Exposure = math.Exp2(-math.Max(0, time.Since(last).Seconds()) / (halfLife * stability))
		if m.Pinned {
			m.Exposure = 1
		}
		m.Sources = []workspace.SourceRef{}
		m.Versions = []workspace.MemoryVersion{}
		m.VisibleTo = []string{}
		result = append(result, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		m := &result[i]
		rows, err := tx.Query(ctx, `SELECT rv.recorded_at,rv.actor,c.value #>> '{}',c.reason FROM claim_revisions c JOIN record_versions rv
			ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version) WHERE c.owner_id=$1 AND c.claim_id=$2 ORDER BY c.version`, string(scope.OwnerID), m.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var v workspace.MemoryVersion
			var at time.Time
			if err := rows.Scan(&at, &v.By, &v.Text, &v.Reason); err != nil {
				rows.Close()
				return nil, err
			}
			v.At = at.UTC().Format(time.RFC3339Nano)
			m.Versions = append(m.Versions, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(ctx, "SELECT principal_id FROM record_grants WHERE owner_id=$1 AND record_id=$2 ORDER BY principal_id", string(scope.OwnerID), m.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			m.VisibleTo = append(m.VisibleTo, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(ctx, `SELECT DISTINCT v.source_id::text,v.version,v.title,left(v.body,400),rv.recorded_at FROM evidence e JOIN source_versions v
			ON (v.owner_id,v.source_id,v.version)=(e.owner_id,e.source_id,e.source_version) JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
			WHERE e.owner_id=$1 AND e.target_id=$2 AND ($3 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=v.owner_id AND g.record_id=v.source_id AND g.principal_id=$4))`, string(scope.OwnerID), m.ID, scope.IsOwner, scope.PrincipalID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r workspace.SourceRef
			var at time.Time
			if err := rows.Scan(&r.SourceID, &r.Version, &r.Label, &r.Excerpt, &at); err != nil {
				rows.Close()
				return nil, err
			}
			r.At = at.UTC().Format(time.RFC3339Nano)
			m.Sources = append(m.Sources, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
