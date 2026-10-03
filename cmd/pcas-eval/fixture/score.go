package fixture

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

type RetrievalScore struct {
	Query        string  `json:"query"`
	Delivered    int     `json:"delivered"`
	Required     int     `json:"required"`
	Inversions   int     `json:"inversions"`
	Pairs        int     `json:"pairs"`
	Recall       float64 `json:"recall"`
	Interference float64 `json:"interference"`
	MemoriesSent int     `json:"memories_sent"`
	SourcesSent  int     `json:"sources_sent"`
}

var sentAlias = regexp.MustCompile(`(?m)^\[(M|S)[0-9]+ / `)

// ContextSections excludes the question/history. Markers are product section
// headings, not injected gold labels. Fail closed if formatting changes.
func ContextSections(prompt string) string {
	start := strings.Index(prompt, "召回的记忆")
	if start < 0 {
		return ""
	}
	end := strings.Index(prompt[start:], "\nTHIS：")
	if end < 0 {
		return ""
	}
	return prompt[start : start+end]
}
func position(text string, alternatives ...string) int {
	pos := math.MaxInt
	for _, a := range alternatives {
		if a == "" {
			continue
		}
		if p := strings.Index(text, a); p >= 0 && p < pos {
			pos = p
		}
	}
	return pos
}
func ScoreRetrieval(c Corpus, q Query, context string) RetrievalScore {
	return ScoreRetrievalIdentified(c, q, context, nil)
}
func ScoreRetrievalIdentified(c Corpus, q Query, context string, received map[string]bool) RetrievalScore {
	docs := c.ByID()
	s := RetrievalScore{Query: q.ID, Required: len(q.Evidence), Pairs: len(q.Evidence) * len(q.Distractors)}
	for _, alias := range sentAlias.FindAllStringSubmatch(context, -1) {
		if alias[1] == "M" {
			s.MemoriesSent++
		} else {
			s.SourcesSent++
		}
	}
	for _, e := range q.Evidence {
		item := docs[e.Source].Gold.Items[e.Item]
		ep := position(context, item.Text, item.Quote)
		if received != nil && !received[fmt.Sprintf("%s/%d", e.Source, e.Item)] {
			ep = math.MaxInt
		}
		if ep < math.MaxInt {
			s.Delivered++
		}
		for _, id := range q.Distractors {
			d := docs[id]
			dp := position(context, d.Text)
			for _, i := range d.Gold.Items {
				if p := position(context, i.Text, i.Quote); p < dp {
					dp = p
				}
			}
			if received != nil && !received[id] {
				dp = math.MaxInt
			}
			if dp < ep {
				s.Inversions++
			}
		}
	}
	if s.Required > 0 {
		s.Recall = float64(s.Delivered) / float64(s.Required)
	}
	if s.Pairs > 0 {
		s.Interference = float64(s.Inversions) / float64(s.Pairs)
	}
	return s
}
func Facts(answer string, q Query) (int, int) {
	hits := 0
	for _, f := range q.Facts {
		for _, alias := range f.Any {
			if strings.Contains(strings.ToLower(answer), strings.ToLower(alias)) {
				hits++
				break
			}
		}
	}
	return hits, len(q.Facts)
}

// Extraction comparison anchors items to their exact source quote, so reordered
// model output is accepted without matching unrelated items to convenient gold.
// Names use set micro-F1 (including extra predictions); dates/nature use item
// accuracy with extra or missing items counted wrong. Null gold time matters.
type Counts struct {
	Correct int `json:"correct"`
	Total   int `json:"total"`
}

func (c Counts) Accuracy() float64 {
	if c.Total == 0 {
		return 1
	}
	return float64(c.Correct) / float64(c.Total)
}

type ExtractionScore struct {
	Document string `json:"document"`
	People   Counts `json:"people"`
	Places   Counts `json:"places"`
	Time     Counts `json:"time"`
	Nature   Counts `json:"nature"`
}

func names(g, p []string) Counts {
	gs := map[string]bool{}
	ps := map[string]bool{}
	for _, n := range g {
		gs[strings.ToLower(strings.TrimSpace(n))] = true
	}
	for _, n := range p {
		ps[strings.ToLower(strings.TrimSpace(n))] = true
	}
	c := Counts{Total: len(gs) + len(ps)}
	for n := range gs {
		if ps[n] {
			c.Correct += 2
		}
	}
	return c
}
func timeEqual(a, b *When) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.From == b.From && a.To == b.To && a.Precision == b.Precision
}
func CompareExtraction(d Document, pred []Item) ExtractionScore {
	s := ExtractionScore{Document: d.ID}
	used := map[int]bool{}
	add := func(dst *Counts, src Counts) { dst.Correct += src.Correct; dst.Total += src.Total }
	for _, g := range d.Gold.Items {
		found := -1
		for n, p := range pred {
			if !used[n] && p.Quote == g.Quote {
				found = n
				break
			}
		}
		if found < 0 {
			add(&s.People, names(g.People, nil))
			add(&s.Places, names(g.Places, nil))
			s.Time.Total++
			s.Nature.Total++
			continue
		}
		used[found] = true
		p := pred[found]
		add(&s.People, names(g.People, p.People))
		add(&s.Places, names(g.Places, p.Places))
		s.Time.Total++
		s.Nature.Total++
		if timeEqual(g.When, p.When) {
			s.Time.Correct++
		}
		if g.Nature == p.Nature {
			s.Nature.Correct++
		}
	}
	for n, p := range pred {
		if used[n] {
			continue
		}
		add(&s.People, names(nil, p.People))
		add(&s.Places, names(nil, p.Places))
		s.Time.Total++
		s.Nature.Total++
	}
	return s
}

// RetrievalTotals uses raw counts for micro-averages. Coverage reports keep
// query types separate so eight paraphrases of one set cannot hide failures.
type RetrievalTotals struct {
	Tier         string  `json:"tier"`
	Type         string  `json:"type"`
	Queries      int     `json:"queries"`
	Delivered    int     `json:"delivered"`
	Required     int     `json:"required"`
	Inversions   int     `json:"inversions"`
	Pairs        int     `json:"pairs"`
	Recall       float64 `json:"recall"`
	Interference float64 `json:"interference"`
}

func AggregateRetrieval(tier string, c Corpus, scores []RetrievalScore) []RetrievalTotals {
	byID := map[string]Query{}
	for _, q := range c.Queries() {
		byID[q.ID] = q
	}
	groups := map[string]*RetrievalTotals{}
	for _, typ := range append(append([]string{}, QueryTypes...), "all") {
		groups[typ] = &RetrievalTotals{Tier: tier, Type: typ}
	}
	for _, s := range scores {
		typ := QueryType(c, byID[s.Query])
		for _, key := range []string{typ, "all"} {
			g := groups[key]
			g.Queries++
			g.Delivered += s.Delivered
			g.Required += s.Required
			g.Inversions += s.Inversions
			g.Pairs += s.Pairs
		}
	}
	var out []RetrievalTotals
	for _, typ := range append(append([]string{}, QueryTypes...), "all") {
		g := groups[typ]
		if g.Required > 0 {
			g.Recall = float64(g.Delivered) / float64(g.Required)
		}
		if g.Pairs > 0 {
			g.Interference = float64(g.Inversions) / float64(g.Pairs)
		}
		out = append(out, *g)
	}
	return out
}
