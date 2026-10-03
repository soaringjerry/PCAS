package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
)

// A local DSN is mandatory and database-wide emptiness is checked before any
// migration or write. Existing product/test tables cause refusal, even when a
// supplied search_path could hide them. Every run owns one removable schema.
func openTemporary(ctx context.Context, dsn string) (*postgres.Store, *pgxpool.Pool, func(), error) {
	noop := func() {}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, nil, noop, fmt.Errorf("invalid temporary database DSN")
	}
	hosts := []string{config.Host}
	for _, fallback := range config.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return nil, nil, noop, fmt.Errorf("eval_database_not_local: use a disposable local PostgreSQL instance")
		}
	}
	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, nil, noop, fmt.Errorf("eval_database_unavailable: check temporary PostgreSQL")
	}
	defer admin.Close(ctx)
	var existing int
	err = admin.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp_%'
 AND c.relkind IN ('r','p','v','m','S','f') AND NOT EXISTS(SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.deptype='e')`).Scan(&existing)
	if err != nil {
		return nil, nil, noop, err
	}
	if existing != 0 {
		return nil, nil, noop, fmt.Errorf("eval_database_not_empty: use a new disposable database; existing tables are never changed")
	}
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public`); err != nil {
		return nil, nil, noop, err
	}
	schema := "pcas_eval_" + strings.ReplaceAll(string(memory.NewID()), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return nil, nil, noop, err
	}
	config.RuntimeParams["search_path"] = schema + ",public"
	isolatedDSN := config.ConnString()
	if strings.HasPrefix(isolatedDSN, "postgres://") || strings.HasPrefix(isolatedDSN, "postgresql://") {
		u, err := url.Parse(isolatedDSN)
		if err != nil {
			cleanupSchema(ctx, admin, schema)
			return nil, nil, noop, err
		}
		q := u.Query()
		q.Set("search_path", schema+",public")
		u.RawQuery = q.Encode()
		isolatedDSN = u.String()
	} else {
		isolatedDSN += " search_path='" + schema + ",public'"
	}
	cleanup := func() {
		conn, e := pgx.Connect(context.Background(), dsn)
		if e == nil {
			_, _ = conn.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			_ = conn.Close(context.Background())
		}
	}
	store, err := postgres.Open(ctx, isolatedDSN)
	if err != nil {
		cleanup()
		return nil, nil, noop, err
	}
	pool, err := pgxpool.New(ctx, isolatedDSN)
	if err != nil {
		store.Close()
		cleanup()
		return nil, nil, noop, err
	}
	done := func() { pool.Close(); store.Close(); cleanup() }
	if err := store.Migrate(ctx); err != nil {
		done()
		return nil, nil, noop, err
	}
	return store, pool, done, nil
}

func cleanupSchema(ctx context.Context, conn *pgx.Conn, schema string) {
	_, _ = conn.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
}
