package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestOrganizeSkeletonUpgradePreservesData(t *testing.T) {
	s := b1EmptyStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE TABLE schema_migrations (
 name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "035_memory_organize.sql" {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, string(body)); err != nil {
			t.Fatal(file.Name(), err)
		}
		if _, err := s.pool.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", file.Name(), fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			t.Fatal(err)
		}
	}
	b1Model(t, s, `{}`)
	scope := owner()
	source := b1Source(t, s, scope, "虚构园艺记录", "云杉喜欢在周末照料盆栽。", "manual")
	ref := b1Claim(t, s, scope, "云杉喜欢在周末照料盆栽。", "preference", "adopted", source)
	before := b1DatabaseRows(t, s, false)
	columns := b1DatabaseColumns(t, s, before)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, b1DatabaseRowsAtColumns(t, s, columns)) {
		t.Fatal("035 changed existing business data")
	}
	for range 2 {
		if err := s.Migrate(ctx); err != nil {
			t.Fatal("restart", err)
		}
	}
	var organized, attempts, applied int
	if err := s.pool.QueryRow(ctx, "SELECT organized,organize_attempts FROM claims WHERE owner_id=$1 AND id=$2", scope.OwnerID, ref.ID).Scan(&organized, &attempts); err != nil || organized != 0 || attempts != 0 {
		t.Fatal("unexpected organization defaults", organized, attempts, err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE name='035_memory_organize.sql'").Scan(&applied); err != nil || applied != 1 {
		t.Fatal("035 must be recorded once", applied, err)
	}
	m, err := s.GetMemory(ctx, scope, string(ref.ID))
	if err != nil || m.Category != "unknown" || m.Durable != nil || m.Groups == nil || len(m.Groups) != 0 || m.Version != 1 {
		t.Fatal("unexpected memory defaults", m, err)
	}
	facets, err := s.MemoryFacets(ctx, scope)
	if err != nil || facets.Groups == nil || len(facets.Groups) != 0 {
		t.Fatal("unexpected group facets", facets, err)
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil || state.Organize != (workspace.Organize{Done: 0, Total: 1, Version: 1}) {
		t.Fatal("unexpected organize snapshot", state.Organize, err)
	}
}

func TestOrganizeSkeletonMemoryJSON(t *testing.T) {
	for _, durable := range []*bool{nil, new(bool), new(true)} {
		data, err := json.Marshal(workspace.Memory{Durable: durable})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["category"]) != `"unknown"` || string(fields["groups"]) != "[]" {
			t.Fatal("missing defaults", string(data))
		}
		value, present := fields["durable"]
		if durable == nil && present || durable != nil && (!present || string(value) != fmt.Sprint(*durable)) {
			t.Fatal("durable must preserve unset, false and true", string(data))
		}
	}
}
