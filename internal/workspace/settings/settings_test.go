package settings

import (
	"encoding/json"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var base = workspace.Settings{WakeIdeas: true, FollowUps: true, DailyReviewAt: "09:00", DailyBudget: 10, Timezone: "Asia/Shanghai"}

func patch(v string) json.RawMessage { return json.RawMessage(v) }

func TestAChangeOfTheZoneOrTheReviewTimeNeedsNewPlans(t *testing.T) {
	next, replan, err := Apply(base, patch(`{"city":"  虚构城 ","dailyBudget":3}`))
	if err != nil || replan || next.City != "虚构城" || next.DailyBudget != 3 || next.Timezone != "Asia/Shanghai" {
		t.Fatalf("%+v %v %v", next, replan, err)
	}
	if _, replan, err := Apply(base, patch(`{"timezone":"Australia/Sydney"}`)); err != nil || !replan {
		t.Fatal(replan, err)
	}
	if _, replan, err := Apply(base, patch(`{"dailyReviewAt":"08:30"}`)); err != nil || !replan {
		t.Fatal(replan, err)
	}
	// The retired field is ignored and stays false.
	if next, _, err := Apply(base, patch(`{"autoAccept":true}`)); err != nil || next.AutoAccept {
		t.Fatal(next.AutoAccept, err)
	}
}

func TestSettingsThatAreNotValidLeaveTheCurrentSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		patch string
		want  error
	}{
		"an unknown zone":             {`{"timezone":"Mars/Olympus"}`, workspace.ErrTimezone},
		"a zone of another type":      {`{"timezone":8}`, workspace.ErrTimezone},
		"no field":                    {`{}`, memory.ErrInvalid},
		"a field that is not allowed": {`{"owner":"x"}`, memory.ErrInvalid},
		"a negative budget":           {`{"dailyBudget":-1}`, memory.ErrInvalid},
		"a time that is no clock":     {`{"dailyReviewAt":"9点"}`, memory.ErrInvalid},
		"a city of two lines":         {`{"city":"虚构\n城"}`, memory.ErrInvalid},
	} {
		next, replan, err := Apply(base, patch(tc.patch))
		if err != tc.want || replan || next != base {
			t.Fatal(name, err)
		}
	}
	if CheckZone(patch(`{"timezone":"Mars/Olympus"}`)) != workspace.ErrTimezone || CheckZone(patch(`{"city":"x"}`)) != nil {
		t.Fatal("zone check")
	}
}

func TestAnAgentGetsItsMemorySelectionWhenItIsFirstEnabled(t *testing.T) {
	agent := workspace.Agent{ID: "a1", Name: "虚构副手", MemoryKinds: []string{"fact"}}
	next, initialize, err := ApplyAgent(agent, patch(`{"enabled":true,"memoryKinds":["fact","plan"]}`))
	if err != nil || !initialize || !next.Enabled || len(next.MemoryKinds) != 2 {
		t.Fatalf("%+v %v %v", next, initialize, err)
	}
	next.MemoryInitialized = true
	if _, initialize, _ := ApplyAgent(next, patch(`{"note":"虚构的备注"}`)); initialize {
		t.Fatal("the selection is made once")
	}
	for name, p := range map[string]string{"an empty name": `{"name":" "}`, "an unknown kind": `{"memoryKinds":["secret"]}`, "a field that is not allowed": `{"channel":"manual"}`} {
		if _, _, err := ApplyAgent(agent, patch(p)); err != memory.ErrInvalid {
			t.Fatal(name, err)
		}
	}
}
