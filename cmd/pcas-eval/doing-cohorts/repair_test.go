package main

import (
	"encoding/json"
	"testing"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func cloneSuite(s doing.Suite) doing.Suite {
	b, _ := json.Marshal(s)
	var v doing.Suite
	json.Unmarshal(b, &v)
	return v
}
func TestScopeRepairReplacesWholeTasksAcrossAllMethods(t *testing.T) {
	_, after, original, snap := fixture(t)
	before := cloneSuite(after)
	for i, t := range before.Tasks {
		if t.ID == "I-NOISE-07" {
			before.Tasks[i].Must[1].Text = "earlier narrow label scope"
		}
		if t.ID == "I-NOISE-10" {
			before.Tasks[i].Must[0].Text = "earlier narrow meeting scope"
		}
	}
	beforeSHA := doing.SHA("before")
	afterSHA := original.SuiteSHA
	original.SuiteSHA = beforeSHA
	snap.SuiteSHA = beforeSHA
	repair := cloneReport(original)
	allRepairRows := repair.Rows
	repair.Rows = []doing.Row{}
	repair.SuiteSHA = afterSHA
	repair.Revision = "repair"
	repair.StartedAt = "2026-10-05T00:01:00Z"
	repair.HostDate = "2026-10-05"
	for _, row := range allRepairRows {
		if row.Task == "I-NOISE-07" || row.Task == "I-NOISE-10" {
			repair.Rows = append(repair.Rows, row)
		}
	}
	for i, row := range repair.Rows {
		if row.Run == 1 && row.Method == "ideal" && row.Task == "I-NOISE-07" {
			j := row.Judgments
			for k := range j {
				for n, c := range j[k].Checks {
					if c.ID[0] == 'M' {
						j[k].Checks[n].Value = true
					}
				}
			}
			task := after.Tasks[len(after.Tasks)-4]
			if task.ID != "I-NOISE-07" {
				t.Fatal("fixture order")
			}
			score := doing.Score(task, j)
			row.Judgments = j
			row.MustBoth = score.MustBoth
			row.Usable = score.Usable
			repair.Rows[i] = row
		}
	}
	merged, rebound, proof, err := repairGold(before, after, original, repair, snap, beforeSHA, afterSHA, doing.SHA("original"), doing.SHA("repair"))
	if err != nil {
		t.Fatal(err)
	}
	if proof.RetainedRows != 1494 || proof.DiscardedRows != 18 || proof.ReplacementRows != 18 || proof.OriginalCalls != 4536 || proof.RepairCalls != 54 || proof.RepairHostDate != "2026-10-05" {
		t.Fatal("incorrect replacement accounting")
	}
	if rebound.SuiteSHA != afterSHA || snap.SuiteSHA != beforeSHA || len(merged.Rows) != 1512 {
		t.Fatal("input snapshot mutated or matrix incomplete")
	}
	found := false
	for _, row := range merged.Rows {
		if row.Run == 1 && row.Task == "I-NOISE-07" && row.Method == "ideal" {
			found = true
			if !row.Usable {
				t.Fatal("replacement not retained")
			}
		}
	}
	if !found {
		t.Fatal("missing replacement")
	}
	bad := cloneSuite(after)
	bad.Tasks[0].Request = "modified old request"
	if _, err = changedScopes(before, bad); err == nil {
		t.Fatal("changed request accepted")
	}
	bad = cloneSuite(after)
	bad.Memories[700].Group = "changed source"
	if _, err = changedScopes(before, bad); err == nil {
		t.Fatal("changed corpus accepted")
	}
	badRepair := cloneReport(repair)
	badRepair.Rows = badRepair.Rows[1:]
	if _, _, _, err = repairGold(before, after, original, badRepair, snap, beforeSHA, afterSHA, "o", "r"); err == nil {
		t.Fatal("partial repair accepted")
	}
}
