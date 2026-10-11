package deputy

import (
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// selfcheckLimit is the longest time of a self-check. The self-check also
// gets no more time than the answer took, so that a run stays inside its
// wall clock budget.
const selfcheckLimit = 30 * time.Second

// Selfchecks reports whether a run of this memory tier checks its own draft.
// A light run does not.
func Selfchecks(tier string) bool { return tier != "light" }

// SelfcheckBudget is the time that the self-check of a run gets.
func SelfcheckBudget(answer time.Duration) time.Duration { return min(answer, selfcheckLimit) }

// SelfcheckInput is the model input of a self-check: the brief of the run
// and the draft.
func SelfcheckInput(brief, draft string) string { return brief + "\n待自查草稿：\n" + draft }

// AcceptSelfcheck returns the text that the run keeps. The checked text
// replaces the draft only when the check finished, gave text, and kept the
// count of checklist entries. In each other case the draft stays, and
// fallback names why.
func AcceptSelfcheck(draft, checked string, err error) (text, fallback string) {
	if err == nil && strings.TrimSpace(checked) != "" && len(Checklist(checked)) == len(Checklist(draft)) {
		return checked, ""
	}
	if err == nil {
		return draft, "selfcheck_output_rejected"
	}
	return draft, secretary.ErrorType("selfcheck", err)
}

// SettledCost is the cost of a run's answer call. A recorded bill replaces
// the reported cost. A call whose outcome is not known costs its reservation
// at least.
func SettledCost(reported float64, billed *float64, unknown bool, reserved float64) float64 {
	cost := reported
	if billed != nil {
		cost = *billed
	}
	if unknown {
		cost = max(cost, reserved)
	}
	return cost
}
