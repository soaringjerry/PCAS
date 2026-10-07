package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/cmd/pcas-eval/fixture"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// No tags, supersession edges, must references or expected answers enter the
// product seed. The product itself derives every grouping and retirement.
func seedTierSuite(ctx context.Context, store *postgres.Store, pool *pgxpool.Pool, s doing.Suite) (fixture.Seeded, error) {
	asof, _ := time.Parse(time.RFC3339, s.AsOf)
	anchor := time.Date(asof.Year(), asof.Month(), asof.Day(), 0, 0, 0, 0, time.UTC)
	c := fixture.Corpus{SchemaVersion: 1, Persona: s.Persona, Timezone: s.Timezone}
	for _, m := range s.Memories {
		at, _ := time.Parse(time.RFC3339, m.ExpressedAt)
		recorded := at
		if m.RecordedAt != "" {
			recorded, _ = time.Parse(time.RFC3339, m.RecordedAt)
		}
		item := fixture.Item{Kind: "memory", Text: m.Text, Nature: "fact", Subject: "我", Predicate: "描述", Quote: m.Text, Confidence: 1, Acquisition: "direct", Qualification: "asserted", Explicit: true}
		c.Noise = append(c.Noise, fixture.Document{ID: m.ID, Title: m.Group, Text: m.Text, ExpressedDays: int(at.Truncate(24*time.Hour).Sub(anchor).Hours() / 24), RecordedDays: int(recorded.Truncate(24*time.Hour).Sub(anchor).Hours() / 24), Connector: "capture", Role: "user", Gold: fixture.Extraction{Items: []fixture.Item{item}}})
	}
	seeded, err := fixture.Seed(ctx, store, pool, c, anchor, "v2-tiers", true)
	if err != nil {
		return seeded, err
	}
	if !s.Synthetic {
		for _, m := range s.Memories {
			expressed, _ := time.Parse(time.RFC3339, m.ExpressedAt)
			recorded := expressed
			if m.RecordedAt != "" {
				recorded, _ = time.Parse(time.RFC3339, m.RecordedAt)
			}
			if _, err = pool.Exec(ctx, `UPDATE record_versions v SET expressed_at=$3,recorded_at=$4
 WHERE v.owner_id=$1 AND (v.record_id=$2 OR v.record_id IN (
 SELECT id FROM chunks WHERE owner_id=$1 AND source_id=$2 UNION SELECT target_id FROM evidence WHERE owner_id=$1 AND source_id=$2))`, string(seeded.Scope.OwnerID), string(seeded.Sources[m.ID].ID), expressed, recorded); err != nil {
				return seeded, err
			}
		}
	}
	return seeded, nil
}

