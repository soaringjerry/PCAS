// Recalculate the unchanged old tasks, new independent tasks and their union.
// Inputs and outputs contain scores and identifiers, never generated answers.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func read(path string, v any) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(path, ".gz") {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return "", err
		}
		b, err = io.ReadAll(r)
		r.Close()
		if err != nil {
			return "", err
		}
	}
	return doing.SHA(string(b)), json.Unmarshal(b, v)
}
func unchanged(old, extended doing.Suite) error {
	if !old.Synthetic || !extended.Synthetic || len(old.Tasks) != 120 || len(extended.Tasks) != 168 || len(old.Memories) != 671 || len(extended.Memories) != 801 {
		return fmt.Errorf("unexpected synthetic cohort sizes")
	}
	if !reflect.DeepEqual(old.Tasks, extended.Tasks[:120]) || !reflect.DeepEqual(old.Memories, extended.Memories[:671]) {
		return fmt.Errorf("old task or memory prefix changed")
	}
	x := extended
	x.Tasks = old.Tasks
	x.Memories = old.Memories
	if !reflect.DeepEqual(x, old) {
		return fmt.Errorf("old suite conditions changed")
	}
	return nil
}
func validate(r doing.Report, s doing.Suite, suiteSHA string, snap doing.ContextSnapshot) error {
	if r.Version != 1 || r.SuiteSHA != suiteSHA || snap.SuiteSHA != suiteSHA || r.AsOf != s.AsOf || snap.AsOf != s.AsOf || r.HostDate != snap.HostDate || r.Repeats != 3 || r.AnswerLimit != doing.AnswerLimit || r.AnswerPromptSHA != doing.SHA(doing.AnswerSystem) || r.JudgePromptSHA != doing.SHA(doing.JudgeSystem) || len(r.Failures) != 0 {
		return fmt.Errorf("execution conditions mismatch or failures remain")
	}
	tasks := map[string]doing.Task{}
	contexts := map[string]doing.ContextEntry{}
	for _, t := range s.Tasks {
		tasks[t.ID] = t
	}
	for _, e := range snap.Entries {
		t, ok := tasks[e.Task]
		if !ok || e.RequestSHA != doing.SHA(t.Request) {
			return fmt.Errorf("snapshot request mismatch")
		}
		contexts[e.Task] = e
	}
	if len(contexts) != len(tasks) {
		return fmt.Errorf("snapshot incomplete")
	}
	seen := map[string]bool{}
	for _, row := range r.Rows {
		t, ok := tasks[row.Task]
		k := fmt.Sprintf("%d/%s/%s", row.Run, row.Task, row.Method)
		if !ok || row.Run < 1 || row.Run > 3 || seen[k] || (row.Method != "none" && row.Method != "frozen-current" && row.Method != "ideal") {
			return fmt.Errorf("row matrix invalid")
		}
		seen[k] = true
		for _, j := range row.Judgments {
			b, _ := json.Marshal(j)
			if _, err := doing.ParseJudgment(string(b), t); err != nil {
				return err
			}
		}
		sc := doing.Score(t, row.Judgments)
		if row.Category != t.Category || row.MustTotal != sc.MustTotal || row.MustBoth != sc.MustBoth || row.BonusTotal != sc.BonusTotal || row.BonusBoth != sc.BonusBoth || row.ForbiddenTotal != sc.ForbiddenTotal || row.ForbiddenEither != sc.ForbiddenEither || row.Usable != (sc.Usable && row.AnswerChars <= doing.AnswerLimit) || !reflect.DeepEqual(row.Disagreements, sc.Disagreements) || row.ModelCalls != 3 || row.CaptureCalls != 0 || row.AnswerChars < 0 || row.EvidenceMS < 0 || row.TotalMS < 0 {
			return fmt.Errorf("invalid scored row %s", k)
		}
		contextText := ""
		if row.Method == "frozen-current" {
			contextText = contexts[row.Task].Text
		}
		if row.Method == "ideal" {
			contextText = doing.IdealEvidence(s, t)
		}
		if row.ContextChars != utf8.RuneCountInString(contextText) || row.InputChars != utf8.RuneCountInString(doing.AnswerSystem+doing.AnswerPrompt(s, t, contextText)) {
			return fmt.Errorf("context/input size mismatch")
		}
	}
	if len(r.Rows) != len(tasks)*9 {
		return fmt.Errorf("incomplete three-method three-run matrix")
	}
	return nil
}
func split(r doing.Report, old doing.Suite) map[string]doing.Report {
	ids := map[string]bool{}
	for _, t := range old.Tasks {
		ids[t.ID] = true
	}
	out := map[string]doing.Report{}
	for _, cohort := range []string{"old120", "new48", "all168"} {
		v := r
		v.Rows = []doing.Row{}
		for _, row := range r.Rows {
			if cohort == "old120" && !ids[row.Task] || cohort == "new48" && ids[row.Task] {
				continue
			}
			v.Rows = append(v.Rows, row)
		}
		sort.Slice(v.Rows, func(i, j int) bool {
			a, b := v.Rows[i], v.Rows[j]
			return fmt.Sprintf("%d/%s/%s", a.Run, a.Method, a.Task) < fmt.Sprintf("%d/%s/%s", b.Run, b.Method, b.Task)
		})
		v.Summaries, v.Ranges = doing.Aggregate(v.Rows, 3)
		v.Notes = []string{
			"Cohort=" + cohort + ". Old tasks are unchanged but evaluated against the extended 801-memory corpus. frozen-current replays the actual product route captured once per task on the suite's UTC date; row evidence_ms is replay overhead, not retrieval latency. Raw method label is retained. See capture-manifest.json for one-off retrieval cost and provenance.",
			"Must and bonus require both independent judgments; forbidden counts either judgment. Usable requires all must, zero forbidden, both handling and at most 600 Unicode characters. Instructions and model are unchanged from V2.",
			"Initial capture measures actual DeskTurn keyword fallback without embeddings; seeded flat facts and no-action local capture remain the V2 protocol. Three model repetitions reuse the fixed snapshot; no retrieval variability or complete action execution is measured.",
			"The old120 cohort contains 20 shared scenarios; new48 contains independently authored requests. The min-max floor is descriptive repeatability, not statistical significance. Both judges use the same model family as the answers. See the evaluation record for start/end dates, clock limitations and any interrupted attempts.",
		}
		if r.Fake {
			v.Notes = append(v.Notes, "FAKE plumbing only; not measured quality.")
		}
		out[cohort] = v
	}
	return out
}

