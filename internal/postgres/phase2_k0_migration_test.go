package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// This fixture is independent of the migration. SQL below is an adapter for
// those frozen expected identities, versions, bodies and boundary values.
type phase2K0Gold struct {
	LegacyMax string `json:"legacy_max_migration"`
	Cases     []struct {
		ID               string   `json:"id"`
		BodyLimit        int      `json:"body_limit_bytes"`
		MetadataLimit    int      `json:"attempt_metadata_limit_bytes"`
		Accepted         []string `json:"accepted_kinds"`
		Rejected         []string `json:"rejected_kinds"`
		RejectedVersions []int    `json:"rejected_versions"`
		Records          []struct {
			ID             string `json:"id"`
			Kind           string `json:"kind"`
			Version        int    `json:"version"`
			Body           string `json:"body"`
			HistoricalBody string `json:"historical_body"`
		} `json:"dependency_records"`
	} `json:"cases"`
}

func phase2K0ReadGold(t *testing.T) phase2K0Gold {
	t.Helper()
	body, err := os.ReadFile("../../testdata/phase2/k0-migration-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold phase2K0Gold
	if err := json.Unmarshal(body, &gold); err != nil {
		t.Fatal(err)
	}
	if len(gold.Cases) != 7 || len(gold.Cases[1].Records) != 3 {
		t.Fatal("independent migration gold shape changed")
	}
	return gold
}

func phase2K0Evidence(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s=%s", name, body)
	if dir := os.Getenv("PCAS_PHASE2_EVIDENCE_DIR"); dir != "" {
		if !strings.HasPrefix(filepath.Clean(dir), "/tmp/pcas-phase2-c-") {
			t.Fatal("dedicated C evidence directory required")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := strings.ReplaceAll(t.Name(), "/", "__") + "-" + name + ".json"
		if err := os.WriteFile(filepath.Join(dir, file), append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// legacy creates an actual 001..021 schema without applying and removing 022.
// It never modifies an existing schema or an old workspace/database.
func phase2K0Store(t *testing.T, legacy bool) *Store {
	t.Helper()
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	u, err := url.Parse(dsn)
	if err != nil || dsn == "" || u.Hostname() != "127.0.0.1" || u.Path != "/phase2_c" {
		t.Fatal("K0 acceptance requires the dedicated loopback phase2_c database; no skip")
	}
	ctx := context.Background()
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	var encoding string
	if err := admin.pool.QueryRow(ctx, "SHOW server_encoding").Scan(&encoding); err != nil || encoding != "UTF8" {
		t.Fatalf("UTF8 required: %s %v", encoding, err)
	}
	if _, err := admin.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	schema := "pcas_phase2_c_" + strings.ReplaceAll(string(memory.NewID()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	s, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if !legacy {
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if _, err := s.pool.Exec(ctx, "CREATE TABLE schema_migrations(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		t.Fatal(err)
	}
	gold := phase2K0ReadGold(t)
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		if file.Name() > gold.LegacyMax {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
			return err
		}); err != nil {
			t.Fatal(file.Name(), err)
		}
		count++
	}
	if count != 21 {
		t.Fatalf("legacy fixture must apply exactly 21 migrations, got %d", count)
	}
	return s
}

func phase2K0Ledger(t *testing.T, s *Store) string {
	t.Helper()
	var data string
	if err := s.pool.QueryRow(context.Background(), "SELECT jsonb_agg(to_jsonb(m) ORDER BY name)::text FROM schema_migrations m").Scan(&data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPhase2K0FreshMigrationAndRestart(t *testing.T) {
	s := phase2K0Store(t, false)
	ctx := context.Background()
	before := phase2K0Ledger(t, s)
	var count, policies, jobs int
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM schema_migrations),(SELECT count(*) FROM source_authorizations),(SELECT count(*) FROM memory_jobs)").Scan(&count, &policies, &jobs); err != nil {
		t.Fatal(err)
	}
	if count != 22 || policies != 0 || jobs != 0 {
		t.Errorf("fresh migration counts: applied=%d policy=%d jobs=%d, want 22/0/0", count, policies, jobs)
	}
	if err := s.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("restart migration", err)
	}
	if after := phase2K0Ledger(t, s); after != before {
		t.Error("restart changed migration ledger or applied timestamps")
	}
	phase2K0Evidence(t, "fresh", map[string]any{"applied": count, "policies": policies, "jobs": jobs, "ledger": json.RawMessage(before), "restart_ledger_equal": before == phase2K0Ledger(t, s), "model_registry_configured": false})
}

const phase2K0Owner = "10000000-0000-0000-0000-000000000001"

func phase2K0LegacyFixture(t *testing.T, s *Store) {
	t.Helper()
	gold := phase2K0ReadGold(t)
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		exec := func(sql string, args ...any) error { _, err := tx.Exec(ctx, sql, args...); return err }
		for _, record := range gold.Cases[1].Records {
			if err := exec("INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,$4)", phase2K0Owner, record.ID, record.Kind, record.Version); err != nil {
				return err
			}
			if err := exec("INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,$3)", phase2K0Owner, record.ID, record.Version); err != nil {
				return err
			}
		}
		entity, parent := "20000000-0000-0000-0000-000000000002", "20000000-0000-0000-0000-000000000005"
		for id, kind := range map[string]string{entity: "entity", parent: "summary"} {
			if err := exec("INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,1)", phase2K0Owner, id, kind); err != nil {
				return err
			}
			if err := exec("INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,1)", phase2K0Owner, id); err != nil {
				return err
			}
		}
		source, claim, summary := gold.Cases[1].Records[0], gold.Cases[1].Records[1], gold.Cases[1].Records[2]
		if err := exec("INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,1)", phase2K0Owner, source.ID); err != nil {
			return err
		}
		if err := exec("INSERT INTO sources(owner_id,id,connector,external_id) VALUES($1,$2,'phase2-k0-synthetic','legacy-source')", phase2K0Owner, source.ID); err != nil {
			return err
		}
		for version, body := range map[int]string{1: source.HistoricalBody, source.Version: source.Body} {
			hash := sha256.Sum256([]byte(body))
			if err := exec("INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type) VALUES($1,$2,$3,$4,$5,'K0 synthetic source',$6,'text/plain')", phase2K0Owner, source.ID, version, fmt.Sprintf("v%d", version), hash[:], body); err != nil {
				return err
			}
		}
		if err := exec("INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,'phase2-model')", phase2K0Owner, source.ID); err != nil {
			return err
		}
		if err := exec("INSERT INTO entities(owner_id,id) VALUES($1,$2)", phase2K0Owner, entity); err != nil {
			return err
		}
		if err := exec("INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,'person','Synthetic owner')", phase2K0Owner, entity); err != nil {
			return err
		}
		if err := exec("INSERT INTO claims(owner_id,id) VALUES($1,$2)", phase2K0Owner, claim.ID); err != nil {
			return err
		}
		claimJSON, _ := json.Marshal(map[string]string{"text": claim.Body})
		if err := exec("INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type) VALUES($1,$2,$3,$4,'synthetic',$5,'fact','direct','confirmed','initial')", phase2K0Owner, claim.ID, claim.Version, entity, string(claimJSON)); err != nil {
			return err
		}
		for id, body := range map[string]string{summary.ID: summary.Body, parent: "K0 parent summary preserves typed lineage"} {
			if err := exec("INSERT INTO derived_views(owner_id,id,version,purpose,body) VALUES($1,$2,1,'summary',$3)", phase2K0Owner, id, body); err != nil {
				return err
			}
		}
		if err := exec("INSERT INTO workspace_owners(owner_id,settings) VALUES($1,'{}')", phase2K0Owner); err != nil {
			return err
		}
		if err := exec("INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,'phase2-model','{}')", phase2K0Owner); err != nil {
			return err
		}
		thing, run := "30000000-0000-0000-0000-000000000001", "40000000-0000-0000-0000-000000000001"
		if err := exec("INSERT INTO work_items(owner_id,id,kind,title,status,version,document,created_at,updated_at) VALUES($1,$2,'task','K0 legacy task','todo',1,'{}',now(),now())", phase2K0Owner, thing); err != nil {
			return err
		}
		if err := exec("INSERT INTO agent_runs(owner_id,id,thing_id,agent_id,status,reserved_cost,created_at,document) VALUES($1,$2,$3,'phase2-model','queued',0,now(),'{\"fixture\":\"K0 legacy run preserved\"}')", phase2K0Owner, run, thing); err != nil {
			return err
		}
		for _, record := range gold.Cases[1].Records {
			if err := exec("INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) VALUES($1,$2,$3,$4)", phase2K0Owner, run, record.ID, record.Version); err != nil {
				return err
			}
			if err := exec("INSERT INTO derived_dependencies(owner_id,view_id,view_version,dependency_id,dependency_version) VALUES($1,$2,1,$3,$4)", phase2K0Owner, parent, record.ID, record.Version); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal("legacy SQL fixture", err)
	}
}

func phase2K0LegacyRows(t *testing.T, s *Store) map[string]string {
	t.Helper()
	rows := map[string]string{}
	for _, table := range []string{"memory_records", "record_versions", "sources", "source_versions", "claims", "claim_revisions", "entities", "entity_versions", "derived_views", "record_grants", "workspace_owners", "workspace_agents", "work_items", "agent_runs", "run_dependencies", "derived_dependencies"} {
		omit := "not_a_column"
		if table == "run_dependencies" {
			omit = "memory_kind"
		}
		if table == "derived_dependencies" {
			omit = "dependency_kind"
		}
		var data string
		query := "SELECT coalesce(jsonb_agg(to_jsonb(t)-$1 ORDER BY (to_jsonb(t)-$1)::text),'[]'::jsonb)::text FROM " + pgx.Identifier{table}.Sanitize() + " t"
		if err := s.pool.QueryRow(context.Background(), query, omit).Scan(&data); err != nil {
			t.Fatal(err)
		}
		rows[table] = data
	}
	return rows
}

func TestPhase2K0LegacyKindsAndContentPreserved(t *testing.T) {
	s := phase2K0Store(t, true)
	phase2K0LegacyFixture(t, s)
	ctx := context.Background()
	var oldKinds int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND ((table_name='run_dependencies' AND column_name='memory_kind') OR (table_name='derived_dependencies' AND column_name='dependency_kind'))").Scan(&oldKinds); err != nil {
		t.Fatal(err)
	}
	if oldKinds != 0 {
		t.Fatal("fixture was not a true legacy 021 schema")
	}
	before := phase2K0LegacyRows(t, s)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("021 to K0 migration", err)
	}
	after := phase2K0LegacyRows(t, s)
	if !reflect.DeepEqual(before, after) {
		t.Error("upgrade altered legacy identity/version/content or prior columns")
	}
	gold := phase2K0ReadGold(t)
	actual := map[string]any{}
	for _, record := range gold.Cases[1].Records {
		var runKind, derivedKind string
		var runVersion, derivedVersion int
		if err := s.pool.QueryRow(ctx, "SELECT memory_kind,memory_version FROM run_dependencies WHERE owner_id=$1 AND memory_id=$2", phase2K0Owner, record.ID).Scan(&runKind, &runVersion); err != nil {
			t.Fatal(err)
		}
		if err := s.pool.QueryRow(ctx, "SELECT dependency_kind,dependency_version FROM derived_dependencies WHERE owner_id=$1 AND dependency_id=$2", phase2K0Owner, record.ID).Scan(&derivedKind, &derivedVersion); err != nil {
			t.Fatal(err)
		}
		if runKind != record.Kind || derivedKind != record.Kind || runVersion != record.Version || derivedVersion != record.Version {
			t.Errorf("canonical legacy kind/version %s: run=%s@%d derived=%s@%d want %s@%d", record.ID, runKind, runVersion, derivedKind, derivedVersion, record.Kind, record.Version)
		}
		actual[record.ID] = map[string]any{"run_kind": runKind, "run_version": runVersion, "derived_kind": derivedKind, "derived_version": derivedVersion}
	}
	var policies int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM source_authorizations").Scan(&policies); err != nil {
		t.Fatal(err)
	}
	if policies != 0 {
		t.Error("migration inferred source permission from legacy SQL grant")
	}
	ledger := phase2K0Ledger(t, s)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("repeated upgrade", err)
	}
	if ledger != phase2K0Ledger(t, s) || !reflect.DeepEqual(after, phase2K0LegacyRows(t, s)) {
		t.Error("repeated migration changed legacy data or ledger")
	}
	phase2K0Evidence(t, "legacy", map[string]any{"before": before, "after": after, "typed_kinds": actual, "policies": policies, "legacy_column_count": oldKinds, "restart_unchanged": ledger == phase2K0Ledger(t, s)})
}

type phase2K0Attempt struct {
	ID             string
	Snapshot       []byte
	SnapshotState  string
	SnapshotBytes  int
	InputBytes     int
	MetadataBytes  int
	Manifest       string
	Recipient      string
	BodyExpiry     time.Time
	MetadataExpiry time.Time
}

func phase2K0AttemptDefault() phase2K0Attempt {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return phase2K0Attempt{ID: string(memory.NewID()), SnapshotState: "expired", MetadataBytes: 4, Manifest: "{}", Recipient: "{}", BodyExpiry: now.Add(7 * 24 * time.Hour), MetadataExpiry: now.Add(30 * 24 * time.Hour)}
}

func phase2K0InsertAttempt(s *Store, in phase2K0Attempt) error {
	_, err := s.pool.Exec(context.Background(), `INSERT INTO context_attempts(owner_id,id,operation_id,ordinal,state,recipient,purpose,manifest,metadata_bytes,snapshot,snapshot_state,snapshot_bytes,input_bytes,observation_layer,created_at,body_expires_at,metadata_expires_at) VALUES($1,$2::uuid,$2::text,1,'prepared',$3,'knowledge',$4,$5,$6,$7,$8,$9,'serialized_request','2026-10-01T00:00:00Z',$10,$11)`, phase2K0Owner, in.ID, in.Recipient, in.Manifest, in.MetadataBytes, in.Snapshot, in.SnapshotState, in.SnapshotBytes, in.InputBytes, in.BodyExpiry, in.MetadataExpiry)
	return err
}

func phase2K0Constraint(t *testing.T, name string, err error, reject bool) {
	t.Helper()
	code := ""
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		code = pgerr.Code
	}
	if reject {
		if err == nil {
			t.Errorf("%s: invalid input was accepted", name)
		} else if code != "23514" {
			t.Errorf("%s: expected check violation, got %v", name, err)
		}
	} else if err != nil {
		t.Errorf("%s: legal boundary rejected: %v", name, err)
	}
	phase2K0Evidence(t, "constraint", map[string]any{"case": name, "expected_rejection": reject, "rejected": err != nil, "sqlstate": code})
}

