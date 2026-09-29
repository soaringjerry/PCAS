package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func readRevision(ctx context.Context, tx pgx.Tx, scope memory.Scope, ref memory.Ref) (memory.Revision, error) {
	v := memory.Revision{Ref: ref}
	err := tx.QueryRow(ctx, `SELECT valid_from,valid_to,time_precision,expressed_at,recorded_at,state FROM record_versions WHERE owner_id=$1 AND record_id=$2 AND version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&v.ValidTime.From, &v.ValidTime.To, &v.ValidTime.Precision, &v.ExpressedAt, &v.RecordedAt, &v.State)
	return v, err
}
func readEntity(ctx context.Context, tx pgx.Tx, scope memory.Scope, ref memory.Ref) (memory.Entity, error) {
	e := memory.Entity{Aliases: []string{}}
	var err error
	e.Revision, err = readRevision(ctx, tx, scope, ref)
	if err != nil {
		return e, err
	}
	err = tx.QueryRow(ctx, `SELECT entity_type,name,disambiguation FROM entity_versions WHERE owner_id=$1 AND entity_id=$2 AND version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&e.Type, &e.Name, &e.Disambiguation)
	if err != nil {
		return e, err
	}
	rows, err := tx.Query(ctx, `SELECT alias FROM aliases WHERE owner_id=$1 AND entity_id=$2 AND entity_version=$3 ORDER BY alias`, string(scope.OwnerID), string(ref.ID), ref.Version)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return e, err
		}
		e.Aliases = append(e.Aliases, alias)
	}
	return e, rows.Err()
}
func readEpisode(ctx context.Context, tx pgx.Tx, scope memory.Scope, ref memory.Ref) (memory.Episode, error) {
	e := memory.Episode{Members: []memory.Ref{}}
	var err error
	e.Revision, err = readRevision(ctx, tx, scope, ref)
	if err != nil {
		return e, err
	}
	err = tx.QueryRow(ctx, `SELECT title FROM episodes WHERE owner_id=$1 AND id=$2 AND version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&e.Title)
	if err != nil {
		return e, err
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,m.member_version,r.kind FROM episode_members m JOIN memory_records r ON(r.owner_id,r.id)=(m.owner_id,m.member_id) JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(m.owner_id,m.member_id,m.member_version) WHERE m.owner_id=$1 AND m.episode_id=$2 AND m.episode_version=$3 AND r.state='active' AND rv.state='active' AND ($4 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$5)) ORDER BY r.id,m.member_version`, string(scope.OwnerID), string(ref.ID), ref.Version, scope.IsOwner, scope.PrincipalID)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var member memory.Ref
		if err := rows.Scan(&member.ID, &member.Version, &member.Kind); err != nil {
			return e, err
		}
		e.Members = append(e.Members, member)
	}
	return e, rows.Err()
}
