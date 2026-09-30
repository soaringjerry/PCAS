package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type statement struct {
	Text         string
	Nature       string
	ProjectID    string
	Subject      string
	Predicate    string
	Confirmation string
	Acquisition  string
	Actor        string
	Quote        string
	Source       memory.Ref
}

func createRecord(ctx context.Context, tx pgx.Tx, owner memory.ID, id memory.ID, kind memory.Kind, actor string) error {
	if _, err := tx.Exec(ctx, "INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,1)", string(owner), string(id), string(kind)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO record_versions(owner_id,record_id,version,actor) VALUES($1,$2,1,$3)", string(owner), string(id), actor)
	return err
}

func (s *Store) rememberTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in statement) (memory.Ref, error) {
	var result memory.Ref
	if strings.TrimSpace(in.Text) == "" || len(in.Text) > 1<<20 {
		return result, memory.ErrInvalid
	}
	if !oneOf(in.Nature, "fact", "preference", "intention", "plan", "decision") {
		return result, memory.ErrInvalid
	}
	if in.Subject == "" {
		in.Subject = "未解析主体"
	}
	if in.Predicate == "" {
		in.Predicate = "描述"
	}
	if in.Actor == "" {
		in.Actor = "user"
	}
	if in.Confirmation == "" {
		in.Confirmation = "confirmed"
	}
	if in.Acquisition == "" {
		in.Acquisition = "direct"
		if in.Actor == "ai" {
			in.Acquisition = "inferred"
		}
	}
	if !oneOf(in.Acquisition, "direct", "reported", "inferred", "execution") {
		return result, memory.ErrInvalid
	}
	if !in.Source.ID.Valid() || in.Source.Version < 1 {
		return result, memory.ErrInvalid
	}
	var original string
	if err := tx.QueryRow(ctx, "SELECT body FROM source_versions WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), string(in.Source.ID), in.Source.Version).Scan(&original); err != nil {
		return result, err
	}
	start, end := 0, len([]rune(original))
	if in.Quote != "" {
		at := strings.Index(original, in.Quote)
		if at < 0 {
			return result, memory.ErrInvalid
		}
		start = len([]rune(original[:at]))
		end = start + len([]rune(in.Quote))
	}
	key := sha256.Sum256([]byte(in.Subject + "\x00" + in.Predicate + "\x00" + in.Text))
	var blocked bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM claim_reimport_blocks WHERE owner_id=$1 AND key_hash=$2)", string(scope.OwnerID), key[:]).Scan(&blocked); err != nil {
		return result, err
	}
	if blocked {
		return result, memory.ErrBlocked
	}
	var redirected string
	err := tx.QueryRow(ctx, "SELECT k.claim_id::text,r.version FROM claim_key_redirects k JOIN memory_records r ON (r.owner_id,r.id)=(k.owner_id,k.claim_id) WHERE k.owner_id=$1 AND k.key_hash=$2 AND r.state='active'", string(scope.OwnerID), key[:]).Scan(&redirected, &result.Version)
	if err == nil {
		result.ID = memory.ID(redirected)
		result.Kind = memory.ClaimKind
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	var prior string
	err = tx.QueryRow(ctx, `SELECT k.claim_id::text FROM claim_source_keys k WHERE k.owner_id=$1 AND k.source_id=$2 AND k.claim_key=$3`, string(scope.OwnerID), string(in.Source.ID), stringHex(key[:])).Scan(&prior)
	if err == nil {
		var version int
		if err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), prior).Scan(&version); err != nil {
			return result, err
		}
		// A repeated assertion in a newer source version is fresh evidence for
		// the same claim, not a second claim and not continued reliance on v1.
		locator := asJSON(map[string]int{"start_rune": start, "end_rune": end})
		if _, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance)
			SELECT $1,gen_random_uuid(),$2,$3,$4,$5,$6,$7,'supports'
			WHERE NOT EXISTS(SELECT 1 FROM evidence WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND target_id=$4 AND target_version=$5 AND stance='supports')`, string(scope.OwnerID), string(in.Source.ID), in.Source.Version, prior, version, locator, in.Acquisition); err != nil {
			return result, err
		}
		if err := invalidateTx(ctx, tx, scope, prior); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage)
			VALUES(gen_random_uuid(),$1,$2,$3,'memory.index') ON CONFLICT(owner_id,record_id,record_version,stage)
			DO UPDATE SET state='queued',attempts=0,available_at=now(),error_code='',lease_token=NULL,lease_until=NULL WHERE memory_jobs.state!='leased'`, string(scope.OwnerID), prior, version); err != nil {
			return result, err
		}
		return memory.Ref{ID: memory.ID(prior), Version: version, Kind: memory.ClaimKind}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	var subject string
	err = tx.QueryRow(ctx, `SELECT e.entity_id::text FROM entity_versions e JOIN memory_records r ON (r.owner_id,r.id,r.version)=(e.owner_id,e.entity_id,e.version)
		WHERE e.owner_id=$1 AND e.name=$2 AND e.disambiguation->>'source_id'=$3 AND r.state='active' ORDER BY r.created_at LIMIT 1`, string(scope.OwnerID), in.Subject, string(in.Source.ID)).Scan(&subject)
	if errors.Is(err, pgx.ErrNoRows) {
		subject = string(memory.NewID())
		if err := createRecord(ctx, tx, scope.OwnerID, memory.ID(subject), memory.EntityKind, in.Actor); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO entities(owner_id,id) VALUES($1,$2)", string(scope.OwnerID), subject); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name,disambiguation) VALUES($1,$2,1,'unknown',$3,$4)", string(scope.OwnerID), subject, in.Subject, asJSON(map[string]string{"source_id": string(in.Source.ID)})); err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)", string(scope.OwnerID), subject, in.Subject); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, err
	}
	id := memory.NewID()
	if err := createRecord(ctx, tx, scope.OwnerID, id, memory.ClaimKind, in.Actor); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO claims(owner_id,id) VALUES($1,$2)", string(scope.OwnerID), string(id)); err != nil {
		return result, err
	}
	value, _ := json.Marshal(in.Text)
	claimScope := map[string]string{}
	if in.ProjectID != "" {
		claimScope["project_id"] = in.ProjectID
	}
	scopeJSON, _ := json.Marshal(claimScope)
	acquisition := in.Acquisition
	if _, err := tx.Exec(ctx, `INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,scope,nature,acquisition,confirmation,change_type)
		VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,'initial')`, string(scope.OwnerID), string(id), subject, in.Predicate, value, scopeJSON, in.Nature, acquisition, in.Confirmation); err != nil {
		return result, err
	}
	locator, _ := json.Marshal(map[string]int{"start_rune": start, "end_rune": end})
	if _, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,1,$6,$7,'supports')`, string(scope.OwnerID), string(memory.NewID()), string(in.Source.ID), in.Source.Version, string(id), locator, acquisition); err != nil {
		return result, err
	}
	if in.Actor == "user" {
		receipt, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "memory-input", ExternalID: string(id), ExternalVersion: "1", Title: "用户确认的记忆", Text: in.Text, MediaType: "text/plain"})
		if err != nil {
			return result, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,1,'{}','direct','supports')", string(scope.OwnerID), string(memory.NewID()), string(receipt.ID), receipt.Version, string(id)); err != nil {
			return result, err
		}
	}
	if _, err := tx.Exec(ctx, "INSERT INTO activity(owner_id,record_id,last_effective_use_at) VALUES($1,$2,now())", string(scope.OwnerID), string(id)); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,$2,id FROM workspace_agents WHERE owner_id=$1 AND (document->>'enabled')::boolean`, string(scope.OwnerID), string(id)); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO claim_source_keys(owner_id,source_id,claim_key,claim_id) VALUES($1,$2,$3,$4)", string(scope.OwnerID), string(in.Source.ID), stringHex(key[:]), string(id)); err != nil {
		return result, err
	}
	if err := enqueue(ctx, tx, scope.OwnerID, id, 1, "memory.index"); err != nil {
		return result, err
	}
	if err := refreshSummaryJobsTx(ctx, tx, scope.OwnerID, in.Source.ID); err != nil {
		return result, err
	}
	return memory.Ref{ID: id, Version: 1, Kind: memory.ClaimKind}, nil
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func stringHex(value []byte) string {
	const chars = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2] = chars[b>>4]
		out[i*2+1] = chars[b&15]
	}
	return string(out)
}

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