type presence struct {
	Memory   string `json:"memory_id"`
	FullText bool   `json:"full_original_text_present"`
}
type exposure struct {
	Task     string     `json:"task"`
	Memories []presence `json:"memories"`
}

// Exact full-original-text presence measures exposure, not semantic use. It
// discloses only synthetic memory identifiers and booleans, never the text.
func noiseExposure(s doing.Suite, snapshot doing.ContextSnapshot) []exposure {
	by := s.ByID()
	contexts := map[string]string{}
	for _, e := range snapshot.Entries {
		contexts[e.Task] = e.Text
	}
	out := []exposure{}
	for _, task := range s.Tasks {
		if !strings.HasPrefix(task.ID, "I-NOISE-") {
			continue
		}
		refs := map[string]bool{}
		for _, check := range task.Forbidden {
			for _, id := range check.Evidence {
				refs[id] = true
			}
		}
		ids := []string{}
		for id := range refs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		e := exposure{Task: task.ID, Memories: []presence{}}
		for _, id := range ids {
			e.Memories = append(e.Memories, presence{id, strings.Contains(contexts[task.ID], by[id].Text)})
		}
		out = append(out, e)
	}
	return out
}
func run() error {
	f := flag.NewFlagSet("doing-cohorts", flag.ContinueOnError)
	base := f.String("base-suite", "testdata/phase2_5/doing/suite.json", "unchanged old suite")
	suite := f.String("suite", "testdata/phase2_5/doing-independent/suite.json", "extended suite")
	source := f.String("source", "", "complete numeric report, JSON or gzip")
	snapshot := f.String("snapshot", "", "outside-Git synthetic context snapshot")
	out := f.String("output", "", "new numeric artifact directory")
	fake := f.Bool("allow-fake", false, "explicit plumbing only; never real scores")
	if err := f.Parse(os.Args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 || *source == "" || *snapshot == "" || *out == "" {
		return fmt.Errorf("source, snapshot and output required")
	}
	old, err := doing.Load(*base)
	if err != nil {
		return err
	}
	s, err := doing.Load(*suite)
	if err != nil {
		return err
	}
	if err = unchanged(old, s); err != nil {
		return err
	}
	snap, err := doing.LoadSnapshot(*snapshot)
	if err != nil {
		return err
	}
	var r doing.Report
	sourceSHA, err := read(*source, &r)
	if err != nil {
		return err
	}
	if r.Fake != *fake {
		return fmt.Errorf("fake report requires explicit allow-fake; real report cannot use it")
	}
	raw, err := os.ReadFile(*suite)
	if err != nil {
		return err
	}
	if err = validate(r, s, doing.SHA(string(raw)), snap); err != nil {
		return err
	}
	if err = os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	if err = doing.WriteJSON(filepath.Join(*out, "noise-exposure.json"), noiseExposure(s, snap)); err != nil {
		return err
	}
	for name, report := range split(r, old) {
		if err = doing.WriteJSON(filepath.Join(*out, name+".json"), report); err != nil {
			return err
		}
		if err = doing.WriteMarkdown(filepath.Join(*out, name+".md"), report); err != nil {
			return err
		}
	}
	// Strip context text before producing the shareable capture manifest.
	type numericEntry struct {
		Task       string  `json:"task"`
		RequestSHA string  `json:"request_sha256"`
		ContextSHA string  `json:"context_sha256"`
		Chars      int     `json:"context_chars"`
		MS         float64 `json:"capture_ms"`
		Calls      int     `json:"local_capture_calls"`
	}
	entries := []numericEntry{}
	for _, e := range snap.Entries {
		entries = append(entries, numericEntry{e.Task, e.RequestSHA, e.ContextSHA, e.ContextChars, e.CaptureMS, e.CaptureCalls})
	}
	snapshotBytes, err := os.ReadFile(*snapshot)
	if err != nil {
		return err
	}
	baseBytes, err := os.ReadFile(*base)
	if err != nil {
		return err
	}
	return doing.WriteJSON(filepath.Join(*out, "capture-manifest.json"), struct {
		Schema          int            `json:"schema_version"`
		Fake            bool           `json:"fake"`
		SourceSHA       string         `json:"source_uncompressed_sha256"`
		SnapshotSHA     string         `json:"snapshot_sha256"`
		SuiteSHA        string         `json:"suite_sha256"`
		BaseSHA         string         `json:"base_suite_sha256"`
		CaptureRevision string         `json:"capture_product_revision"`
		ModelRevision   string         `json:"model_run_revision"`
		AsOf            string         `json:"as_of"`
		CaptureStart    string         `json:"capture_started_at"`
		CaptureEnd      string         `json:"capture_completed_at"`
		ModelStart      string         `json:"model_run_started_at"`
		Entries         []numericEntry `json:"entries"`
		Note            string         `json:"note"`
	}{1, r.Fake, sourceSHA, doing.SHA(string(snapshotBytes)), r.SuiteSHA, doing.SHA(string(baseBytes)), snap.Revision, r.Revision, snap.AsOf, snap.StartedAt, snap.CompletedAt, r.StartedAt, entries, "Snapshot text is synthetic and stays outside Git. Hashes refer to uncompressed JSON except snapshot_sha256 (exact bytes). Capture is once per task; no retrieval variability is measured. Source report has zero local captures per answer and three real model calls per completed answer; preflight and interrupted work are accounted separately in the evaluation record."})
}
