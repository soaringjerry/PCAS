package app

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/app/stagelimits"
)

type observeLabelCatalog struct {
	Functions      map[string]string `json:"functions"`
	Reasons        map[string]string `json:"reasons"`
	ReasonPrefixes map[string]string `json:"reasonPrefixes"`
}

func loadObserveLabels(t *testing.T, root string) observeLabelCatalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "web", "src", "domain", "observe-labels.json"))
	if err != nil {
		t.Fatalf("read observe-labels.json: %v", err)
	}
	var cat observeLabelCatalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatalf("parse observe-labels.json: %v", err)
	}
	return cat
}

func latestMigrationEnum(t *testing.T, root, constraint string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "internal", "postgres", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	re := regexp.MustCompile(constraint + `\s+CHECK\s*\([^)]*IN\s*\(([^)]+)\)`)
	var latest string
	for _, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if m := re.FindSubmatch(b); m != nil {
			latest = string(m[1])
		}
	}
	if latest == "" {
		t.Fatalf("constraint %s not found in migrations", constraint)
	}
	itemRe := regexp.MustCompile(`'([^']+)'`)
	found := itemRe.FindAllStringSubmatch(latest, -1)
	out := make([]string, 0, len(found))
	for _, m := range found {
		out = append(out, m[1])
	}
	return out
}

func TestObserveLabelsCoverAllBackendFunctionsStagesAndReasons(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cat := loadObserveLabels(t, root)

	// 1. Collect every registered function/purpose/source/stage from authoritative backend sources.
	registeredFunctions := map[string]string{}
	for _, r := range Responses {
		registeredFunctions[r.Stage] = "app.Responses"
	}
	for _, purpose := range latestMigrationEnum(t, root, "model_usage_purpose_check") {
		registeredFunctions[purpose] = "model_usage_purpose_check"
	}
	for _, source := range latestMigrationEnum(t, root, "action_log_source_check") {
		registeredFunctions[source] = "action_log_source_check"
	}

	// Scan all non-test Go files under internal/ for literal Function:/Purpose: and reason codes.
	funcLitRe := regexp.MustCompile(`(?:Function|Purpose):\s*"([^"]+)"`)
	overflowRe := regexp.MustCompile(`Overflow\([^,\n]+,\s*"([a-z0-9_.]+)"`)
	eventRe := regexp.MustCompile(`\.Event\(\s*"(?:overflow|failure|deferred)"\s*,\s*"([a-z0-9_.]+)"`)
	jobErrRe := regexp.MustCompile(`JobError\{Code:\s*"([a-z0-9_.]+)"`)
	hourlyRe := regexp.MustCompile(`HourlyLimit\([^)]+,\s*"([a-z0-9_.]+)"\)`)
	recoverySQLRe := regexp.MustCompile(`recovery_reason='([a-z0-9_.]+)'`)
	appReasonSQLRe := regexp.MustCompile(`'applicationReason','([a-z0-9_.]+)'`)
	stageLitRe := regexp.MustCompile(`StageEvent\([^,\n]+,[^,\n]+,[^,\n]+,\s*(?:"([a-z0-9_.]+)"|[^,\n]+),\s*"(?:overflow|failure|deferred)"\s*,\s*"([a-z0-9_.]+)"`)

	requiredReasons := map[string]string{
		"no_call_record":                    "activity_day.go",
		"chain_limit":                       "event_limits.go",
		"schedule_background_write_busy":    "background_health.go",
		"task_context_limit":                "secretary/compose.go",
		"project_context_limit":             "secretary/compose.go",
		"idea_context_limit":                "secretary/compose.go",
		"recall_budget":                     "secretary/compose.go",
		"source_recall_budget":              "secretary/compose.go",
		"scoped_requirement_left_to_recall": "contextbuilder/use.go",
		"global_requirement_char_budget":    "contextbuilder/use.go",
		"topic_project_uncertain":           "topicproject/service.go",
		"topic_project_output_invalid":      "topicproject/service.go",
		"date_tidy_output_invalid":          "datereview/service.go",
		"handover_interval":                 "handover/service.go",
		"handover_invalid_output":           "handover/service.go",
		"invalid_entity_candidates_output":  "aliasscan/service.go",
		"invalid_entity_compare_output":     "comparison/entity_service.go",
		"identity_input_changed":            "comparison/entity_service.go",
		"identity_name_changed":             "comparison/entity_service.go",
		"identity_sample_memory_limit":      "interpstore/compare_entities.go",
		"selfcheck_output_rejected":         "deputy/selfcheck.go",
		"selfcheck_interrupted":             "deputy/conduct.go",
		"run_failed_or_stale":               "deputy/finish.go",
		"revision_not_adopted":              "deputy/finish.go",
		"deadline_exceeded":                 "budgetstore/background_health.go",
		"canceled":                          "budgetstore/background_health.go",
		"timeout":                           "secretary/rules.go",
		"unsupported_capability":            "secretary/rules.go",
		"unknown_outcome":                   "secretary/rules.go",
		"budget_exceeded":                   "secretary/rules.go",
		"unavailable":                       "secretary/rules.go",
		"conflict":                          "secretary/rules.go",
		"no_json_object":                    "secretary/rules.go",
		"invalid_field_type":                "secretary/rules.go",
		"invalid_json":                      "secretary/rules.go",
		"invalid_input":                     "secretary/rules.go",
		"reorganize_deadlines_withdrawn":    "budgetstore/background_health.go",
		"reorganize_requirements_withdrawn": "budgetstore/background_health.go",
		"unknown_outcome_recovery":          "callstore/model_calls.go",
		"model_provider_not_available":      "modelcall/gateway.go",
	}

	// Hourly limits from stagelimits.Hourly() match budgetstore/budget.go rule.
	for stage := range stagelimits.Hourly() {
		reason := strings.TrimPrefix(stage, "memory.") + "_hourly_limit"
		if stage == "memory.handover" {
			reason = "memory.handover_hourly_limit"
		}
		requiredReasons[reason] = "stagelimits.Hourly"
	}

	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		text := string(b)
		for _, m := range funcLitRe.FindAllStringSubmatch(text, -1) {
			registeredFunctions[m[1]] = rel
		}
		for _, re := range []*regexp.Regexp{overflowRe, eventRe, jobErrRe, hourlyRe, recoverySQLRe, appReasonSQLRe} {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				requiredReasons[m[1]] = rel
			}
		}
		for _, m := range stageLitRe.FindAllStringSubmatch(text, -1) {
			if m[1] != "" {
				registeredFunctions[m[1]] = rel
			}
			if m[2] != "" {
				requiredReasons[m[2]] = rel
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify every registered backend function/stage has a non-empty label.
	for name, src := range registeredFunctions {
		if strings.TrimSpace(cat.Functions[name]) == "" {
			t.Errorf("missing Chinese function/stage label in observe-labels.json for %q (from %s)", name, src)
		}
	}
	// Verify observe-labels.json contains no invented/unregistered function names.
	for name := range cat.Functions {
		if _, ok := registeredFunctions[name]; !ok {
			t.Errorf("observe-labels.json contains unregistered/guessed function name %q", name)
		}
	}

	// Verify every required backend reason has a label or an explicit prefix handler.
	for reason, src := range requiredReasons {
		if strings.TrimSpace(cat.Reasons[reason]) != "" {
			continue
		}
		covered := false
		for prefix := range cat.ReasonPrefixes {
			if strings.HasPrefix(reason, prefix) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("missing Chinese reason label in observe-labels.json for %q (from %s)", reason, src)
		}
	}
}
