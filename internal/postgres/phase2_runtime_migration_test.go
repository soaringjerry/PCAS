package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func phase2RTLegacy022Store(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("independent migration requires disposable PCAS_TEST_DATABASE_URL; no skip")
	}
	ctx := context.Background()
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"pcas_phase2_c_023_" + strings.ReplaceAll(string(memory.NewID()), "-", "")}.Sanitize()
	if _, err := admin.pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", strings.Trim(schema, "\"")+",public")
	u.RawQuery = q.Encode()
	s, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err := s.pool.Exec(ctx, "CREATE TABLE schema_migrations(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range files {
		if f.Name() >= "023_" {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", f.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
			return err
		}); err != nil {
			t.Fatal(f.Name(), err)
		}
		count++
	}
	if count != 22 {
		t.Fatalf("actual old 022 fixture migrations=%d want22", count)
	}
	return s
}

func TestPhase2Runtime023PreservesOldPolicyMeaningAndVersions(t *testing.T) {
	s := phase2RTLegacy022Store(t)
	ctx, scope := context.Background(), owner()
	source := mustIngest(t, s, scope, input())
	for _, revoked := range []bool{false, true} {
		id, principal := string(memory.NewID()), "old-active"
		if revoked {
			principal = "old-revoked"
		}
		_, err := s.pool.Exec(ctx, `INSERT INTO source_authorizations(owner_id,id,source_id,principal_id,role,model,provider,protocol,channel,route_fingerprint,purpose,scope_kind,revision,revoked) VALUES($1,$2,$3,$4,'secretary','old-synthetic','old-provider','openai','automatic',repeat('0',64),'knowledge','unscoped',7,$5)`, string(scope.OwnerID), id, string(source.ID), principal, revoked)
		if err != nil {
			t.Fatal(err)
		}
	}
	policies := func(after bool) string {
		expression := "to_jsonb(a)"
		if after {
			expression += "-'explicit_deny'"
		}
		var text string
		if err := s.pool.QueryRow(ctx, "SELECT jsonb_agg("+expression+" ORDER BY id)::text FROM source_authorizations a WHERE owner_id=$1", string(scope.OwnerID)).Scan(&text); err != nil {
			t.Fatal(err)
		}
		return text
	}
	before := policies(false)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("actual 022 upgrade", err)
	}
	if policies(true) != before {
		t.Error("migration lost existing policy identity/revision/recipient/allow state")
	}
	var mismatch, count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE explicit_deny!=revoked),count(*) FROM source_authorizations WHERE owner_id=$1", string(scope.OwnerID)).Scan(&mismatch, &count); err != nil {
		t.Fatal(err)
	}
	if count != 2 || mismatch != 0 {
		t.Errorf("old revoked deny meaning mismatch=%d rows=%d", mismatch, count)
	}
	current, err := s.GetSource(ctx, scope, source.ID, source.Version)
	if err != nil || current.Source.Text != input().Text || current.Source.Ref != source.Ref {
		t.Error("source body or exact identity/version changed during upgrade", err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE source_authorizations SET explicit_deny=true WHERE owner_id=$1 AND NOT revoked", string(scope.OwnerID)); err == nil {
		t.Error("DDL permitted explicit deny without revoked")
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("repeat upgraded startup", err)
	}
	if policies(true) != before {
		t.Error("repeat migration reset existing policy or restored allow")
	}
	phase2RTEvidence(t, "023-upgrade", map[string]any{"legacy_migrations": 22, "policy_rows": count, "meaning_mismatches": mismatch, "original_policy_rows": before, "source_ref": source.Ref, "external_send": "none; actual old schema policy SQL fixture"})
}