func phase2K0Dependency(s *Store, attempt, parent, kind string, version int, durable bool) error {
	if durable {
		_, err := s.pool.Exec(context.Background(), `INSERT INTO context_artifact_dependencies(owner_id,parent_kind,parent_id,parent_version,dependency_id,dependency_version,dependency_kind,purpose,hard_scope,recipient) VALUES($1,'run',$2,1,$3,$4,$5,'knowledge','{}','{}')`, phase2K0Owner, parent, attempt, version, kind)
		return err
	}
	_, err := s.pool.Exec(context.Background(), `INSERT INTO attempt_typed_dependencies(owner_id,attempt_id,dependency_id,dependency_version,dependency_kind,purpose,hard_scope) VALUES($1,$2,$3,$4,$5,'knowledge','{}')`, phase2K0Owner, parent, attempt, version, kind)
	return err
}

func TestPhase2K0ExactTypedDependencyConstraints(t *testing.T) {
	s := phase2K0Store(t, false)
	gold := phase2K0ReadGold(t).Cases[2]
	in := phase2K0AttemptDefault()
	if err := phase2K0InsertAttempt(s, in); err != nil {
		t.Fatal(err)
	}
	for _, durable := range []bool{false, true} {
		for _, kind := range append(append([]string{}, gold.Accepted...), gold.Rejected...) {
			label := fmt.Sprintf("durable_%t_kind_%s", durable, kind)
			t.Run(label, func(t *testing.T) {
				id := string(memory.NewID())
				reject := true
				for _, accepted := range gold.Accepted {
					if kind == accepted {
						reject = false
					}
				}
				err := phase2K0Dependency(s, id, in.ID, kind, 2, durable)
				phase2K0Constraint(t, label, err, reject)
				if err == nil && !reject {
					var got string
					var version int
					query := "SELECT dependency_kind,dependency_version FROM attempt_typed_dependencies WHERE owner_id=$1 AND dependency_id=$2"
					if durable {
						query = "SELECT dependency_kind,dependency_version FROM context_artifact_dependencies WHERE owner_id=$1 AND dependency_id=$2"
					}
					if err := s.pool.QueryRow(context.Background(), query, phase2K0Owner, id).Scan(&got, &version); err != nil {
						t.Fatal(err)
					}
					if got != kind || version != 2 {
						t.Error("typed dependency lost exact kind/version")
					}
				}
			})
		}
		for _, version := range gold.RejectedVersions {
			t.Run(fmt.Sprintf("durable_%t_version_%d", durable, version), func(t *testing.T) {
				phase2K0Constraint(t, "nonpositive_version", phase2K0Dependency(s, string(memory.NewID()), in.ID, "source", version, durable), true)
			})
		}
	}
}

