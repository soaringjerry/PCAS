package deputy

import (
	"context"
	"testing"
	"time"
)

func TestTheCheckedTextReplacesTheDraftOnlyWhenItKeepsTheSteps(t *testing.T) {
	draft := "- [ ] 第一步\n- [ ] 第二步"
	if text, fallback := AcceptSelfcheck(draft, "- [ ] 第一步（已核对）\n- [ ] 第二步", nil); fallback != "" || text == draft {
		t.Fatal(text, fallback)
	}
	for name, tc := range map[string]struct {
		checked string
		err     error
		want    string
	}{
		"a step was lost":   {"- [ ] 第一步", nil, "selfcheck_output_rejected"},
		"an empty answer":   {"  ", nil, "selfcheck_output_rejected"},
		"the check ran out": {"- [ ] 第一步\n- [ ] 第二步", context.DeadlineExceeded, "timeout"},
	} {
		if text, fallback := AcceptSelfcheck(draft, tc.checked, tc.err); text != draft || fallback != tc.want {
			t.Fatal(name, fallback)
		}
	}
	if !Selfchecks("heavy") || !Selfchecks("medium") || Selfchecks("light") {
		t.Fatal("a light run does not check its draft")
	}
	if SelfcheckBudget(5*time.Second) != 5*time.Second || SelfcheckBudget(2*time.Minute) != 30*time.Second {
		t.Fatal("the check gets the time of the answer, and 30 seconds at most")
	}
	if SelfcheckInput("虚构的材料", "虚构的草稿") != "虚构的材料\n待自查草稿：\n虚构的草稿" {
		t.Fatal("input")
	}
}

func TestACallOfUnknownOutcomeCostsItsReservationAtLeast(t *testing.T) {
	billed := 0.02
	if SettledCost(0.01, nil, false, 0.5) != 0.01 || SettledCost(0.01, &billed, false, 0.5) != 0.02 || SettledCost(0.01, &billed, true, 0.5) != 0.5 || SettledCost(0.9, nil, true, 0.5) != 0.9 {
		t.Fatal("settled cost")
	}
}
