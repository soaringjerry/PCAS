package fixture

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func hardFixtureConfig(t *testing.T) HardConfig {
	t.Helper()
	cfg, err := LoadHardConfig("../../../testdata/phase2/eval")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func TestHardGeneratorDeterminismAndNoiseOnlyCalibration(t *testing.T) {
	cfg := hardFixtureConfig(t)
	anchor := time.Date(2028, 3, 1, 0, 0, 0, 0, time.FixedZone("fixture", 8*3600))
	c, err := GenerateHard(cfg, anchor)
	if err != nil {
		t.Fatal(err)
	}
	again, err := GenerateHard(cfg, anchor)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, again) {
		t.Fatal("same seed and anchor produced different corpus")
	}
	if len(c.Documents()) != 2728 || len(c.Queries()) != 40 {
		t.Fatal("wrong pressure scale")
	}
	cfg.CompetitorsPerCase += 32
	more, err := GenerateHard(cfg, anchor)
	if err != nil {
		t.Fatal(err)
	}
	for n, s := range c.Scenarios {
		if !reflect.DeepEqual(s.Documents, more.Scenarios[n].Documents) {
			t.Fatal("noise count changed standard source")
		}
		for m, q := range s.Queries {
			next := more.Scenarios[n].Queries[m]
			q.Distractors = nil
			next.Distractors = nil
			if !reflect.DeepEqual(q, next) {
				t.Fatal("noise count changed standard query, evidence or facts")
			}
		}
	}
	if got := len(CIQueries(c, 2)); got != 10 {
		t.Fatal("fixed CI subset", got)
	}
	// Only manifest configuration and generator are versioned, never generated
	// documents: a digest can still identify the full rendered corpus in reports.
	b, _ := json.Marshal(c)
	if len(b) == 0 {
		t.Fatal("missing reproducibility snapshot")
	}
}
func TestHardGoldAcrossCalendarBoundariesAndDistractorTypes(t *testing.T) {
	cfg := hardFixtureConfig(t)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	for _, date := range []string{"2027-01-01", "2028-02-29", "2028-03-01", "2026-12-31"} {
		anchor, _ := time.ParseInLocation("2006-01-02", date, loc)
		c, err := GenerateHard(cfg, anchor)
		if err != nil {
			t.Fatal(date, err)
		}
		docs := c.ByID()
		monday := (int(anchor.Weekday()) + 6) % 7
		weekStart := -monday - 7
		for _, q := range c.Queries() {
			for _, e := range q.Evidence {
				d := docs[e.Source]
				at := anchor.AddDate(0, 0, d.ExpressedDays)
				switch q.Type {
				case TimeEntity:
					if at.Year() != anchor.Year()-1 {
						t.Fatal(date, q.ID, "wrong gold year")
					}
				case WrongTime:
					if at.Year() != anchor.Year()-2 {
						t.Fatal(date, q.ID, "wrong fallback year")
					}
				case TimeOnly:
					if d.ExpressedDays < weekStart || d.ExpressedDays >= weekStart+7 || d.Gold.Items[0].Subject != "我" || d.Gold.Items[0].Nature != "plan" {
						t.Fatal(date, q.ID, "incomplete weekly set")
					}
				}
			}
			if q.Type == WrongTime {
				for _, id := range q.Distractors {
					if anchor.AddDate(0, 0, docs[id].ExpressedDays).Year() == anchor.Year()-1 {
						t.Fatal("R8 accidentally has requested-year competitors")
					}
				}
			}
			if q.Type != TimeOnly {
				target := docs[q.Evidence[0].Source].Gold.Items[0]
				for _, name := range append(append([]string{}, target.People...), target.Places...) {
					matches := 0
					for _, id := range q.Distractors {
						if strings.Contains(docs[id].Text, name) {
							matches++
						}
					}
					if matches < 40 {
						t.Fatal(q.ID, name, matches)
					}
				}
			}
		}
	}
}

func TestTierTotalsUseEvidenceMicroAverage(t *testing.T) {
	c := Corpus{Scenarios: []Scenario{{Queries: []Query{{ID: "one", Type: TimeOnly}, {ID: "many", Type: TimeOnly}, {ID: "control", Type: Ordinary}}}}}
	scores := []RetrievalScore{
		{Query: "one", Delivered: 1, Required: 1, Inversions: 1, Pairs: 2},
		{Query: "many", Delivered: 0, Required: 8, Inversions: 2, Pairs: 16},
		{Query: "control", Delivered: 1, Required: 1, Pairs: 2},
	}
	groups := AggregateRetrieval("hard", c, scores)
	weekly, all := groups[1], groups[5]
	if weekly.Delivered != 1 || weekly.Required != 9 || weekly.Recall != 1.0/9 || weekly.Interference != 3.0/18 || all.Recall != 0.2 || all.Pairs != 20 {
		t.Fatal("used question averages or mixed tier denominators", groups)
	}
}

func TestSentSlotCountsExcludeQuestionAndHistory(t *testing.T) {
	prompt := "[M99 / history] old\n召回的记忆\n[M1 / fact] alpha\n[M2 / plan] beta\n相关原话\n[S1 / old / date] gamma\nTHIS：\n[M3 / query] question"
	s := ScoreRetrieval(Corpus{}, Query{}, ContextSections(prompt))
	if s.MemoriesSent != 2 || s.SourcesSent != 1 {
		t.Fatal(s)
	}
}
