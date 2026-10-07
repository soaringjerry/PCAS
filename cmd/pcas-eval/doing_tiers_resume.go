package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func tierCell(run int, method, task string) string {
	return fmt.Sprintf("%d/%s/%s", run, method, task)
}

func missingTierProviders(providers []doing.Provider, rows []doing.Row) []doing.Provider {
	seen := map[string]bool{}
	for _, row := range rows {
		seen[tierCell(row.Run, row.Method, row.Task)] = true
	}
	for i := range providers {
		name := providers[i].Name
		providers[i].AcceptRun = func(run int, t doing.Task) bool { return !seen[tierCell(run, name, t.ID)] }
	}
	return providers
}

func loadTierResume(path, suitePath string, s doing.Suite, fake bool, model, channel string, workers int) (doing.Report, string, error) {
	var r doing.Report
	if err := outsideRepository(path); err != nil {
		return r, "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return r, "", err
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, "", fmt.Errorf("invalid numeric resume report")
	}
	raw, err := os.ReadFile(suitePath)
	if err != nil {
		return r, "", err
	}
	if fake {
		model, channel = "v2-fake", "local"
	}
	if !s.Synthetic || len(s.Memories) != 671 || len(s.Tasks) != 120 || r.Version != 1 || r.Fake != fake || r.Model != model || r.Channel != channel || r.Repeats != 3 || r.Workers != workers || r.HostDate != time.Now().UTC().Format("2006-01-02") || r.AsOf != s.AsOf || r.SuiteSHA != doing.SHA(string(raw)) || r.AnswerPromptSHA != doing.SHA(doing.AnswerSystem) || r.JudgePromptSHA != doing.SHA(doing.JudgeSystem) || r.AnswerLimit != doing.AnswerLimit || r.Preparation == nil || !r.Preparation.Complete || len(r.Preparation.Stages) != 3 || len(r.Rows) >= 840 {
		return r, "", fmt.Errorf("resume fingerprints/matrix/preparation mismatch or nothing missing")
	}
	tasks := map[string]doing.Task{}
	for _, t := range s.Tasks {
		tasks[t.ID] = t
	}
	validCell := func(run int, method, task string) bool {
		t, exists := tasks[task]
		return exists && run >= 1 && run <= 3 && (method == "light" || method == "medium" || method == "heavy" && (t.Category == "cross_group" || t.Category == "outgoing"))
	}
	seen := map[string]bool{}
	for _, row := range r.Rows {
		key := tierCell(row.Run, row.Method, row.Task)
		t := tasks[row.Task]
		if !validCell(row.Run, row.Method, row.Task) || seen[key] || row.Category != t.Category || row.TierUsage == nil || row.TierUsage.Requested != row.Method {
			return r, "", fmt.Errorf("invalid/duplicate completed resume cell")
		}
		seen[key] = true
		calls := 2
		for _, n := range row.TierUsage.Calls {
			if n < 0 {
				return r, "", fmt.Errorf("invalid completed call ledger")
			}
			calls += n
		}
		if calls != row.ModelCalls {
			return r, "", fmt.Errorf("completed call ledger changed")
		}
		for _, j := range row.Judgments {
			encoded, _ := json.Marshal(j)
			if _, err := doing.ParseJudgment(string(encoded), t); err != nil {
				return r, "", fmt.Errorf("invalid completed judgment")
			}
		}
		score := doing.Score(t, row.Judgments)
		score.Usable = score.Usable && row.AnswerChars <= doing.AnswerLimit
		if score.MustTotal != row.MustTotal || score.MustBoth != row.MustBoth || score.BonusTotal != row.BonusTotal || score.BonusBoth != row.BonusBoth || score.ForbiddenTotal != row.ForbiddenTotal || score.ForbiddenEither != row.ForbiddenEither || score.Usable != row.Usable || !reflect.DeepEqual(score.Disagreements, row.Disagreements) {
			return r, "", fmt.Errorf("completed resume score was changed")
		}
	}
	for _, f := range r.Failures {
		key := tierCell(f.Run, f.Method, f.Task)
		if !validCell(f.Run, f.Method, f.Task) || seen[key] || f.ModelCalls < 0 {
			return r, "", fmt.Errorf("invalid/overlapping resume failure")
		}
		seen[key] = true
	}
	return r, doing.SHA(string(b)), nil
}

