package fixture

import (
	"testing"
	"time"
)

func TestCorpus(t *testing.T) {
	c, _, err := Load("../../../testdata/phase2/eval", time.Date(2031, 1, 2, 0, 0, 0, 0, time.FixedZone("eval", 8*3600)))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Documents()) != 120 || len(c.Queries()) != 36 {
		t.Fatal("corpus size changed; review oracles")
	}
}
func TestScoresExcludeQuestionAndChargeMissingEvidence(t *testing.T) {
	d := Document{ID: "gold", Text: "actual gold", Gold: Extraction{Items: []Item{{Text: "actual gold", Quote: "actual gold"}}}}
	c := Corpus{Noise: []Document{d, {ID: "noise", Text: "distractor"}}}
	q := Query{Evidence: []Evidence{{Source: "gold"}}, Distractors: []string{"noise"}}
	context := ContextSections("召回的记忆\ndistractor\n相关原话\n\nTHIS：\n这句话：actual gold")
	s := ScoreRetrieval(c, q, context)
	if s.Delivered != 0 || s.Inversions != 1 {
		t.Fatal(s)
	}
	s = ScoreRetrieval(c, q, "actual gold distractor actual gold")
	if s.Delivered != 1 || s.Inversions != 0 {
		t.Fatal(s)
	}
}
func TestExtractionPenalizesExtraMissingAndMismatchedItems(t *testing.T) {
	gold := Item{Quote: "q", Nature: "plan", People: []string{"甲"}, When: &When{From: "2031-01-02", To: "2031-01-03", Precision: "day"}}
	d := Document{ID: "d", Gold: Extraction{Items: []Item{gold}}}
	perfect := CompareExtraction(d, []Item{gold})
	if perfect.People.Accuracy() != 1 || perfect.Time.Accuracy() != 1 || perfect.Nature.Accuracy() != 1 {
		t.Fatal(perfect)
	}
	bad := gold
	bad.People = []string{"甲", "乙"}
	bad.When = nil
	s := CompareExtraction(d, []Item{bad, {Quote: "extra", Nature: "fact", Places: []string{"错地"}}})
	if s.People.Accuracy() >= 1 || s.Places.Accuracy() != 0 || s.Time.Accuracy() != 0 || s.Nature.Accuracy() != 0.5 {
		t.Fatal(s)
	}
	missing := CompareExtraction(d, nil)
	if missing.People.Accuracy() != 0 || missing.Time.Accuracy() != 0 || missing.Nature.Accuracy() != 0 {
		t.Fatal(missing)
	}
}

func TestIdenticalTextNeedsGoldSourceIdentity(t *testing.T) {
	c := Corpus{Noise: []Document{{ID: "required", Text: "same", Gold: Extraction{Items: []Item{{Text: "same", Quote: "same"}}}}}}
	q := Query{Evidence: []Evidence{{Source: "required"}}}
	s := ScoreRetrievalIdentified(c, q, "same", map[string]bool{"other/0": true})
	if s.Delivered != 0 {
		t.Fatal("identical text from another source counted as gold", s)
	}
}
