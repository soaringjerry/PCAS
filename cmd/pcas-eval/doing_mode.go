package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/cmd/pcas-eval/fixture"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func doingModeArgs(args []string) ([]string, bool) {
	for i, a := range args {
		if a == "-mode=doing" || a == "--mode=doing" {
			return append(append([]string{}, args[:i]...), args[i+1:]...), true
		}
		if (a == "-mode" || a == "--mode") && i+1 < len(args) && args[i+1] == "doing" {
			return append(append([]string{}, args[:i]...), args[i+2:]...), true
		}
	}
	return nil, false
}

func doingProposeArgs(args []string) ([]string, bool) {
	for i, a := range args {
		if a == "-mode=doing-propose" || a == "--mode=doing-propose" {
			return append(append([]string{}, args[:i]...), args[i+1:]...), true
		}
		if (a == "-mode" || a == "--mode") && i+1 < len(args) && args[i+1] == "doing-propose" {
			return append(append([]string{}, args[:i]...), args[i+2:]...), true
		}
	}
	return nil, false
}

type evalModel struct{ registry *ai.Registry }

func (m evalModel) Generate(ctx context.Context, system, prompt string) (string, error) {
	callctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	out, err := m.registry.Generate(callctx, "v2-answer", system, prompt)
	return out.Text, err
}

type adapterFlags []string

func (a *adapterFlags) String() string     { return strings.Join(*a, ",") }
func (a *adapterFlags) Set(s string) error { *a = append(*a, s); return nil }
func runDoing(args []string) error {
	if doingTierArgs(args) {
		return runDoingTiers(args)
	}
	return runDoingObserved(args, nil)
}

