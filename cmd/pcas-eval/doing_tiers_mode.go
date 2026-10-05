package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/internal/ai"
)

func doingTierArgs(args []string) bool {
	for i, a := range args {
		value := ""
		if strings.HasPrefix(a, "-methods=") || strings.HasPrefix(a, "--methods=") {
			_, value, _ = strings.Cut(a, "=")
		}
		if (a == "-methods" || a == "--methods") && i+1 < len(args) {
			value = args[i+1]
		}
		for _, name := range strings.Split(value, ",") {
			if name == "light" || name == "medium" || name == "heavy" {
				return true
			}
		}
	}
	return false
}

func runDoingTiers(args []string) error {
	f := flag.NewFlagSet("pcas-eval -mode=doing tiers", flag.ContinueOnError)
	path := f.String("suite", "testdata/phase2_5/doing/suite.json", "frozen synthetic or reviewed private suite")
	dsn := f.String("database-url", "", "empty local disposable PostgreSQL URI; CREATEDB required")
	output := f.String("output", "/var/tmp/pcas-v2c-eval/report", "metrics-only output prefix outside repositories")
	fake := f.Bool("fake", false, "plumbing only, not semantic quality")
	validate := f.Bool("validate", false, "validate suite and method selection without calls")
	workers := f.Int("workers", 4, "global model-call concurrency, 1..4 including readers/judges")
	repeats := f.Int("repeats", 3, "complete repetitions")
	filter := f.String("tasks", "", "comma-separated unchanged task IDs")
	channel := f.String("channel", "codex", "codex or openai")
	modelName := f.String("model", "gpt-6.1-sol", "same model for preparation, readers, answer and judges")
	codexHome := f.String("codex-home", os.Getenv("PCAS_EVAL_CODEX_HOME"), "existing dedicated signed-in home")
	revision := f.String("revision", "unknown", "evaluated revision")
	private := f.Bool("private", false, "approved private suite outside repositories")
	methods := f.String("methods", "light,medium,heavy", "product tiers only; reuse baseline reports separately")
	heavyCategories := f.String("heavy-categories", "", "optional category subset for heavy; default all, also in private mode")
	prepareTimeout := f.Duration("prepare-timeout", 12*time.Hour, "preparation deadline including product rate-limit waits")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *workers < 1 || *workers > 4 || *repeats < 1 || *prepareTimeout <= 0 {
		return fmt.Errorf("invalid tier arguments")
	}
	names := []string{}
	seen := map[string]bool{}
	for _, name := range strings.Split(*methods, ",") {
		if seen[name] || (name != "light" && name != "medium" && name != "heavy") {
			return fmt.Errorf("tier run accepts distinct light,medium,heavy only; reuse current/ideal reports")
		}
		seen[name] = true
		names = append(names, name)
	}
	categories := map[string]bool{}
	if *heavyCategories != "" {
		for _, c := range strings.Split(*heavyCategories, ",") {
			valid := false
			for _, known := range doing.Categories {
				if known == c {
					valid = true
				}
			}
			if !valid || categories[c] {
				return fmt.Errorf("invalid heavy category subset")
			}
			categories[c] = true
		}
		if !seen["heavy"] {
			return fmt.Errorf("heavy category subset requires heavy")
		}
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
		fmt.Printf("validated memories=%d tasks=%d synthetic=%t methods=%s\n", len(s.Memories), len(s.Tasks), s.Synthetic, *methods)
		return nil
	}
	if *filter != "" {
		wanted := map[string]bool{}
		for _, id := range strings.Split(*filter, ",") {
			if wanted[id] {
				return fmt.Errorf("repeated task filter")
			}
			wanted[id] = true
		}
		tasks := []doing.Task{}
		for _, t := range s.Tasks {
			if wanted[t.ID] {
				tasks = append(tasks, t)
				delete(wanted, t.ID)
			}
		}
		if len(wanted) > 0 || len(tasks) == 0 {
			return fmt.Errorf("unknown task filter")
		}
		s.Tasks = tasks
	}
	for _, name := range names {
		count := 0
		for _, t := range s.Tasks {
			if name != "heavy" || len(categories) == 0 || categories[t.Category] {
				count++
			}
		}
		if count == 0 {
			return fmt.Errorf("method has no selected tasks")
		}
	}
	if *dsn == "" {
		return fmt.Errorf("tiers require an empty disposable database")
	}
	if err = outsideRepository(*output); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	model, closeModel, err := newTierEvalModel(*fake, *channel, *modelName, *codexHome, *workers)
	if err != nil {
		return err
	}
	defer closeModel()
	preflight := 0
	if !*fake {
		if _, err = model.Generate(ctx, "只回复OK；不使用工具。", "通道预检"); err != nil {
			return fmt.Errorf("doing_model_unavailable: verify existing channel login and access")
		}
		preflight = 1
	}
	store, pool, cleanup, err := openTemporary(ctx, *dsn)
	if err != nil {
		return err
	}
	defer cleanup()
	bridge := newTierBridge(model, *fake)
	defer bridge.server.Close()
	store.SetModels(bridge.registry())
	seeded, err := seedTierSuite(ctx, store, pool, s)
	if err != nil {
		return fmt.Errorf("tier_seed_failed")
	}
	prepareCtx, prepareCancel := context.WithTimeout(ctx, *prepareTimeout)
	preparation, err := prepareTierSuite(prepareCtx, store, pool, seeded.Scope, bridge, *output+".prepare.json")
	prepareCancel()
	if err != nil {
		return err
	}
	// Save the isolated URI (including search_path); then release every template
	// connection before PostgreSQL copies it. This never dumps private text.
	preparedDSN := pool.Config().ConnConfig.ConnString()
	store.Close()
	pool.Close()
	databases, cleanupCopies, err := cloneTierDatabases(ctx, preparedDSN, seeded.Scope, *workers, model, *fake)
	if err != nil {
		return err
	}
	defer cleanupCopies()
	providers := []doing.Provider{}
	for _, name := range names {
		var subset map[string]bool
		if name == "heavy" {
			subset = categories
		}
		providers = append(providers, tierProvider(name, databases, subset))
	}
	raw, _ := os.ReadFile(*path)
	r := doing.Report{Version: 1, Revision: *revision, SuiteSHA: doing.SHA(string(raw)), AnswerPromptSHA: doing.SHA(doing.AnswerSystem), JudgePromptSHA: doing.SHA(doing.JudgeSystem), Model: *modelName, Channel: *channel, Fake: *fake, AsOf: s.AsOf, StartedAt: time.Now().UTC().Format(time.RFC3339), HostDate: time.Now().UTC().Format("2006-01-02"), Repeats: *repeats, Workers: *workers, AnswerLimit: doing.AnswerLimit, Preparation: &preparation, Notes: []string{
		"Product DeskTurn with WithMemoryTier constructs context, selects/reads groups, validates the reply and performs real selfcheck. Main answer uses the unchanged frozen AnswerSystem; the initial envelope deliberately has no actions/remember/missingKeyInfo. This remains a draft-result evaluation, not a complete secretary executor or automatic tier-selection test.",
		"Selfcheck uses the native product instruction/schema, including its reply formatting and deadline. It can revise text; it cannot add actions to the empty initial action list. The judges score the final returned reply with unchanged gold, two independent calls and the 600-character cap.",
		"Preparation ran product scheduling/queue handlers for organize, group/entity compare, cards and handover. Every actual preparation call and wall time, including throttle waits, is recorded separately; no timestamp/label/gold bypass was used.",
		"Each worker uses its own PostgreSQL TEMPLATE copy of the same completed preparation. Question/answer sources, orders and candidates are cleared before the next task. No background workers run during answering and no embedding provider is configured.",
		"All model calls, including parallel heavy readers and judges, share the reported global concurrency limit. Product reader/selfcheck timeouts include waiting for that limit; fallback is an observed outcome, not a retry by the evaluator.",
		fmt.Sprintf("Outside row costs: preflight_calls=%d; preparation recorded separately. Rows count product attempted calls, including fallbacks/retries, plus the two judge attempts.", preflight),
		"Product retrieval/date validation/deadlines use host time while the main answer and gold retain the frozen suite as_of. Reused historical current/ideal scores have a different host date and concurrency; numerical differences do not by themselves establish a causal or performance improvement.",
	}}
	if *fake {
		r.Model = "v2-fake"
		r.Channel = "local"
		r.Notes = append(r.Notes, "FAKE: shape only; all must flags false, never quality scores.")
	}
	r, err = doing.Execute(ctx, s, model, providers, r, *output+".partial.json", *workers)
	if err != nil {
		return err
	}
	if err = doing.WriteJSON(*output+".json", r); err != nil {
		return err
	}
	if err = doing.WriteMarkdown(*output+".md", r); err != nil {
		return err
	}
	return os.Remove(*output + ".partial.json")
}

