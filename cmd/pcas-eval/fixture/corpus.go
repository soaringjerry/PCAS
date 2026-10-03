// Package fixture is shared only by the evaluation CLI and the independent CI
// evaluator. It does not participate in product retrieval or extraction.
package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type When struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Precision string `json:"precision"`
	Quote     string `json:"quote"`
}
type Item struct {
	Kind          string   `json:"kind"`
	Text          string   `json:"text"`
	Nature        string   `json:"nature"`
	Subject       string   `json:"subject"`
	Predicate     string   `json:"predicate"`
	Quote         string   `json:"quote"`
	Confidence    float64  `json:"confidence"`
	Explicit      bool     `json:"explicit"`
	Acquisition   string   `json:"acquisition"`
	Qualification string   `json:"qualification"`
	People        []string `json:"people"`
	Places        []string `json:"places"`
	Organizations []string `json:"organizations"`
	When          *When    `json:"when,omitempty"`
}
type Extraction struct {
	Items []Item `json:"items"`
}
type Document struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Text          string     `json:"text"`
	ExpressedDays int        `json:"expressed_days"`
	RecordedDays  int        `json:"recorded_days"`
	Connector     string     `json:"connector"`
	Role          string     `json:"role"`
	Gold          Extraction `json:"gold"`
}
type Evidence struct {
	Source string `json:"source"`
	Item   int    `json:"item"`
}
type Fact struct {
	Any []string `json:"any"`
}
type Query struct {
	Type        string     `json:"type,omitempty"`
	ID          string     `json:"id"`
	Text        string     `json:"text"`
	Evidence    []Evidence `json:"evidence"`
	Distractors []string   `json:"distractors"`
	Facts       []Fact     `json:"facts"`
	Rationale   string     `json:"rationale"`
}
type Scenario struct {
	ID           string     `json:"id"`
	Interference []string   `json:"interference"`
	Rationale    string     `json:"rationale"`
	Documents    []Document `json:"documents"`
	Queries      []Query    `json:"queries"`
}
type Corpus struct {
	Tier          string     `json:"tier,omitempty"`
	SchemaVersion int        `json:"schema_version"`
	Persona       string     `json:"persona"`
	Timezone      string     `json:"timezone"`
	Scenarios     []Scenario `json:"scenarios"`
	Noise         []Document `json:"noise"`
}
type Baseline struct {
	MinimumRecall float64 `json:"minimum_recall"`
	Status        string  `json:"status"`
	Reason        string  `json:"reason"`
}

var template = regexp.MustCompile(`\{\{(date|year):(-?\d+)\}\}`)

