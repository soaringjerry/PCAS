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
	"time"
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
	return validateMeasured(r, s, suiteSHA, snap, nil)
}
func validateMeasured(r doing.Report, s doing.Suite, suiteSHA string, snap doing.ContextSnapshot, jsonSizes map[string]int) error {
	return validateMatrix(r, s, suiteSHA, snap, jsonSizes, 3, true)
}
func validateMatrix(r doing.Report, s doing.Suite, suiteSHA string, snap doing.ContextSnapshot, jsonSizes map[string]int, repeats int, complete bool) error {
	started, dateErr := time.Parse(time.RFC3339, r.StartedAt)
	if dateErr != nil || r.HostDate != started.UTC().Format("2006-01-02") || r.Version != 1 || r.SuiteSHA != suiteSHA || snap.SuiteSHA != suiteSHA || r.AsOf != s.AsOf || snap.AsOf != s.AsOf || r.Repeats != repeats || r.AnswerLimit != doing.AnswerLimit || r.AnswerPromptSHA != doing.SHA(doing.AnswerSystem) || r.JudgePromptSHA != doing.SHA(doing.JudgeSystem) || (complete && len(r.Failures) != 0) {
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
		if !ok || row.Run < 1 || row.Run > repeats || seen[k] || (row.Method != "none" && row.Method != "frozen-current" && row.Method != "ideal") {
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
		chars := utf8.RuneCountInString(contextText)
		inputChars := utf8.RuneCountInString(doing.AnswerSystem + doing.AnswerPrompt(s, t, contextText))
		if row.Method == "frozen-current" && jsonSizes != nil {
			chars = contexts[row.Task].ContextChars
			inputChars = utf8.RuneCountInString(doing.AnswerSystem+doing.AnswerPrompt(s, t, "")) + jsonSizes[row.Task] - 2
		}
		if row.ContextChars != chars || row.InputChars != inputChars {
			return fmt.Errorf("context/input size mismatch")
		}
	}
	if complete && len(r.Rows) != len(tasks)*3*repeats {
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
	manifest := f.String("manifest", "", "numeric capture manifest; recompute without context text")
	originalSuite := f.String("original-suite", "", "suite before the two documented scope repairs")
	replacement := f.String("replacement", "", "all-three-repetition report for exactly the repaired tasks")
	completion2 := f.String("completion-run2", "", "one-repeat full-method completion of missing second-run tasks")
	completion3 := f.String("completion-run3", "", "one-repeat full-method third run")
	out := f.String("output", "", "new numeric artifact directory")
	fake := f.Bool("allow-fake", false, "explicit plumbing only; never real scores")
	if err := f.Parse(os.Args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 || *source == "" || *out == "" || (*snapshot == "") == (*manifest == "") {
		return fmt.Errorf("source, output and exactly one of snapshot/manifest required")
	}
	if (*originalSuite == "") != (*replacement == "") || (*manifest != "" && *replacement != "") {
		return fmt.Errorf("gold repair needs both original-suite/replacement and the original snapshot")
	}
	if (*completion2 == "") != (*completion3 == "") || (*completion2 != "" && *replacement == "") {
		return fmt.Errorf("transport completion needs both files and the documented gold replacement")
	}
	if *manifest != "" {
		return recalculate(*base, *suite, *source, *manifest, *out, *fake)
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
	captureSuiteSHA := snap.SuiteSHA
	var repairProof *goldProvenance
	var transportProof *transportProvenance
	var beforeGoldReport *doing.Report
	if *replacement != "" {
		before, loadErr := doing.Load(*originalSuite)
		if loadErr != nil {
			return loadErr
		}
		beforeBytes, loadErr := os.ReadFile(*originalSuite)
		if loadErr != nil {
			return loadErr
		}
		var repair doing.Report
		repairSHA, loadErr := read(*replacement, &repair)
		if loadErr != nil {
			return loadErr
		}
		var proof goldProvenance
		if *completion2 != "" {
			var c2, c3 doing.Report
			sha2, readErr := read(*completion2, &c2)
			if readErr != nil {
				return readErr
			}
			sha3, readErr := read(*completion3, &c3)
			if readErr != nil {
				return readErr
			}
			var tp transportProvenance
			r, tp, err = completeTransport(before, r, c2, c3, snap, doing.SHA(string(beforeBytes)), sourceSHA, sha2, sha3)
			if err != nil {
				return err
			}
			transportProof = &tp
			copyReport := r
			beforeGoldReport = &copyReport
			b, marshalErr := json.MarshalIndent(r, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			sourceSHA = doing.SHA(string(append(b, '\n')))
		}
		r, snap, proof, err = repairGold(before, s, r, repair, snap, doing.SHA(string(beforeBytes)), doing.SHA(string(raw)), sourceSHA, repairSHA)
		if err != nil {
			return err
		}
		repairProof = &proof
		mergedBytes, marshalErr := json.MarshalIndent(r, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		sourceSHA = doing.SHA(string(append(mergedBytes, '\n')))
	}
	if err = validate(r, s, doing.SHA(string(raw)), snap); err != nil {
		return err
	}
	if err = os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	repairSHA := ""
	if repairProof != nil {
		if err = doing.WriteJSON(filepath.Join(*out, "source-merged.json"), r); err != nil {
			return err
		}
		if err = doing.WriteJSON(filepath.Join(*out, "gold-repair-provenance.json"), repairProof); err != nil {
			return err
		}
		proofBytes, readErr := os.ReadFile(filepath.Join(*out, "gold-repair-provenance.json"))
		if readErr != nil {
			return readErr
		}
		repairSHA = doing.SHA(string(proofBytes))
	}
	if transportProof != nil {
		if err = doing.WriteJSON(filepath.Join(*out, "transport-provenance.json"), transportProof); err != nil {
			return err
		}
		if err = doing.WriteJSON(filepath.Join(*out, "source-before-gold.json"), beforeGoldReport); err != nil {
			return err
		}
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
	entries := []numericEntry{}
	for _, e := range snap.Entries {
		encoded, _ := json.Marshal(e.Text)
		entries = append(entries, numericEntry{e.Task, e.RequestSHA, e.ContextSHA, e.ContextChars, utf8.RuneCount(encoded), e.CaptureMS, e.CaptureCalls})
	}
	snapshotBytes, err := os.ReadFile(*snapshot)
	if err != nil {
		return err
	}
	baseBytes, err := os.ReadFile(*base)
	if err != nil {
		return err
	}
	return doing.WriteJSON(filepath.Join(*out, "capture-manifest.json"), captureManifest{Schema: 1, Fake: r.Fake, SourceSHA: sourceSHA, SnapshotSHA: doing.SHA(string(snapshotBytes)), SuiteSHA: r.SuiteSHA, BaseSHA: doing.SHA(string(baseBytes)), SnapshotSuiteSHA: captureSuiteSHA, GoldRepairSHA: repairSHA, CaptureRevision: snap.Revision, ModelRevision: r.Revision, AsOf: snap.AsOf, CaptureStart: snap.StartedAt, CaptureEnd: snap.CompletedAt, ModelStart: r.StartedAt, Entries: entries, Exposure: noiseExposure(s, snap), Note: "Snapshot text is synthetic and stays outside Git. Capture is once per task; no retrieval variability is measured. context_json_chars counts the encoded JSON string including quotes. Snapshot suite may differ from graded suite only for the two documented rubric-scope repairs; memory corpus, requests, evidence references and answer instructions are unchanged. Source has zero local captures per answer and three model calls per completed answer; interrupted and replaced work is accounted separately."})
}
