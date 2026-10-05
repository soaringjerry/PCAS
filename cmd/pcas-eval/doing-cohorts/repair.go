package main

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

type goldProvenance struct {
	OriginalSuite   string   `json:"original_suite_sha256"`
	FinalSuite      string   `json:"final_suite_sha256"`
	OriginalSource  string   `json:"original_source_uncompressed_sha256"`
	RepairSource    string   `json:"repair_source_uncompressed_sha256"`
	Changed         []string `json:"changed_task_check_ids"`
	OriginalRows    int      `json:"original_completed_rows"`
	RetainedRows    int      `json:"original_retained_rows"`
	DiscardedRows   int      `json:"original_replaced_rows"`
	ReplacementRows int      `json:"replacement_rows"`
	OriginalCalls   int      `json:"original_completed_model_calls"`
	RepairCalls     int      `json:"repair_completed_model_calls"`
	RepairRevision  string   `json:"repair_revision"`
	RepairStarted   string   `json:"repair_started_at"`
	RepairHostDate  string   `json:"repair_host_date"`
	Note            string   `json:"note"`
}

// Only the two explicitly documented positive rubric text changes are allowed.
// Requests, every memory field, evidence lists, handling and other gold stay
// exact. A snapshot uses requests/memories, not scoring text, so no recapture or
// imaginary product clock is needed for this scope-only repair.
func changedScopes(before, after doing.Suite) (map[string]bool, error) {
	allowed := map[string]string{"I-NOISE-07": "M2", "I-NOISE-10": "M1"}
	oldTasks, newTasks := before.Tasks, after.Tasks
	before.Tasks = nil
	after.Tasks = nil
	if !reflect.DeepEqual(before, after) || len(oldTasks) != len(newTasks) {
		return nil, fmt.Errorf("scope repair changed corpus or topology")
	}
	changed := map[string]bool{}
	for i, t := range newTasks {
		old := oldTasks[i]
		if reflect.DeepEqual(t, old) {
			continue
		}
		check, ok := allowed[t.ID]
		if !ok || len(t.Must) != len(old.Must) {
			return nil, fmt.Errorf("undocumented scope repair")
		}
		copyTask := t
		copyTask.Must = append([]doing.Check{}, t.Must...)
		n := 0
		for j, c := range copyTask.Must {
			if c.ID == check && c.Text != old.Must[j].Text {
				copyTask.Must[j].Text = old.Must[j].Text
				n++
			}
		}
		if n != 1 || !reflect.DeepEqual(copyTask, old) {
			return nil, fmt.Errorf("repair changed more than positive check wording")
		}
		changed[t.ID] = true
	}
	if len(changed) != 2 {
		return nil, fmt.Errorf("both documented scopes must be repaired")
	}
	return changed, nil
}

func repairGold(before, after doing.Suite, original, replacement doing.Report, snap doing.ContextSnapshot, beforeSHA, afterSHA, originalSHA, repairSHA string) (doing.Report, doing.ContextSnapshot, goldProvenance, error) {
	var proof goldProvenance
	changed, err := changedScopes(before, after)
	if err != nil {
		return original, snap, proof, err
	}
	if err = validate(original, before, beforeSHA, snap); err != nil {
		return original, snap, proof, err
	}
	if replacement.Model != original.Model || replacement.Channel != original.Channel || replacement.Workers != original.Workers || replacement.Fake != original.Fake {
		return original, snap, proof, fmt.Errorf("replacement model/channel/concurrency mismatch")
	}
	// Frozen retrieval's capture date stays fixed. Generations may start on a
	// later date; each raw report preserves its actual start/host date. This is a
	// declared clock limitation, not a product clock injection.
	subset := after
	subset.Tasks = []doing.Task{}
	for _, t := range after.Tasks {
		if changed[t.ID] {
			subset.Tasks = append(subset.Tasks, t)
		}
	}
	rebound := snap
	rebound.SuiteSHA = afterSHA
	repairSnapshot := rebound
	repairSnapshot.Entries = []doing.ContextEntry{}
	for _, e := range snap.Entries {
		if changed[e.Task] {
			repairSnapshot.Entries = append(repairSnapshot.Entries, e)
		}
	}
	if err = validate(replacement, subset, afterSHA, repairSnapshot); err != nil {
		return original, snap, proof, err
	}
	proof = goldProvenance{OriginalSuite: beforeSHA, FinalSuite: afterSHA, OriginalSource: originalSHA, RepairSource: repairSHA, OriginalRows: len(original.Rows), ReplacementRows: len(replacement.Rows), RepairRevision: replacement.Revision, RepairStarted: replacement.StartedAt, RepairHostDate: replacement.HostDate, Changed: []string{"I-NOISE-07/M2", "I-NOISE-10/M1"}, Note: "Scope repair was triggered after first-round scores revealed possible overly narrow positive checks. No answer texts were read to fit gold. Every method and repetition of both changed tasks is replaced, irrespective of its score. All other tasks and all memory/request/evidence fields remain exact. Snapshot was captured on the original suite date; only rubric text differs, so contexts are reused without recapture. The raw replacement report preserves its actual later host date; model clocks were not frozen."}
	merged := original
	merged.Rows = []doing.Row{}
	merged.SuiteSHA = afterSHA
	for _, row := range original.Rows {
		proof.OriginalCalls += row.ModelCalls
		if changed[row.Task] {
			proof.DiscardedRows++
		} else {
			merged.Rows = append(merged.Rows, row)
			proof.RetainedRows++
		}
	}
	for _, row := range replacement.Rows {
		merged.Rows = append(merged.Rows, row)
		proof.RepairCalls += row.ModelCalls
	}
	sort.Slice(merged.Rows, func(i, j int) bool {
		a, b := merged.Rows[i], merged.Rows[j]
		return fmt.Sprintf("%d/%s/%s", a.Run, a.Method, a.Task) < fmt.Sprintf("%d/%s/%s", b.Run, b.Method, b.Task)
	})
	merged.Summaries, merged.Ranges = doing.Aggregate(merged.Rows, 3)
	merged.Notes = append(append([]string{}, original.Notes...), "All three methods/repetitions of I-NOISE-07 and I-NOISE-10 were replaced after manually broadening reasonable positive-check scopes. No other gold/request/memory fields changed. See gold-repair-provenance.json; raw sources and all discarded work are preserved separately.")
	if err = validate(merged, after, afterSHA, rebound); err != nil {
		return original, snap, proof, err
	}
	return merged, rebound, proof, nil
}
