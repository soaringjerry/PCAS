// doing-tiers-report compares completed tier runs with the frozen old baseline.
// It does not call a model, connect to a database, or accept private suites.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strings"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

type source struct {
	FileSHA       string `json:"file_sha256"`
	DecodedSHA    string `json:"decoded_sha256"`
	Revision      string `json:"revision"`
	HostDate      string `json:"host_date"`
	StartedAt     string `json:"started_at"`
	Workers       int    `json:"workers"`
	RetainedRows  int    `json:"retained_rows"`
	RetainedCalls int    `json:"retained_calls"`
}

type scope struct {
	Tasks     int             `json:"tasks_per_repeat"`
	Summaries []doing.Summary `json:"summaries"`
	Ranges    []doing.Range   `json:"ranges"`
}

type usage struct {
	Rows          int            `json:"rows"`
	ModelCalls    int            `json:"model_calls_including_two_judges"`
	Calls         map[string]int `json:"product_calls"`
	Failed        map[string]int `json:"product_failed"`
	NotStarted    map[string]int `json:"product_not_started"`
	Malformed     map[string]int `json:"product_malformed"`
	InputChars    map[string]int `json:"product_input_chars"`
	Effective     map[string]int `json:"effective_tiers"`
	SelfcheckBack int            `json:"observed_selfcheck_fallbacks"`
}

type comparison struct {
	SuiteSHA    string             `json:"suite_sha256"`
	Sources     map[string]source  `json:"sources"`
	Preparation *doing.Preparation `json:"preparation"`
	Scopes      map[string]scope   `json:"scopes"`
	Usage       map[string]*usage  `json:"tier_usage"`
	Missing     []string           `json:"not_measured"`
}

func read(path string, dst any) (source, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return source{}, err
	}
	s := source{FileSHA: doing.SHA(string(b))}
	if strings.HasSuffix(path, ".gz") {
		z, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return s, err
		}
		b, err = io.ReadAll(z)
		z.Close()
		if err != nil {
			return s, err
		}
	}
	s.DecodedSHA = doing.SHA(string(b))
	return s, json.Unmarshal(b, dst)
}

func heavy(t doing.Task) bool { return t.Category == "cross_group" || t.Category == "outgoing" }

func validate(s doing.Suite, sha string, r doing.Report, tiers bool) error {
	if !s.Synthetic || len(s.Tasks) != 120 || len(s.Memories) != 671 {
		return fmt.Errorf("only the unchanged fictional old120 suite is accepted")
	}
	if r.Fake || r.Version != 1 || r.Repeats != 3 || len(r.Failures) != 0 || r.SuiteSHA != sha || r.AsOf != s.AsOf || r.AnswerLimit != doing.AnswerLimit || r.AnswerPromptSHA != doing.SHA(doing.AnswerSystem) || r.JudgePromptSHA != doing.SHA(doing.JudgeSystem) {
		return fmt.Errorf("incomplete run, fake run, or execution fingerprint mismatch")
	}
	methods := []string{"none", "current", "ideal"}
	if tiers {
		methods = []string{"light", "medium", "heavy"}
		if r.Workers < 1 || r.Workers > 4 || r.Preparation == nil || !r.Preparation.Complete || r.Preparation.HandoverInputs && !r.Preparation.Handover || len(r.Preparation.Stages) != 3 {
			return fmt.Errorf("tier run needs completed native preparation and concurrency <= 4")
		}
		for i, name := range []string{"organize", "compare", "status"} {
			stage := r.Preparation.Stages[i]
			if stage.Stage != name || stage.JobsDone == 0 || stage.WallMS <= 0 {
				return fmt.Errorf("missing preparation stage %s", name)
			}
		}
	}
	tasks := map[string]doing.Task{}
	expected := map[string]bool{}
	key := func(run int, method, task string) string { return fmt.Sprintf("%d/%s/%s", run, method, task) }
	for _, t := range s.Tasks {
		tasks[t.ID] = t
		for _, method := range methods {
			if tiers && method == "heavy" && !heavy(t) {
				continue
			}
			for run := 1; run <= 3; run++ {
				expected[key(run, method, t.ID)] = true
			}
		}
	}
	for _, row := range r.Rows {
		k := key(row.Run, row.Method, row.Task)
		t := tasks[row.Task]
		if !expected[k] || t.Category != row.Category {
			return fmt.Errorf("unexpected or duplicate row %s", k)
		}
		delete(expected, k)
		for _, j := range row.Judgments {
			b, _ := json.Marshal(j)
			if _, err := doing.ParseJudgment(string(b), t); err != nil {
				return fmt.Errorf("invalid judgment %s: %w", k, err)
			}
		}
		score := doing.Score(t, row.Judgments)
		score.Usable = score.Usable && row.AnswerChars <= doing.AnswerLimit
		if score.MustTotal != row.MustTotal || score.MustBoth != row.MustBoth || score.BonusTotal != row.BonusTotal || score.BonusBoth != row.BonusBoth || score.ForbiddenTotal != row.ForbiddenTotal || score.ForbiddenEither != row.ForbiddenEither || score.Usable != row.Usable || !reflect.DeepEqual(score.Disagreements, row.Disagreements) {
			return fmt.Errorf("stored score differs from double judgments %s", k)
		}
		for _, v := range []float64{row.DoingMS, row.TotalMS, row.EvidenceMS, row.AnswerMS, row.JudgeMS} {
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("invalid timing %s", k)
			}
		}
		if tiers {
			u := row.TierUsage
			if u == nil || u.Requested != row.Method {
				return fmt.Errorf("missing product tier usage %s", k)
			}
			calls := 2 // frozen independent double judgment, no extra answer call
			for _, n := range u.Calls {
				if n < 0 {
					return fmt.Errorf("negative calls %s", k)
				}
				calls += n
			}
			if calls != row.ModelCalls {
				return fmt.Errorf("call ledger mismatch %s", k)
			}
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("missing %d rows", len(expected))
	}
	return nil
}