func TestPhase2K0SnapshotBoundaries(t *testing.T) {
	s := phase2K0Store(t, false)
	limit := phase2K0ReadGold(t).Cases[3].BodyLimit
	for _, label := range []string{"exact_bound", "above_bound", "snapshot_bytes_mismatch", "input_bytes_mismatch", "nonretained_body", "retained_missing_body", "expiry_reversed"} {
		t.Run(label, func(t *testing.T) {
			in := phase2K0AttemptDefault()
			in.Snapshot = []byte(strings.Repeat("x", limit))
			in.SnapshotState = "retained"
			in.SnapshotBytes = limit
			in.InputBytes = limit
			switch label {
			case "above_bound":
				in.Snapshot = append(in.Snapshot, 'x')
				in.SnapshotBytes++
				in.InputBytes++
			case "snapshot_bytes_mismatch":
				in.SnapshotBytes--
			case "input_bytes_mismatch":
				in.InputBytes--
			case "nonretained_body":
				in.SnapshotState = "deleted"
			case "retained_missing_body":
				in.Snapshot = nil
				in.SnapshotBytes = 0
				in.InputBytes = 0
			case "expiry_reversed":
				in.MetadataExpiry = in.BodyExpiry.Add(-time.Second)
			}
			err := phase2K0InsertAttempt(s, in)
			phase2K0Constraint(t, label, err, label != "exact_bound")
			if err == nil && label == "exact_bound" {
				var body []byte
				var actual, declared, input int
				if err := s.pool.QueryRow(context.Background(), "SELECT snapshot,octet_length(snapshot),snapshot_bytes,input_bytes FROM context_attempts WHERE owner_id=$1 AND id=$2", phase2K0Owner, in.ID).Scan(&body, &actual, &declared, &input); err != nil {
					t.Fatal(err)
				}
				if len(body) != limit || actual != limit || declared != limit || input != limit || string(body) != string(in.Snapshot) {
					t.Error("exact boundary snapshot did not round-trip")
				}
			}
		})
	}
}

