package main

import (
	"fmt"
	"sort"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

type transportProvenance struct {
	PartialSHA       string         `json:"partial_source_uncompressed_sha256"`
	Completion2SHA   string         `json:"run2_completion_uncompressed_sha256"`
	Completion3SHA   string         `json:"run3_completion_uncompressed_sha256"`
	SuiteSHA         string         `json:"suite_before_gold_repair_sha256"`
	Run2Tasks        []string       `json:"run2_completion_task_ids"`
	PartialRows      int            `json:"partial_completed_rows"`
	RetainedRows     int            `json:"partial_retained_rows"`
	DiscardedRows    int            `json:"partial_discarded_run2_rows"`
	Completion2Rows  int            `json:"run2_completion_rows"`
	Completion3Rows  int            `json:"run3_completion_rows"`
	CompletedCalls   int            `json:"all_source_completed_model_calls"`
	FailedAttempts   int            `json:"partial_failed_model_call_attempts"`
	FailureTypes     map[string]int `json:"failure_types"`
	Completion2Start string         `json:"run2_completion_started_at"`
	Completion3Start string         `json:"run3_completion_started_at"`
	Note             string         `json:"note"`
}

func sameModels(a, b doing.Report) bool {
	return a.Model == b.Model && a.Channel == b.Channel && a.Workers == b.Workers && a.Fake == b.Fake
}
func filtered(s doing.Suite, snap doing.ContextSnapshot, ids map[string]bool) (doing.Suite, doing.ContextSnapshot) {
	subset := s
	subset.Tasks = []doing.Task{}
	for _, t := range s.Tasks {
		if ids[t.ID] {
			subset.Tasks = append(subset.Tasks, t)
		}
	}
	ss := snap
	ss.Entries = []doing.ContextEntry{}
	for _, e := range snap.Entries {
		if ids[e.Task] {
			ss.Entries = append(ss.Entries, e)
		}
	}
	return subset, ss
}

// Recover the actual interrupted experiment: complete first run, partial second,
// no third. Missing second-run task triples are fully replaced. Selection uses
// transport completeness only. Fresh source processes keep long-lived channel
// failures from being presented as quality failures.
func completeTransport(s doing.Suite, partial, c2, c3 doing.Report, snap doing.ContextSnapshot, suiteSHA, partialSHA, c2SHA, c3SHA string) (doing.Report, transportProvenance, error) {
	var proof transportProvenance
	if err := validateMatrix(partial, s, suiteSHA, snap, nil, 3, false); err != nil {
		return partial, proof, err
	}
	if !sameModels(partial, c2) || !sameModels(partial, c3) {
		return partial, proof, fmt.Errorf("completion model/channel/concurrency mismatch")
	}
	seen := map[string]bool{}
	key := func(run int, task, method string) string { return fmt.Sprintf("%d/%s/%s", run, task, method) }
	for _, r := range partial.Rows {
		if r.Run > 2 {
			return partial, proof, fmt.Errorf("original unexpectedly has third-run rows")
		}
		seen[key(r.Run, r.Task, r.Method)] = true
	}
	missing := map[string]bool{}
	for _, t := range s.Tasks {
		for _, m := range []string{"none", "frozen-current", "ideal"} {
			if !seen[key(1, t.ID, m)] {
				return partial, proof, fmt.Errorf("original first run incomplete")
			}
			if !seen[key(2, t.ID, m)] {
				missing[t.ID] = true
			}
		}
	}
	failed := map[string]bool{}
	taskIDs := map[string]bool{}
	for _, t := range s.Tasks {
		taskIDs[t.ID] = true
	}
	for _, f := range partial.Failures {
		k := key(f.Run, f.Task, f.Method)
		if f.Run != 2 || !taskIDs[f.Task] || seen[k] || failed[k] || f.ModelCalls < 0 || f.ModelCalls > 3 || (f.Method != "none" && f.Method != "frozen-current" && f.Method != "ideal") {
			return partial, proof, fmt.Errorf("invalid partial failure ledger")
		}
		failed[k] = true
	}
	for _, t := range s.Tasks {
		for _, m := range []string{"none", "frozen-current", "ideal"} {
			if !seen[key(2, t.ID, m)] && !failed[key(2, t.ID, m)] {
				return partial, proof, fmt.Errorf("missing original failure record")
			}
		}
	}
	subset, ss := filtered(s, snap, missing)
	if err := validateMatrix(c2, subset, suiteSHA, ss, nil, 1, true); err != nil {
		return partial, proof, err
	}
	if err := validateMatrix(c3, s, suiteSHA, snap, nil, 1, true); err != nil {
		return partial, proof, err
	}
	proof = transportProvenance{PartialSHA: partialSHA, Completion2SHA: c2SHA, Completion3SHA: c3SHA, SuiteSHA: suiteSHA, PartialRows: len(partial.Rows), Completion2Rows: len(c2.Rows), Completion3Rows: len(c3.Rows), Run2Tasks: []string{}, FailureTypes: map[string]int{}, Completion2Start: c2.StartedAt, Completion3Start: c3.StartedAt, Note: "The first run is complete, the second interrupted and the third unstarted. Every second-run task missing any method is rerun across all three methods, and its existing second-run rows are discarded irrespective of score. Completion sources are single repetitions mapped to global runs 2/3. The old rubric suite is deliberately retained until the separate full three-run two-task scope repair. Raw sources preserve actual dates and failure attempts; no product clock is changed."}
	for id := range missing {
		proof.Run2Tasks = append(proof.Run2Tasks, id)
	}
	sort.Strings(proof.Run2Tasks)
	merged := partial
	merged.Rows = []doing.Row{}
	merged.Failures = nil
	for _, r := range partial.Rows {
		proof.CompletedCalls += r.ModelCalls
		if r.Run == 2 && missing[r.Task] {
			proof.DiscardedRows++
		} else {
			merged.Rows = append(merged.Rows, r)
			proof.RetainedRows++
		}
	}
	for _, source := range []struct {
		report doing.Report
		run    int
	}{{c2, 2}, {c3, 3}} {
		for _, r := range source.report.Rows {
			proof.CompletedCalls += r.ModelCalls
			r.Run = source.run
			merged.Rows = append(merged.Rows, r)
		}
	}
	for _, f := range partial.Failures {
		proof.FailedAttempts += f.ModelCalls
		proof.FailureTypes[f.Error]++
	}
	merged.Summaries, merged.Ranges = doing.Aggregate(merged.Rows, 3)
	merged.Notes = append(append([]string{}, partial.Notes...), "Reconstructed after recorded model errors/timeouts. Completion used missing task/method coverage only; see transport-provenance.json. Row times are actual source measurements, not aggregate wall time or omitted failed work.")
	if err := validate(merged, s, suiteSHA, snap); err != nil {
		return partial, proof, err
	}
	return merged, proof, nil
}