func compare(s doing.Suite, sha string, base, tiers doing.Report) (comparison, error) {
	c := comparison{SuiteSHA: sha, Scopes: map[string]scope{}, Usage: map[string]*usage{}, Missing: []string{"heavy/direct_recall", "new48/all_methods"}}
	if err := validate(s, sha, base, false); err != nil {
		return c, fmt.Errorf("baseline: %w", err)
	}
	if err := validate(s, sha, tiers, true); err != nil {
		return c, fmt.Errorf("tiers: %w", err)
	}
	if base.Model != tiers.Model || base.Channel != tiers.Channel {
		return c, fmt.Errorf("model/channel changed; do not combine into this comparison")
	}
	c.Preparation = tiers.Preparation
	full, hard := []doing.Row{}, []doing.Row{}
	for _, r := range []doing.Report{base, tiers} {
		for _, row := range r.Rows {
			if row.Method == "none" {
				continue
			}
			if row.Method != "heavy" {
				full = append(full, row)
			}
			if heavy(doing.Task{Category: row.Category}) {
				hard = append(hard, row)
			}
		}
	}
	for name, rows := range map[string][]doing.Row{"old120": full, "cross_outgoing40": hard} {
		summary, ranges := doing.Aggregate(rows, 3)
		n := 120
		if name == "cross_outgoing40" {
			n = 40
		}
		c.Scopes[name] = scope{Tasks: n, Summaries: summary, Ranges: ranges}
	}
	for _, row := range tiers.Rows {
		u := c.Usage[row.Method]
		if u == nil {
			u = &usage{Calls: map[string]int{}, Failed: map[string]int{}, NotStarted: map[string]int{}, Malformed: map[string]int{}, InputChars: map[string]int{}, Effective: map[string]int{}}
			c.Usage[row.Method] = u
		}
		u.Rows++
		u.ModelCalls += row.ModelCalls
		v := row.TierUsage
		for _, pair := range [][2]map[string]int{{u.Calls, v.Calls}, {u.Failed, v.Failed}, {u.NotStarted, v.NotStarted}, {u.Malformed, v.Malformed}, {u.InputChars, v.InputChars}} {
			for k, n := range pair[1] {
				pair[0][k] += n
			}
		}
		u.Effective[v.Effective]++
		if v.SelfcheckFallback {
			u.SelfcheckBack++
		}
	}
	return c, nil
}

func run() error {
	suitePath := flag.String("suite", "testdata/phase2_5/doing/suite.json", "unchanged fictional old120 suite")
	basePath := flag.String("baseline", "docs/evaluations/2026-10-04-phase2_5-v2-artifacts/result.json.gz", "existing baseline, never rerun")
	tierPath := flag.String("tiers", "", "completed metrics-only tier report (JSON or gzip)")
	out := flag.String("output", "", "new comparison prefix")
	flag.Parse()
	if flag.NArg() != 0 || *tierPath == "" || *out == "" {
		return fmt.Errorf("tiers and output required; no positional arguments")
	}
	s, err := doing.Load(*suitePath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*suitePath)
	if err != nil {
		return err
	}
	var base, tiers doing.Report
	a, err := read(*basePath, &base)
	if err != nil {
		return err
	}
	b, err := read(*tierPath, &tiers)
	if err != nil {
		return err
	}
	c, err := compare(s, doing.SHA(string(raw)), base, tiers)
	if err != nil {
		return err
	}
	for _, pair := range []struct {
		s *source
		r doing.Report
	}{{&a, base}, {&b, tiers}} {
		pair.s.Revision, pair.s.HostDate, pair.s.StartedAt, pair.s.Workers = pair.r.Revision, pair.r.HostDate, pair.r.StartedAt, pair.r.Workers
		for _, row := range pair.r.Rows {
			if row.Method != "none" {
				pair.s.RetainedRows++
				pair.s.RetainedCalls += row.ModelCalls
			}
		}
	}
	c.Sources = map[string]source{"reused_baseline": a, "new_tiers": b}
	return doing.WriteJSON(*out+".json", c)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
