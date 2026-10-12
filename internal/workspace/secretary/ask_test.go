package secretary

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// fakeModel answers with fixed text and records what the turn asked of it.
type fakeModel struct {
	text     string
	failure  error
	fatal    error
	checkErr error
	log      []string
	tiers    []string
	inputs   []string
	timeouts []time.Duration
}

func (f *fakeModel) HeavyRead(_ context.Context, deadline time.Time, task string, _ *Asking, stage string) ([]workspace.Memory, []memory.Ref, []string) {
	f.log = append(f.log, stage)
	return []workspace.Memory{{ID: "h-" + stage, Text: "读到的记忆", Trust: "confirmed"}}, []memory.Ref{{ID: "r", Version: 1}}, []string{"entity:x"}
}
func (f *fakeModel) Prepared()             { f.log = append(f.log, "prepared") }
func (f *fakeModel) Reading(time.Duration) {}
func (f *fakeModel) Generate(_ context.Context, st *Asking) (string, string, error, error) {
	f.log = append(f.log, "generate")
	f.inputs = append(f.inputs, st.Input)
	return f.text, "model", f.failure, f.fatal
}
func (f *fakeModel) Check(_ context.Context, timeout time.Duration, st *Asking, answer Output) (Output, error) {
	f.log = append(f.log, "check")
	f.timeouts = append(f.timeouts, timeout)
	f.inputs = append(f.inputs, st.Input)
	if f.checkErr != nil {
		return answer, f.checkErr
	}
	answer.Reply += "（已自查）"
	return answer, nil
}
func (f *fakeModel) SetTier(_ context.Context, tier string) error {
	f.tiers = append(f.tiers, tier)
	return nil
}

var limits = Limits{Model: 30 * time.Second, HeavyReader: 90 * time.Second, Total: 3 * time.Minute}

func asking(tier string, ready bool) *Asking {
	return &Asking{Tier: tier, Use: contextbuilder.UseContext{Ready: ready, Index: []workspace.StatusCardRef{{Key: "entity:x"}}}, Sent: map[string]workspace.Memory{}, Input: "输入", Text: "一句话", Location: zone, Started: time.Now()}
}

func TestALightAnswerIsAppliedWithoutAReadingOrACheck(t *testing.T) {
	m := &fakeModel{text: `{"reply":"好的","actions":[{"op":"create_task","title":"虚构"}]}`}
	st := asking("light", true)
	got, err := Ask(context.Background(), m, st, limits)
	if err != nil || got.Failure != nil || !got.Generated || got.Checked || got.Answer.Reply != "好的" || strings.Join(m.log, ",") != "prepared,generate" || st.Tier != "light" {
		t.Fatalf("%+v %v %v", got, err, m.log)
	}
}

func TestAnAnswerThatDeletesOrLacksKeyInformationIsChecked(t *testing.T) {
	for name, text := range map[string]string{
		"deletes":           `{"reply":"好","actions":[{"op":"delete_task","ref":"T1"}]}`,
		"lacks information": `{"reply":"好","missingKeyInfo":true}`,
	} {
		m := &fakeModel{text: text}
		st := asking("light", false)
		got, err := Ask(context.Background(), m, st, limits)
		if err != nil || !got.Checked || got.Answer.Reply != "好（已自查）" || st.Tier != "medium" || strings.Join(m.tiers, ",") != "medium" || m.timeouts[0] > limits.Model {
			t.Fatalf("%s: %+v %v %v", name, got, err, m.tiers)
		}
	}
	// A check that fails keeps the draft and names the failure.
	m := &fakeModel{text: `{"reply":"好","missingKeyInfo":true}`, checkErr: context.DeadlineExceeded}
	got, _ := Ask(context.Background(), m, asking("light", true), limits)
	if !got.Checked || got.CheckFailure == nil || got.Answer.Reply != "好" {
		t.Fatalf("%+v", got)
	}
}

func TestAHeavyTurnReadsBeforeTheAnswerAndADeeperAnswerReadsOnce(t *testing.T) {
	m := &fakeModel{text: `{"reply":"好","memoryPlan":{"depth":"heavy","groups":["G1"]}}`}
	st := asking("heavy", true)
	got, err := Ask(context.Background(), m, st, limits)
	if err != nil || strings.Join(m.log, ",") != "reader:prepare,prepared,generate,check" || !got.Checked {
		t.Fatal(err, m.log)
	}
	if !strings.Contains(m.inputs[0], "重档读者补充 [M1 / trust=confirmed] 读到的记忆") || len(st.Sent) != 1 || len(st.Dependencies) != 1 {
		t.Fatalf("%q %v", m.inputs[0], st.Dependencies)
	}
	// A medium turn whose answer asks for heavy depth reads the named groups once, then checks.
	m = &fakeModel{text: `{"reply":"好","memoryPlan":{"depth":"heavy","groups":["G1"]}}`}
	st = asking("medium", true)
	if _, err := Ask(context.Background(), m, st, limits); err != nil || strings.Join(m.log, ",") != "prepared,generate,reader:plan,check" || st.Tier != "heavy" || !st.Use.Selected || strings.Join(st.Use.RequiredGroups, ",") != "entity:x" {
		t.Fatal(err, m.log, st.Tier, st.Use.RequiredGroups)
	}
	if !strings.Contains(m.inputs[1], "重读补充 [M1] 读到的记忆") {
		t.Fatalf("%q", m.inputs[1])
	}
	// Without a ready current state a turn cannot read heavy.
	m = &fakeModel{text: `{"reply":"好","memoryPlan":{"depth":"heavy"}}`}
	st = asking("light", false)
	if _, err := Ask(context.Background(), m, st, limits); err != nil || st.Tier != "medium" || strings.Join(m.log, ",") != "prepared,generate,check" {
		t.Fatal(err, st.Tier, m.log)
	}
}

func TestAnAnswerThatIsNoObjectIsKeptAsTextAndAFailureEndsTheAsking(t *testing.T) {
	m := &fakeModel{text: "  只是一段话  "}
	got, err := Ask(context.Background(), m, asking("medium", true), limits)
	if err != nil || got.Answer.Reply != "只是一段话" || len(got.Answer.Actions) != 0 || got.Checked {
		t.Fatalf("%+v %v", got, err)
	}
	m = &fakeModel{failure: context.DeadlineExceeded}
	got, err = Ask(context.Background(), m, asking("medium", true), limits)
	if err != nil || got.Failure != context.DeadlineExceeded || got.Stage != "model" || !got.Generated || got.Checked {
		t.Fatalf("%+v %v", got, err)
	}
	broken := errors.New("fictitious accounting failure")
	m = &fakeModel{fatal: broken}
	if _, err = Ask(context.Background(), m, asking("light", true), limits); !errors.Is(err, broken) {
		t.Fatal(err)
	}
	if TierFor("heavy", false) != "medium" || TierFor("heavy", true) != "heavy" || TierFor("light", false) != "light" {
		t.Fatal("tier")
	}
}