func newTierEvalModel(fake bool, channel, name, home string, workers int) (doing.Model, func(), error) {
	var model doing.Model = doing.FakeModel{}
	closeModel := func() {}
	if !fake {
		p := ai.Provider{ID: "v2-answer", Name: "办事评测", Protocol: channel, Model: name, MaxOutput: 4096, CostMode: "free"}
		registry := &ai.Registry{HTTP: &http.Client{Timeout: 3 * time.Minute}}
		if channel == "codex" {
			if home == "" {
				return nil, closeModel, fmt.Errorf("set PCAS_EVAL_CODEX_HOME to an existing dedicated signed-in home")
			}
			c, err := ai.NewCodex("", home)
			if err != nil {
				return nil, closeModel, err
			}
			registry.Codex = c
			closeModel = c.Close
		} else if channel == "openai" {
			p.BaseURL = os.Getenv("PCAS_EVAL_BASE_URL")
			p.KeyEnv = "PCAS_EVAL_API_KEY"
			if p.BaseURL == "" || os.Getenv(p.KeyEnv) == "" {
				return nil, closeModel, fmt.Errorf("set PCAS_EVAL_BASE_URL and PCAS_EVAL_API_KEY")
			}
		} else {
			return nil, closeModel, fmt.Errorf("unsupported real channel")
		}
		registry.Config.Providers = []ai.Provider{p}
		model = evalModel{registry}
	}
	return &tierLimitedModel{model: model, slots: make(chan struct{}, workers)}, closeModel, nil
}
