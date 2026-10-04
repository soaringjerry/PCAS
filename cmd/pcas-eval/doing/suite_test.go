package doing

import (
	"strings"
	"testing"
)

func TestFrozenSuite(t *testing.T) {
	s, err := Load("../../../testdata/phase2_5/doing/suite.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Memories) != 671 || len(s.Tasks) != 120 {
		t.Fatal("frozen counts changed")
	}
	months := map[string]bool{}
	counts := map[string]int{}
	for _, m := range s.Memories {
		months[m.ExpressedAt[:7]] = true
		for _, tag := range m.Tags {
			counts[tag]++
		}
	}
	if len(months) != 6 || counts["update"] != 20 || counts["requirement"] != 6 || counts["one_off"] != 420 || counts["duplicate"] != 120 {
		t.Fatalf("coverage %v %v", months, counts)
	}
	for _, task := range s.Tasks {
		ctx := IdealEvidence(s, task)
		if strings.Contains(ctx, "M1") || strings.Contains(ctx, "合理") {
			t.Fatal("rubric leaked")
		}
		if task.Category == "irrelevant" && ctx != "" {
			t.Fatal("unrelated task got personal facts")
		}
	}
}
func TestBadReferencesAndChronology(t *testing.T) {
	s, _ := Load("../../../testdata/phase2_5/doing/suite.json")
	s.Tasks[0].Must[0].Evidence = []string{"missing"}
	if s.Validate() == nil {
		t.Fatal("accepted missing reference")
	}
	s, _ = Load("../../../testdata/phase2_5/doing/suite.json")
	for i := range s.Memories {
		if s.Memories[i].DuplicateOf != "" {
			s.Memories[i].ExpressedAt = "2026-09-30T09:00:00Z"
			break
		}
	}
	if s.Validate() == nil {
		t.Fatal("accepted repeat that reasserts obsolete fact after update")
	}
}
