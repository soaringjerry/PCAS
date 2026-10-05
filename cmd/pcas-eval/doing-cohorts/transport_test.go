package main

import (
	"testing"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func TestTransportCompletionReplacesWholeIncompleteTriples(t *testing.T) {
	_, s, full, snap := fixture(t)
	partial := cloneReport(full)
	partial.Rows = []doing.Row{}
	partial.Failures = []doing.Failure{}
	c2 := cloneReport(full)
	c2.Rows = []doing.Row{}
	c2.Repeats = 1
	c3 := cloneReport(full)
	c3.Rows = []doing.Row{}
	c3.Repeats = 1
	c3.HostDate = "2026-10-05"
	c3.StartedAt = "2026-10-05T00:01:00Z"
	first, second := s.Tasks[0].ID, s.Tasks[1].ID
	for _, row := range full.Rows {
		if row.Run == 1 {
			partial.Rows = append(partial.Rows, row)
		}
		if row.Run == 2 {
			if row.Task == first || (row.Task == second && row.Method == "none") {
				partial.Failures = append(partial.Failures, doing.Failure{Run: 2, Task: row.Task, Method: row.Method, Error: "judge_call_failed pass=1 type=timeout", ModelCalls: 2})
			} else {
				partial.Rows = append(partial.Rows, row)
			}
			if row.Task == first || row.Task == second {
				row.Run = 1
				c2.Rows = append(c2.Rows, row)
			}
		}
		if row.Run == 3 {
			row.Run = 1
			c3.Rows = append(c3.Rows, row)
		}
	}
	merged, proof, err := completeTransport(s, partial, c2, c3, snap, full.SuiteSHA, "partial", "c2", "c3")
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Rows) != 1512 || proof.PartialRows != 1004 || proof.DiscardedRows != 2 || proof.RetainedRows != 1002 || proof.Completion2Rows != 6 || proof.Completion3Rows != 504 || proof.FailedAttempts != 8 || len(proof.Run2Tasks) != 2 {
		t.Fatal("wrong recovery selection/accounting")
	}
	missing := cloneReport(c2)
	missing.Rows = missing.Rows[1:]
	if _, _, err = completeTransport(s, partial, missing, c3, snap, full.SuiteSHA, "p", "2", "3"); err == nil {
		t.Fatal("incomplete completion accepted")
	}
	wrong := cloneReport(c3)
	wrong.Model = "different"
	if _, _, err = completeTransport(s, partial, c2, wrong, snap, full.SuiteSHA, "p", "2", "3"); err == nil {
		t.Fatal("changed model accepted")
	}
	bad := cloneReport(partial)
	bad.Failures = bad.Failures[1:]
	if _, _, err = completeTransport(s, bad, c2, c3, snap, full.SuiteSHA, "p", "2", "3"); err == nil {
		t.Fatal("failure not accounted")
	}
}