func Render(s string, anchor time.Time) string {
	return template.ReplaceAllStringFunc(s, func(v string) string {
		m := template.FindStringSubmatch(v)
		days, _ := strconv.Atoi(m[2])
		at := anchor.AddDate(0, 0, days)
		if m[1] == "year" {
			return at.Format("2006")
		}
		return at.Format("2006-01-02")
	})
}
func Load(dir string, anchor time.Time) (Corpus, Baseline, error) {
	var c Corpus
	var b Baseline
	raw, err := os.ReadFile(filepath.Join(dir, "corpus.json"))
	if err != nil {
		return c, b, err
	}
	if err = json.Unmarshal([]byte(Render(string(raw), anchor)), &c); err != nil {
		return c, b, err
	}
	raw, err = os.ReadFile(filepath.Join(dir, "baseline.json"))
	if err != nil {
		return c, b, err
	}
	if err = json.Unmarshal(raw, &b); err != nil {
		return c, b, err
	}
	if b.MinimumRecall < 0 || b.MinimumRecall > 1 {
		return c, b, fmt.Errorf("invalid recall baseline")
	}
	return c, b, c.Validate()
}
func (c Corpus) Documents() []Document {
	var out []Document
	for _, s := range c.Scenarios {
		out = append(out, s.Documents...)
	}
	return append(out, c.Noise...)
}
func (c Corpus) Queries() []Query {
	var out []Query
	for _, s := range c.Scenarios {
		out = append(out, s.Queries...)
	}
	return out
}
func (c Corpus) ByID() map[string]Document {
	out := map[string]Document{}
	for _, d := range c.Documents() {
		out[d.ID] = d
	}
	return out
}
func (c Corpus) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("unknown corpus version")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return err
	}
	ids := map[string]bool{}
	coverage := map[string]int{}
	for _, d := range c.Documents() {
		if d.ID == "" || ids[d.ID] || d.Text == "" {
			return fmt.Errorf("invalid document %q", d.ID)
		}
		ids[d.ID] = true
		if d.Role != "user" && len(d.Gold.Items) > 0 {
			return fmt.Errorf("non-user gold: %s", d.ID)
		}
		for _, i := range d.Gold.Items {
			if i.Kind != "memory" || i.Text == "" || !strings.Contains(d.Text, i.Quote) || i.Quote == "" {
				return fmt.Errorf("invalid gold evidence: %s", d.ID)
			}
			if !strings.Contains("|fact|preference|intention|plan|decision|", "|"+i.Nature+"|") {
				return fmt.Errorf("invalid nature: %s", d.ID)
			}
			for _, names := range [][]string{i.People, i.Places, i.Organizations} {
				for _, n := range names {
					if n == "" || !strings.Contains(d.Text, n) {
						return fmt.Errorf("ungrounded mention %q: %s", n, d.ID)
					}
				}
			}
			if w := i.When; w != nil {
				from, e1 := time.Parse("2006-01-02", w.From)
				to, e2 := time.Parse("2006-01-02", w.To)
				if e1 != nil || e2 != nil || !to.After(from) || !strings.Contains(d.Text, w.Quote) {
					return fmt.Errorf("invalid gold time: %s", d.ID)
				}
			}
		}
	}
	docs := c.ByID()
	qids := map[string]bool{}
	for _, s := range c.Scenarios {
		if len(s.Documents) < 6 || len(s.Documents) > 12 || (c.Tier != "hard" && (len(s.Queries) < 2 || len(s.Queries) > 3)) {
			return fmt.Errorf("invalid scene size %s", s.ID)
		}
		for _, tag := range s.Interference {
			coverage[tag]++
		}
		for _, q := range s.Queries {
			if qids[q.ID] || q.ID == "" || len(q.Evidence) == 0 || len(q.Facts) == 0 || q.Rationale == "" {
				return fmt.Errorf("invalid query %s", q.ID)
			}
			qids[q.ID] = true
			evidenceIDs := map[string]bool{}
			for _, e := range q.Evidence {
				d, ok := docs[e.Source]
				if !ok || e.Item < 0 || e.Item >= len(d.Gold.Items) {
					return fmt.Errorf("bad evidence %s", q.ID)
				}
				evidenceIDs[e.Source] = true
			}
			for _, id := range q.Distractors {
				if _, ok := docs[id]; !ok || evidenceIDs[id] {
					return fmt.Errorf("bad distractor %s", q.ID)
				}
			}
			for _, f := range q.Facts {
				if len(f.Any) == 0 {
					return fmt.Errorf("empty fact %s", q.ID)
				}
				for _, a := range f.Any {
					if a == "" {
						return fmt.Errorf("empty alias %s", q.ID)
					}
				}
			}
		}
	}
	if c.Tier == "hard" {
		counts := map[string]int{}
		for _, q := range c.Queries() {
			counts[q.Type]++
			if len(q.Distractors) < 40 {
				return fmt.Errorf("hard query missing competitors: %s", q.ID)
			}
		}
		for _, typ := range QueryTypes {
			if counts[typ] < 8 {
				return fmt.Errorf("insufficient hard query type %s", typ)
			}
		}
		if len(c.Documents()) < 2000 {
			return fmt.Errorf("hard corpus too small")
		}
		return nil
	}
	for _, tag := range []string{"same_name", "others_plan", "cancelled", "completed", "wrong_year", "late_import", "similar_place", "repeated_details"} {
		if coverage[tag] < 2 {
			return fmt.Errorf("insufficient interference coverage: %s", tag)
		}
	}
	return nil
}
