package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func (s *Store) Commit(ctx context.Context, scope memory.Scope, in memory.CommitRequest) ([]memory.Ref, error) {
	refs := []memory.Ref{}
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	if !in.RequestID.Valid() || len(in.Entities)+len(in.Episodes)+len(in.Claims)+len(in.Relations) == 0 || len(in.Entities)+len(in.Episodes)+len(in.Claims)+len(in.Relations) > 200 || len(in.Evidence) > 500 {
		return nil, memory.ErrInvalid
	}
	hash := sha256.Sum256(asJSON(in))
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		var prior, data []byte
		err := tx.QueryRow(ctx, "SELECT request_hash,refs FROM memory_commits WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), string(in.RequestID)).Scan(&prior, &data)
		if err == nil {
			if !bytes.Equal(prior, hash[:]) {
				return memory.ErrConflict
			}
			return json.Unmarshal(data, &refs)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		newRefs := map[memory.ID]memory.Ref{}
		evidenced := map[memory.ID]bool{}
		for _, e := range in.Evidence {
			if !e.Source.ID.Valid() || e.Source.Version < 1 || !e.Target.ID.Valid() || e.Target.Version != 1 || !oneOf(e.Acquisition, "direct", "reported", "inferred", "execution") || !oneOf(e.Stance, "supports", "refutes") {
				return memory.ErrInvalid
			}
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM source_versions v JOIN memory_records r ON (r.owner_id,r.id)=(v.owner_id,v.source_id) WHERE v.owner_id=$1 AND v.source_id=$2 AND v.version=$3 AND r.state='active')", string(scope.OwnerID), string(e.Source.ID), e.Source.Version).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return memory.ErrNotFound
			}
			evidenced[e.Target.ID] = true
		}
		add := func(v memory.Revision, kind memory.Kind) error {
			if !v.ID.Valid() || v.Version != 1 || newRefs[v.ID].ID != "" {
				return memory.ErrInvalid
			}
			if kind != memory.EntityKind && !evidenced[v.ID] {
				return memory.ErrInvalid
			}
			idHash := sha256.Sum256([]byte(v.ID))
			var blocked bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM record_reimport_blocks WHERE owner_id=$1 AND id_hash=$2)", string(scope.OwnerID), idHash[:]).Scan(&blocked); err != nil {
				return err
			}
			if blocked {
				return memory.ErrBlocked
			}
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_records WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), string(v.ID)).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return memory.ErrConflict
			}
			if err := createRecord(ctx, tx, scope.OwnerID, v.ID, kind, "user"); err != nil {
				return err
			}
			precision := v.ValidTime.Precision
			if precision == "" {
				precision = "unknown"
			}
			if !oneOf(precision, "unknown", "instant", "day", "month", "year", "range") || v.ValidTime.From != nil && v.ValidTime.To != nil && v.ValidTime.From.After(*v.ValidTime.To) {
				return memory.ErrInvalid
			}
			if _, err := tx.Exec(ctx, "UPDATE record_versions SET valid_from=$3,valid_to=$4,time_precision=$5,expressed_at=$6 WHERE owner_id=$1 AND record_id=$2 AND version=1", string(scope.OwnerID), string(v.ID), v.ValidTime.From, v.ValidTime.To, precision, v.ExpressedAt); err != nil {
				return err
			}
			ref := memory.Ref{ID: v.ID, Version: 1, Kind: kind}
			newRefs[v.ID] = ref
			refs = append(refs, ref)
			return nil
		}
		for _, e := range in.Entities {
			if requireText(e.Name) != nil || e.Type == "" {
				return memory.ErrInvalid
			}
			if err := add(e.Revision, memory.EntityKind); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO entities(owner_id,id) VALUES($1,$2)", string(scope.OwnerID), string(e.ID)); err != nil {
				return err
			}
			if e.Disambiguation == nil {
				e.Disambiguation = map[string]json.RawMessage{}
			}
			if _, err := tx.Exec(ctx, "INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name,disambiguation) VALUES($1,$2,1,$3,$4,$5)", string(scope.OwnerID), string(e.ID), e.Type, e.Name, asJSON(e.Disambiguation)); err != nil {
				return err
			}
			for _, alias := range append(e.Aliases, e.Name) {
				if requireText(alias) != nil {
					return memory.ErrInvalid
				}
				if _, err := tx.Exec(ctx, "INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3) ON CONFLICT DO NOTHING", string(scope.OwnerID), string(e.ID), alias); err != nil {
					return err
				}
			}
		}
		for _, c := range in.Claims {
			if !c.SubjectID.Valid() || requireText(c.Predicate) != nil || !json.Valid(c.Value) || !oneOf(c.Nature, "fact", "preference", "decision", "intention", "plan") || !oneOf(c.Acquisition, "direct", "reported", "inferred", "execution") || !oneOf(c.Confirmation, "unknown", "candidate", "adopted", "confirmed", "disputed") {
				return memory.ErrInvalid
			}
			if c.Scope == nil {
				c.Scope = map[string]json.RawMessage{}
			}
			if err := add(c.Revision, memory.ClaimKind); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO claims(owner_id,id) VALUES($1,$2)", string(scope.OwnerID), string(c.ID)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,scope,nature,acquisition,confirmation,change_type) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,'initial')", string(scope.OwnerID), string(c.ID), string(c.SubjectID), c.Predicate, c.Value, asJSON(c.Scope), c.Nature, c.Acquisition, c.Confirmation); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO activity(owner_id,record_id,last_effective_use_at) VALUES($1,$2,least(now(),coalesce($3,now())))", string(scope.OwnerID), string(c.ID), c.ExpressedAt); err != nil {
				return err
			}
		}
		for _, e := range in.Episodes {
			if requireText(e.Title) != nil {
				return memory.ErrInvalid
			}
			if err := add(e.Revision, memory.EpisodeKind); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO episodes(owner_id,id,version,title) VALUES($1,$2,1,$3)", string(scope.OwnerID), string(e.ID), e.Title); err != nil {
				return err
			}
			for _, member := range e.Members {
				if !member.ID.Valid() || member.Version < 1 {
					return memory.ErrInvalid
				}
				if _, err := tx.Exec(ctx, "INSERT INTO episode_members(owner_id,episode_id,episode_version,member_id,member_version) VALUES($1,$2,1,$3,$4) ON CONFLICT DO NOTHING", string(scope.OwnerID), string(e.ID), string(member.ID), member.Version); err != nil {
					return err
				}
			}
		}
		for _, r := range in.Relations {
			if !r.From.ID.Valid() || !r.To.ID.Valid() || r.From.Version < 1 || r.To.Version < 1 || !oneOf(r.Type, "belongs_to", "depends_on", "causes", "follows", "replaces", "corrects") {
				return memory.ErrInvalid
			}
			if err := add(r.Revision, memory.RelationKind); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO relations(owner_id,id,version,from_id,from_version,to_id,to_version,relation_type) VALUES($1,$2,1,$3,$4,$5,$6,$7)", string(scope.OwnerID), string(r.ID), string(r.From.ID), r.From.Version, string(r.To.ID), r.To.Version, r.Type); err != nil {
				return err
			}
		}
		for _, e := range in.Evidence {
			if newRefs[e.Target.ID].ID == "" {
				return memory.ErrInvalid
			}
			id := e.ID
			if id == "" {
				id = memory.NewID()
			}
			if !id.Valid() {
				return memory.ErrInvalid
			}
			if e.Locator == nil {
				e.Locator = map[string]json.RawMessage{}
			}
			if _, err := tx.Exec(ctx, "INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", string(scope.OwnerID), string(id), string(e.Source.ID), e.Source.Version, string(e.Target.ID), e.Target.Version, asJSON(e.Locator), e.Acquisition, e.Stance); err != nil {
				return err
			}
		}
		for _, ref := range refs {
			if ref.Kind != memory.RelationKind {
				if err := enqueue(ctx, tx, scope.OwnerID, ref.ID, ref.Version, "memory.index"); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, "INSERT INTO memory_commits(owner_id,request_id,request_hash,refs) VALUES($1,$2,$3,$4)", string(scope.OwnerID), string(in.RequestID), hash[:], asJSON(refs)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
	return refs, err
}