// This is an explicit fictional-only repair path. The caller first restores the
// saved preparation into a NEW disposable container. No existing schema is
// changed: validate the evaluator marker, source texts, counts and empty turn
// tables before creating worker TEMPLATE copies. Normal/private runs retain the
// database-wide emptiness guard and never enter here.
func restoredTierDatabase(ctx context.Context, dsn string, s doing.Suite, p doing.Preparation) (string, memory.Scope, error) {
	var scope memory.Scope
	if !s.Synthetic {
		return "", scope, fmt.Errorf("snapshot repair is fictional-only")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return "", scope, fmt.Errorf("invalid restored database")
	}
	hosts := []string{cfg.Host}
	for _, f := range cfg.Fallbacks {
		hosts = append(hosts, f.Host)
	}
	for _, h := range hosts {
		if h != "localhost" && h != "127.0.0.1" && h != "::1" {
			return "", scope, fmt.Errorf("restored database must be local and disposable")
		}
	}
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return "", scope, fmt.Errorf("restored database unavailable")
	}
	defer admin.Close(ctx)
	var schemas []string
	if err = admin.QueryRow(ctx, `SELECT coalesce(array_agg(schemaname),'{}') FROM pg_tables WHERE tablename='v2_frozen_records'`).Scan(&schemas); err != nil || len(schemas) != 1 {
		return "", scope, fmt.Errorf("restore must contain exactly one evaluator preparation marker")
	}
	schema := schemas[0]
	suffix := strings.TrimPrefix(schema, "pcas_eval_")
	if decoded, e := hex.DecodeString(suffix); e != nil || len(decoded) != 16 || len(suffix) != 32 || !strings.HasPrefix(schema, "pcas_eval_") {
		return "", scope, fmt.Errorf("invalid evaluator schema")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", scope, fmt.Errorf("restore requires PostgreSQL URI")
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	prepared := u.String()
	pool, err := pgxpool.New(ctx, prepared)
	if err != nil {
		return "", scope, fmt.Errorf("restored pool unavailable")
	}
	defer pool.Close()
	var owners []string
	if err = pool.QueryRow(ctx, `SELECT coalesce(array_agg(DISTINCT owner_id::text),'{}') FROM v2_frozen_records`).Scan(&owners); err != nil || len(owners) != 1 || !memory.ID(owners[0]).Valid() {
		return "", scope, fmt.Errorf("invalid restored owner")
	}
	scope = memory.Scope{OwnerID: memory.ID(owners[0]), PrincipalID: "owner", IsOwner: true}
	var extra, turns, candidates int
	if err = pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM memory_records r WHERE NOT EXISTS(SELECT 1 FROM v2_frozen_records f WHERE (f.owner_id,f.id)=(r.owner_id,r.id))),
 (SELECT count(*) FROM desk_turns),(SELECT count(*) FROM capture_candidates)`).Scan(&extra, &turns, &candidates); err != nil || extra != 0 || turns != 0 || candidates != 0 {
		return "", scope, fmt.Errorf("restored preparation differs or contains answer turns")
	}
	state, err := readTierPreparation(ctx, pool, scope)
	if err != nil || state.Claims != p.Claims || state.Retired != p.Retired || state.OrganizeRule != p.OrganizeRule || state.HandoverRule != p.HandoverRule || state.Deadlines != p.Deadlines || state.Requirements != p.Requirements || state.HandoverInputs != p.HandoverInputs || state.Handover != p.Handover {
		return "", scope, fmt.Errorf("restored preparation differs or is stale")
	}
	memories := s.ByID()
	rows, err := pool.Query(ctx, `SELECT s.external_id,v.title,v.body FROM sources s JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(s.owner_id,s.id,1)`)
	if err != nil {
		return "", scope, fmt.Errorf("restored sources unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var id, title, text string
		if err = rows.Scan(&id, &title, &text); err != nil {
			return "", scope, fmt.Errorf("restored source unreadable")
		}
		id = strings.TrimPrefix(id, "eval/")
		m, ok := memories[id]
		if !ok || title != m.Group || text != m.Text {
			return "", scope, fmt.Errorf("restored fictional source differs")
		}
		delete(memories, id)
	}
	if rows.Err() != nil || len(memories) != 0 {
		return "", scope, fmt.Errorf("restored fictional sources incomplete")
	}
	return prepared, scope, nil
}
