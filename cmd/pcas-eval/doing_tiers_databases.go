package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type tierDatabase struct {
	name   string
	store  *postgres.Store
	pool   *pgxpool.Pool
	bridge *tierBridge
	scope  memory.Scope
}

// PostgreSQL TEMPLATE copies the prepared derived state once per worker. The
// original database has already passed the database-wide emptiness guard, and
// all of its connections are closed before copying. No online DSN is accepted.
func cloneTierDatabases(ctx context.Context, preparedDSN string, scope memory.Scope, n int, model doing.Model, fake bool) (chan *tierDatabase, func(), error) {
	cfg, err := pgx.ParseConfig(preparedDSN)
	if err != nil {
		return nil, func() {}, fmt.Errorf("invalid prepared database")
	}
	uri, err := url.Parse(preparedDSN)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") {
		return nil, func() {}, fmt.Errorf("tiers require a PostgreSQL URI DSN")
	}
	template := cfg.Database
	adminCfg := cfg.Copy()
	adminCfg.Database = "postgres"
	delete(adminCfg.RuntimeParams, "search_path")
	admin, err := pgx.ConnectConfig(ctx, adminCfg)
	if err != nil {
		return nil, func() {}, fmt.Errorf("clone_admin_unavailable")
	}
	var databases []*tierDatabase
	cleanup := func() {
		for _, d := range databases {
			if d.bridge != nil {
				d.bridge.server.Close()
			}
			if d.store != nil {
				d.store.Close()
			}
			if d.pool != nil {
				d.pool.Close()
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, _ = admin.Exec(cleanupCtx, "DROP DATABASE "+pgx.Identifier{d.name}.Sanitize()+" WITH (FORCE)")
			cancel()
		}
		_ = admin.Close(context.Background())
	}
	ready := make(chan *tierDatabase, n)
	for i := 0; i < n; i++ {
		d := &tierDatabase{name: "pcas_v2c_" + strings.ReplaceAll(string(memory.NewID()), "-", ""), scope: scope}
		if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{d.name}.Sanitize()+" TEMPLATE "+pgx.Identifier{template}.Sanitize()); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("clone_failed: use a disposable database role with CREATEDB")
		}
		databases = append(databases, d)
		cloneURI := *uri
		cloneURI.Path = "/" + d.name
		cloneDSN := cloneURI.String()
		d.store, err = postgres.Open(ctx, cloneDSN)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("clone_store_failed")
		}
		d.pool, err = pgxpool.New(ctx, cloneDSN)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("clone_pool_failed")
		}
		d.bridge = newTierBridge(model, fake)
		d.store.SetModels(d.bridge.registry())
		ready <- d
	}
	return ready, cleanup, nil
}

func (d *tierDatabase) clear(ctx context.Context) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		for _, table := range []string{"desk_turns", "desk_turn_order", "capture_candidates"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE owner_id=$1", string(d.scope.OwnerID)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM memory_records r WHERE r.owner_id=$1 AND NOT EXISTS(SELECT 1 FROM v2_frozen_records f WHERE (f.owner_id,f.id)=(r.owner_id,r.id))`, string(d.scope.OwnerID))
		return err
	})
}

func tierProvider(name string, databases chan *tierDatabase, categories map[string]bool) doing.Provider {
	p := doing.Provider{Name: name, Get: func(ctx context.Context, s doing.Suite, t doing.Task) (doing.Evidence, error) {
		var d *tierDatabase
		select {
		case d = <-databases:
		case <-ctx.Done():
			return doing.Evidence{}, ctx.Err()
		}
		defer func() { databases <- d }()
		if err := d.clear(ctx); err != nil {
			return doing.Evidence{}, fmt.Errorf("transient_cleanup_failed")
		}
		d.bridge.reset("", s, t, name)
		request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "v2-tiers", Text: t.Request}
		out, err := d.store.DeskTurn(postgres.WithMemoryTier(ctx, name), d.scope, request)
		usage, inputChars, contextChars := d.bridge.metrics()
		e := doing.Evidence{InputChars: inputChars, ContextChars: contextChars, Usage: &usage}
		for _, n := range usage.Calls {
			e.ModelCalls += n
		}
		if err != nil {
			return e, fmt.Errorf("product_turn_failed")
		}
		if len(out.Turn.Receipts) != 0 {
			return e, fmt.Errorf("unexpected_product_actions")
		}
		if usage.Calls["secretary"] == 0 || usage.Failed["secretary"] == usage.Calls["secretary"] {
			return e, fmt.Errorf("product_answer_call_failed")
		}
		var plan struct {
			Tier   string   `json:"tier"`
			Groups []string `json:"groups"`
		}
		var planJSON []byte
		usageErr := d.pool.QueryRow(ctx, `SELECT tier,plan FROM model_usage WHERE owner_id=$1 AND turn_id=$2 AND purpose='secretary' ORDER BY at DESC LIMIT 1`, string(d.scope.OwnerID), out.Turn.ID).Scan(&plan.Tier, &planJSON)
		if usageErr != nil && usageErr != pgx.ErrNoRows {
			return e, fmt.Errorf("product_usage_read_failed")
		}
		if usageErr == pgx.ErrNoRows {
			plan.Tier = "legacy-fallback"
		} else {
			var groups struct {
				Groups []string `json:"groups"`
			}
			if json.Unmarshal(planJSON, &groups) != nil {
				return e, fmt.Errorf("product_usage_plan_invalid")
			}
			plan.Groups = groups.Groups
		}
		usage.Effective, usage.SelectedGroups = plan.Tier, len(plan.Groups)
		usage.SelfcheckFallback = plan.Tier != "light" && plan.Tier != "legacy-fallback" && (usage.Calls["selfcheck"] == 0 || usage.Failed["selfcheck"] > 0 || usage.Malformed["selfcheck"] > 0)
		e.Usage = &usage
		e.Answer = &out.Turn.Reply
		return e, nil
	}}
	if len(categories) > 0 {
		p.Accept = func(t doing.Task) bool { return categories[t.Category] }
	}
	return p
}
