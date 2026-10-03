package memory

// Independent acceptance oracles are frozen in b3-gold.json before this file.
import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"
)

type b3PlanCase struct {
	Group, Name, Text, Zone, Now string
	Time                         *struct{ From, To, Precision, Axis, Phrase string }
	Natures                      []string
	Recall                       bool
}

func b3PlanCases(t *testing.T, group string) []b3PlanCase {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/phase2/b3-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold struct{ Planner []b3PlanCase }
	if err := json.Unmarshal(raw, &gold); err != nil {
		t.Fatal(err)
	}
	var cases []b3PlanCase
	for _, c := range gold.Planner {
		if c.Group == group {
			cases = append(cases, c)
		}
	}
	if len(cases) == 0 {
		t.Fatalf("missing frozen calendar oracle for %s", group)
	}
	return cases
}

func b3AssertPlans(t *testing.T, group string) {
	t.Helper()
	for _, c := range b3PlanCases(t, group) {
		t.Run(c.Name, func(t *testing.T) {
			loc, err := time.LoadLocation(c.Zone)
			if err != nil {
				t.Fatal(err)
			}
			now, err := time.Parse(time.RFC3339, c.Now)
			if err != nil {
				t.Fatal(err)
			}
			got := PlanQuery(c.Text, now, loc)
			again := PlanQuery(c.Text, now, loc)
			if !reflect.DeepEqual(got, again) {
				t.Fatal("same input produced a different plan")
			}
			if c.Time == nil {
				if got.Time != nil {
					t.Errorf("unrecognized time must remain nil: %+v", got.Time)
				}
			} else if got.Time == nil {
				t.Error("recognized time missing")
			} else {
				from, err := time.Parse(time.RFC3339, c.Time.From)
				if err != nil {
					t.Fatal(err)
				}
				to, err := time.Parse(time.RFC3339, c.Time.To)
				if err != nil {
					t.Fatal(err)
				}
				if !got.Time.From.Equal(from) || !got.Time.To.Equal(to) || got.Time.Precision != c.Time.Precision || got.Time.Axis != c.Time.Axis || got.Time.Phrase != c.Time.Phrase {
					t.Errorf("plan time=%+v; frozen oracle=%+v", got.Time, *c.Time)
				}
				if !got.Time.From.Before(got.Time.To) {
					t.Error("interval is not nonempty and half-open")
				}
			}
			actual := append([]string{}, got.Natures...)
			want := append([]string{}, c.Natures...)
			sort.Strings(actual)
			sort.Strings(want)
			if !reflect.DeepEqual(actual, want) || got.Recall != c.Recall {
				t.Errorf("natures=%v recall=%v; want %v %v", got.Natures, got.Recall, c.Natures, c.Recall)
			}
		})
	}
}

func TestPhase2B3_P1_ChengduRecall(t *testing.T)               { b3AssertPlans(t, "P1") }
func TestPhase2B3_P2_EveryTimePhrase(t *testing.T)             { b3AssertPlans(t, "P2") }
func TestPhase2B3_P3_LocalCalendarBoundaries(t *testing.T)     { b3AssertPlans(t, "P3") }
func TestPhase2B3_P4_NoTimeAndMeaningIndependent(t *testing.T) { b3AssertPlans(t, "P4") }
func TestPhase2B3_P5_MostRecentMonthAndDay(t *testing.T)       { b3AssertPlans(t, "P5") }
func TestPhase2B3_P6_NaturesAndRecallWords(t *testing.T)       { b3AssertPlans(t, "P6") }
func TestPhase2B3_P7_AxisAndFirstPhrase(t *testing.T)          { b3AssertPlans(t, "P7") }