func TestPhase2K0MetadataDeclarationBoundaries(t *testing.T) {
	s := phase2K0Store(t, false)
	limit := phase2K0ReadGold(t).Cases[4].MetadataLimit
	for _, label := range []string{"declared_exact_bound", "declared_above_bound", "manifest_above_bound", "metadata_undercount"} {
		t.Run(label, func(t *testing.T) {
			in := phase2K0AttemptDefault()
			in.MetadataBytes = limit
			switch label {
			case "declared_above_bound":
				in.MetadataBytes++
			case "manifest_above_bound":
				in.Manifest = `{"diagnostic":"` + strings.Repeat("x", limit) + `"}`
			case "metadata_undercount":
				in.MetadataBytes = 3
			}
			phase2K0Constraint(t, label, phase2K0InsertAttempt(s, in), label != "declared_exact_bound")
		})
	}
	phase2K0Evidence(t, "runtime-limit", map[string]any{"ddl_scope": "declared metadata_bytes upper bound and physical manifest check", "whole_metadata_accounting": "not implemented/verified in K0", "owner_atomic_quota": "not implemented/verified in K0", "side_table_dependency_accounting": "requires K1"})
}

func TestPhase2K0AttemptIndependentOfBusinessRows(t *testing.T) {
	s := phase2K0Store(t, false)
	in := phase2K0AttemptDefault()
	if err := phase2K0InsertAttempt(s, in); err != nil {
		t.Fatal("independent writer must not require business owner/run/source", err)
	}
	missingSource := string(memory.NewID())
	if err := phase2K0Dependency(s, missingSource, in.ID, "source", 1, false); err != nil {
		t.Fatal("attempt dependency must not require business record", err)
	}
	var businessOwners, records, attempts, dependencies int
	if err := s.pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM workspace_owners),(SELECT count(*) FROM memory_records),(SELECT count(*) FROM context_attempts),(SELECT count(*) FROM attempt_typed_dependencies)").Scan(&businessOwners, &records, &attempts, &dependencies); err != nil {
		t.Fatal(err)
	}
	if businessOwners != 0 || records != 0 || attempts != 1 || dependencies != 1 {
		t.Errorf("independent counts %d/%d/%d/%d want 0/0/1/1", businessOwners, records, attempts, dependencies)
	}
	var businessFKs int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid JOIN pg_namespace n ON n.oid=child.relnamespace JOIN pg_class parent ON parent.oid=c.confrelid WHERE c.contype='f' AND n.nspname=current_schema() AND child.relname IN ('context_attempts','attempt_typed_dependencies') AND parent.relname<>'context_attempts'`).Scan(&businessFKs); err != nil {
		t.Fatal(err)
	}
	if businessFKs != 0 {
		t.Errorf("diagnostic writer has %d business foreign keys", businessFKs)
	}
	phase2K0Evidence(t, "independent", map[string]any{"business_owner_rows": businessOwners, "business_record_rows": records, "attempt_rows": attempts, "diagnostic_dependency_rows": dependencies, "business_foreign_keys": businessFKs})
}

func TestPhase2K0DurableDependencySurvivesAttemptAndSourceDeletion(t *testing.T) {
	s := phase2K0Store(t, false)
	in := phase2K0AttemptDefault()
	if err := phase2K0InsertAttempt(s, in); err != nil {
		t.Fatal(err)
	}
	source := string(memory.NewID())
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'source',1)", phase2K0Owner, source); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,1)", phase2K0Owner, source)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := phase2K0Dependency(s, source, in.ID, "source", 1, false); err != nil {
		t.Fatal(err)
	}
	if err := phase2K0Dependency(s, source, in.ID, "source", 1, true); err != nil {
		t.Fatal(err)
	}
	var before, after string
	query := "SELECT to_jsonb(d)::text FROM context_artifact_dependencies d WHERE owner_id=$1 AND dependency_id=$2"
	if err := s.pool.QueryRow(ctx, query, phase2K0Owner, source).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM context_attempts WHERE owner_id=$1 AND id=$2", phase2K0Owner, in.ID); err != nil {
		t.Fatal(err)
	}
	var attemptRows, diagnosticDeps, durableDeps int
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM context_attempts),(SELECT count(*) FROM attempt_typed_dependencies),(SELECT count(*) FROM context_artifact_dependencies)").Scan(&attemptRows, &diagnosticDeps, &durableDeps); err != nil {
		t.Fatal(err)
	}
	if attemptRows != 0 || diagnosticDeps != 0 || durableDeps != 1 {
		t.Errorf("delete attempt counts %d/%d/%d want 0/0/1", attemptRows, diagnosticDeps, durableDeps)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_records WHERE owner_id=$1 AND id=$2", phase2K0Owner, source); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, query, phase2K0Owner, source).Scan(&after); err != nil {
		t.Fatal("durable lineage disappeared with diagnostic/business record", err)
	}
	if before != after {
		t.Error("durable dependency changed when attempt/source identity deleted")
	}
	phase2K0Evidence(t, "durable", map[string]any{"attempt_rows_after_delete": attemptRows, "diagnostic_dependencies_after_delete": diagnosticDeps, "durable_dependencies_after_delete": durableDeps, "before": json.RawMessage(before), "after": json.RawMessage(after), "runtime_artifact_invalidation": "not implemented/verified in K0"})
}