func prepareTierSuite(ctx context.Context, store *postgres.Store, pool *pgxpool.Pool, scope memory.Scope, bridge *tierBridge, checkpoint string) (doing.Preparation, error) {
	start := time.Now()
	p := doing.Preparation{StartedAt: start.UTC().Format(time.RFC3339), Stages: []doing.PreparationStage{}}
	defer func() { bridge.reset("", doing.Suite{}, doing.Task{}, "") }()
	stages := []struct {
		name     string
		schedule func(context.Context, time.Time) (int, error)
		handlers map[string]worker.Handler
	}{
		{"organize", store.ScheduleOrganize, map[string]worker.Handler{postgres.OrganizeStage: store.ProcessOrganize}},
		{"compare", store.ScheduleCompare, map[string]worker.Handler{postgres.CompareStage: store.ProcessCompare, postgres.EntityCompareStage: store.ProcessEntityCompare}},
		{"status", store.ScheduleStatus, map[string]worker.Handler{postgres.HandoverStage: store.ProcessHandover}},
	}
	for _, stage := range stages {
		at := time.Now()
		p.Stages = append(p.Stages, doing.PreparationStage{Stage: stage.name, PurposeCalls: map[string]int{}})
		metrics := &p.Stages[len(p.Stages)-1]
		handlers := map[string]worker.Handler{}
		for key, h := range stage.handlers {
			handlers[key] = func(ctx context.Context, j worker.Job) error {
				purpose := strings.TrimPrefix(strings.SplitN(j.Stage, ":", 2)[0], "memory.")
				bridge.reset(purpose, doing.Suite{}, doing.Task{}, "")
				err := h(ctx, j)
				usage, _, _ := bridge.metrics()
				for purpose, n := range usage.Calls {
					metrics.ModelCalls += n
					metrics.PurposeCalls[purpose] += n
				}
				for _, n := range usage.Failed {
					metrics.FailedCalls += n
				}
				for _, n := range usage.InputChars {
					metrics.InputChars += n
				}
				if err == nil {
					metrics.JobsDone++
				}
				return err
			}
		}
		// Product queue leasing, retry/backoff, hourly limits and budget remain
		// active. No timestamp edits or fake labels accelerate real preparation.
		w := worker.New(store, handlers, slog.New(slog.NewTextHandler(io.Discard, nil)))
		lastProgress := time.Time{}
		for {
			if _, err := stage.schedule(ctx, time.Now()); err != nil {
				return p, fmt.Errorf("prepare_%s_schedule_failed", stage.name)
			}
			did, err := w.RunOnce(ctx)
			metrics.WallMS = float64(time.Since(at)) / float64(time.Millisecond)
			p.WallMS = float64(time.Since(start)) / float64(time.Millisecond)
			if e := doing.WriteJSON(checkpoint, p); e != nil {
				return p, e
			}
			if err != nil {
				return p, fmt.Errorf("prepare_%s_worker_failed", stage.name)
			}
			var pending, failed int
			prefixes := []string{}
			for key := range stage.handlers {
				prefixes = append(prefixes, key+":%")
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state IN ('queued','leased')),
 count(*) FILTER(WHERE state IN ('failed','blocked')) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE ANY($2::text[])`, string(scope.OwnerID), prefixes).Scan(&pending, &failed); err != nil {
				return p, err
			}
			if failed > 0 {
				return p, fmt.Errorf("prepare_%s_has_failed_jobs", stage.name)
			}
			if !did && pending == 0 {
				break
			}
			if time.Since(lastProgress) >= time.Minute {
				fmt.Printf("prepare stage=%s calls=%d pending=%d elapsed_seconds=%.0f\n", stage.name, metrics.ModelCalls, pending, time.Since(at).Seconds())
				lastProgress = time.Now()
			}
			if !did {
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return p, ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	owner := string(scope.OwnerID)
	var unorganized int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE c.retired!=''),count(*) FILTER(WHERE c.organized<$2)
 FROM claims c JOIN memory_records r ON(r.owner_id,r.id)=(c.owner_id,c.id) WHERE c.owner_id=$1 AND r.state='active'`, owner, postgres.OrganizeVersion).Scan(&p.Claims, &p.Retired, &unorganized); err != nil {
		return p, err
	}
	if unorganized != 0 {
		return p, fmt.Errorf("prepare_organize_incomplete")
	}
	p.Cards = 0
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM handovers WHERE owner_id=$1 AND input_hash=library_handover_hash($1) AND rule>=$2 AND btrim(body)!='')`, owner, postgres.HandoverVersion).Scan(&p.Handover); err != nil {
		return p, err
	}
	var handoverInputs bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM library_handover_members($1))", owner).Scan(&handoverInputs); err != nil {
		return p, err
	}
	if handoverInputs && !p.Handover {
		return p, fmt.Errorf("prepare_handover_incomplete")
	}
	// Freeze identities AFTER derived product records have been created.
	if _, err := pool.Exec(ctx, `CREATE TABLE v2_frozen_records AS SELECT owner_id,id FROM memory_records`); err != nil {
		return p, err
	}
	p.Complete = true
	p.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	p.WallMS = float64(time.Since(start)) / float64(time.Millisecond)
	return p, doing.WriteJSON(checkpoint, p)
}