// Only the separate synthetic freeze mode installs this observer. The normal
// doing mode keeps its live per-answer capture and all existing behavior.
func runDoingObserved(args []string, captured func(doing.Task, doing.Evidence, float64) error) error {
	f := flag.NewFlagSet("pcas-eval -mode=doing", flag.ContinueOnError)
	path := f.String("suite", "testdata/phase2_5/doing/suite.json", "frozen synthetic suite or approved private suite")
	dsn := f.String("database-url", "", "empty local disposable database")
	output := f.String("output", "/var/tmp/pcas-v2-eval/report", "metrics-only output prefix outside git repositories")
	fake := f.Bool("fake", false, "CI plumbing only; never quality scores")
	validate := f.Bool("validate", false, "validate suite without database or models")
	workers := f.Int("workers", 4, "bounded model concurrency, 1..8; report records this")
	repeats := f.Int("repeats", 3, "independent complete repetitions; >=3 required for variability")
	filter := f.String("tasks", "", "comma-separated task IDs, for smoke only")
	channel := f.String("channel", "codex", "codex or openai (real calls)")
	modelName := f.String("model", "gpt-6.1-sol", "same model for every answer and both judges")
	codexHome := f.String("codex-home", os.Getenv("PCAS_EVAL_CODEX_HOME"), "existing dedicated signed-in home; no automatic login")
	revision := f.String("revision", "unknown", "evaluated product revision")
	private := f.Bool("private", false, "accept approved real-data suite; files must stay outside repositories")
	methods := f.String("methods", "none,current,ideal", "comma-separated built-ins or adapter names")
	var adapters adapterFlags
	f.Var(&adapters, "context-adapter", "NAME=/absolute/executable; JSON stdin/stdout, no shell")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *repeats < 1 {
		return fmt.Errorf("invalid doing arguments")
	}
	s, err := doing.Load(*path)
	if err != nil {
		return err
	}
	if *private == s.Synthetic {
		return fmt.Errorf("private flag must match suite kind")
	}
	if *private {
		if err = outsideRepository(*path); err != nil {
			return err
		}
	}
	if *validate {
		fmt.Printf("validated memories=%d tasks=%d synthetic=%t\n", len(s.Memories), len(s.Tasks), s.Synthetic)
		return nil
	}
	if err = outsideRepository(*output); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		return err
	}
	if *dsn == "" {
		return fmt.Errorf("doing requires an empty disposable database")
	}
	selected := map[string]bool{}
	if *filter != "" {
		for _, id := range strings.Split(*filter, ",") {
			selected[id] = true
		}
		ts := []doing.Task{}
		for _, t := range s.Tasks {
			if selected[t.ID] {
				ts = append(ts, t)
				delete(selected, t.ID)
			}
		}
		if len(selected) > 0 || len(ts) == 0 {
			return fmt.Errorf("unknown task filter")
		}
		s.Tasks = ts
	}
	ctx := context.Background()
	var model doing.Model = doing.FakeModel{}
	if !*fake {
		p := ai.Provider{ID: "v2-answer", Name: "办事评测", Protocol: *channel, Model: *modelName, MaxOutput: 4096, CostMode: "free"}
		registry := &ai.Registry{HTTP: &http.Client{Timeout: 3 * time.Minute}}
		if *channel == "codex" {
			if *codexHome == "" {
				return fmt.Errorf("set PCAS_EVAL_CODEX_HOME to an existing signed-in dedicated home")
			}
			c, err := ai.NewCodex("", *codexHome)
			if err != nil {
				return err
			}
			defer c.Close()
			registry.Codex = c
		} else if *channel == "openai" {
			p.BaseURL = os.Getenv("PCAS_EVAL_BASE_URL")
			p.KeyEnv = "PCAS_EVAL_API_KEY"
			if p.BaseURL == "" || os.Getenv(p.KeyEnv) == "" {
				return fmt.Errorf("set PCAS_EVAL_BASE_URL and PCAS_EVAL_API_KEY")
			}
		} else {
			return fmt.Errorf("unsupported real channel")
		}
		registry.Config.Providers = []ai.Provider{p}
		model = evalModel{registry}
		// Test channel before any expensive seeding. No personal data is sent.
		if _, err = model.Generate(ctx, "只回复OK；不使用工具。", "通道预检"); err != nil {
			return fmt.Errorf("doing_model_unavailable: verify existing channel login and model access")
		}
	}
	store, pool, cleanup, err := openTemporary(ctx, *dsn)
	if err != nil {
		return err
	}
	defer cleanup()
	// Fake response executes no actions and schedules no extraction; an observer
	// captures the real secretary's supplied memories AND original-source windows.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"reply\":\"评测捕获\",\"used\":[],\"links\":[],\"show\":[],\"remember\":false,\"actions\":[],\"ask\":null}"}}]}`)
	}))
	defer server.Close()
	observed := &fixture.Observer{Base: server.Client().Transport}
	registry := &ai.Registry{HTTP: &http.Client{Transport: observed}, Config: ai.Configuration{Providers: []ai.Provider{{ID: "v2-capture", Name: "虚构检索捕获", Protocol: "openai", BaseURL: server.URL, Model: "v2-capture", CostMode: "free"}}}}
	store.SetModels(registry)
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
	seeded, err := fixture.Seed(ctx, store, pool, c, anchor, "v2-capture", true)
	if err != nil {
		return err
	}
	if *private {
		// Legacy recall fixtures use day offsets. Private copies restore the exact
		// exported timestamps so local-date boundaries and recency are not shifted.
		for _, m := range s.Memories {
			expressed, _ := time.Parse(time.RFC3339, m.ExpressedAt)
			recorded := expressed
			if m.RecordedAt != "" {
				recorded, _ = time.Parse(time.RFC3339, m.RecordedAt)
			}
			source := seeded.Sources[m.ID]
			if _, err = pool.Exec(ctx, `UPDATE record_versions v SET expressed_at=$3,recorded_at=$4
    WHERE v.owner_id=$1 AND (v.record_id=$2 OR v.record_id IN (
     SELECT id FROM chunks WHERE owner_id=$1 AND source_id=$2
     UNION SELECT target_id FROM evidence WHERE owner_id=$1 AND source_id=$2))`, string(seeded.Scope.OwnerID), string(source.ID), expressed, recorded); err != nil {
				return err
			}
		}
	}
	// Baseline identities let us remove all question/answer records before the next
	// retrieval, without altering any frozen source or claim. No workers run.
	if _, err = pool.Exec(ctx, `CREATE TABLE v2_frozen_records AS SELECT owner_id,id FROM memory_records`); err != nil {
		return err
	}
	owner := string(seeded.Scope.OwnerID)
	var captureMu sync.Mutex
	clearTransient := func(ctx context.Context) error {
		for _, query := range []string{`DELETE FROM desk_turns WHERE owner_id=$1`, `DELETE FROM desk_turn_order WHERE owner_id=$1`} {
			if _, err := pool.Exec(ctx, query, owner); err != nil {
				return err
			}
		}
		// Sources from earlier questions must not enter later evidence.
		if _, err := pool.Exec(ctx, `DELETE FROM memory_records r WHERE r.owner_id=$1 AND NOT EXISTS(SELECT 1 FROM v2_frozen_records f WHERE f.owner_id=r.owner_id AND f.id=r.id)`, owner); err != nil {
			return err
		}
		return nil
	}
	adapterDSN := ""
	if len(adapters) > 0 {
		cfg := pool.Config().ConnConfig
		u, parseErr := url.Parse(cfg.ConnString())
		if parseErr != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			return fmt.Errorf("context adapters require a PostgreSQL URI DSN")
		}
		q := u.Query()
		q.Del("search_path")
		q.Set("options", strings.TrimSpace(q.Get("options")+" -c search_path="+cfg.RuntimeParams["search_path"]+" -c default_transaction_read_only=on"))
		u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
		adapterDSN = u.String()
	}
	providers := map[string]doing.Provider{
		"none": {Name: "none", Get: func(context.Context, doing.Suite, doing.Task) (doing.Evidence, error) { return doing.Evidence{}, nil }},
		"ideal": {Name: "ideal", Get: func(_ context.Context, s doing.Suite, t doing.Task) (doing.Evidence, error) {
			return doing.Evidence{Text: doing.IdealEvidence(s, t)}, nil
		}},
		"current": {Name: "current", Get: func(ctx context.Context, _ doing.Suite, t doing.Task) (doing.Evidence, error) {
			captureMu.Lock()
			defer captureMu.Unlock()
			started := time.Now()
			if err := clearTransient(ctx); err != nil {
				return doing.Evidence{}, err
			}
			before := len(observed.Calls())
			out, err := store.DeskTurn(ctx, seeded.Scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "v2-capture", Text: t.Request})
			if err != nil {
				return doing.Evidence{}, err
			}
			calls := observed.Calls()
			if len(calls) != before+1 || len(out.Turn.Receipts) != 0 {
				return doing.Evidence{}, fmt.Errorf("unexpected secretary capture behavior")
			}
			text := fixture.ContextSections(calls[before].Prompt)
			if text == "" {
				return doing.Evidence{}, fmt.Errorf("secretary section format changed")
			}
			evidence := doing.Evidence{Text: text, CaptureCalls: 1}
			if captured != nil {
				if err := captured(t, evidence, float64(time.Since(started))/float64(time.Millisecond)); err != nil {
					return doing.Evidence{}, err
				}
			}
			return evidence, nil
		}},
	}
	for _, spec := range adapters {
		name, path, ok := strings.Cut(spec, "=")
		if !ok || name == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("invalid adapter")
		}
		if _, exists := providers[name]; exists {
			return fmt.Errorf("duplicate method name")
		}
		providers[name] = doing.Provider{Name: name, Get: func(ctx context.Context, s doing.Suite, t doing.Task) (doing.Evidence, error) {
			captureMu.Lock()
			defer captureMu.Unlock()
			if err := clearTransient(ctx); err != nil {
				return doing.Evidence{}, err
			}
			adapterCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(adapterCtx, path)
			cmd.Stdin = strings.NewReader(stringJSON(map[string]any{"request": t.Request, "task_id": t.ID, "as_of": s.AsOf, "database_url": adapterDSN, "owner_id": owner}))
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = io.Discard
			if err := cmd.Run(); err != nil {
				return doing.Evidence{}, fmt.Errorf("adapter failed")
			}
			var e struct {
				Context    string `json:"context"`
				ModelCalls int    `json:"model_calls"`
			}
			if json.Unmarshal(out.Bytes(), &e) != nil || e.ModelCalls < 0 {
				return doing.Evidence{}, fmt.Errorf("invalid adapter output")
			}
			return doing.Evidence{Text: e.Context, ModelCalls: e.ModelCalls}, nil
		}}
	}
	chosen := []doing.Provider{}
	used := map[string]bool{}
	for _, name := range strings.Split(*methods, ",") {
		p, ok := providers[name]
		if !ok || used[name] {
			return fmt.Errorf("unknown or repeated method")
		}
		chosen = append(chosen, p)
		used[name] = true
	}
	raw, _ := os.ReadFile(*path)
	report := doing.Report{Version: 1, Revision: *revision, SuiteSHA: doing.SHA(string(raw)), AnswerPromptSHA: doing.SHA(doing.AnswerSystem), JudgePromptSHA: doing.SHA(doing.JudgeSystem), Model: *modelName, Channel: *channel, Fake: *fake, AsOf: s.AsOf, StartedAt: time.Now().UTC().Format(time.RFC3339), HostDate: time.Now().UTC().Format("2006-01-02"), Repeats: *repeats, Workers: *workers, AnswerLimit: doing.AnswerLimit, Notes: []string{
		"Must and bonus count only when both judges agree true; forbidden counts when either judge says true. Usable also requires both handling judgments and <=600 Unicode characters.",
		"Doing latency includes evidence construction and answer; total includes both judgments. Seeding and one model preflight call are reported outside per-task measurements.",
		"Current uses the actual DeskTurn recall route with a local no-action capture model, followed by the fixed answer instruction. This tests supplied-context strategies, not the complete secretary action executor.",
		"No embedding provider configured: current is the product keyword fallback. Adapter model_calls are declared by the adapter; local capture calls are separate.",
		"Repeatability floor is the maximum of the must and usable percentage-point ranges for that method/category. Differences within the larger compared floor cannot establish superiority; three runs are not a significance test.",
		"The 120 tasks share 20 scenarios; cluster-aware uncertainty and independent human judge audit are still needed. Judge calls use the same model as answers, with independent threads and no method identity.",
		"The product retrieval planner uses host time. Compare runs only on the same host date with unchanged anchored suite; no clock is injected into product code.",
	}}
	if *fake {
		report.Model = "v2-fake"
		report.Channel = "local"
		report.Notes = append(report.Notes, "FAKE: shape and aggregation checks only; all must flags are false by construction, not measured quality.")
	}
	report, err = doing.Execute(ctx, s, model, chosen, report, *output+".partial.json", *workers)
	if err != nil {
		return err
	}
	if err = doing.WriteJSON(*output+".json", report); err != nil {
		return err
	}
	if err = doing.WriteMarkdown(*output+".md", report); err != nil {
		return err
	}
	return os.Remove(*output + ".partial.json")
}
func stringJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// Reject any enclosing git repository, including other worktrees and symlinked
// destinations. Resolve the nearest existing ancestor before creating outputs.
func outsideRepository(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	ancestor := abs
	for {
		if _, err = os.Lstat(ancestor); err == nil {
			break
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return fmt.Errorf("invalid output path")
		}
		ancestor = next
	}
	real, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return err
	}
	for p := real; ; p = filepath.Dir(p) {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return fmt.Errorf("evaluation files must stay outside all git repositories")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
