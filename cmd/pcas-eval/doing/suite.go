// Package doing implements only the independent phase 2.5 evaluation.
package doing

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

var Categories = []string{"direct_recall", "indirect_use", "cross_group", "updated_fact", "outgoing", "irrelevant"}

type Memory struct {
	ID           string   `json:"id"`
	ExpressedAt  string   `json:"expressed_at"`
	RecordedAt   string   `json:"recorded_at,omitempty"`
	Group        string   `json:"group"`
	Text         string   `json:"text"`
	Tags         []string `json:"tags"`
	Supersedes   []string `json:"supersedes,omitempty"`
	SupersededBy string   `json:"superseded_by,omitempty"`
	DuplicateOf  string   `json:"duplicate_of,omitempty"`
}
type Check struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
}
type Task struct {
	Reviewed    bool    `json:"reviewed,omitempty"`
	ID          string  `json:"id"`
	Category    string  `json:"category"`
	Request     string  `json:"request"`
	Must        []Check `json:"must"`
	Bonus       []Check `json:"bonus"`
	Forbidden   []Check `json:"forbidden"`
	CanComplete bool    `json:"can_complete_without_followup"`
	Handling    string  `json:"reasonable_handling"`
}
type Suite struct {
	Version   int      `json:"schema_version"`
	Synthetic bool     `json:"synthetic"`
	Persona   string   `json:"persona"`
	Timezone  string   `json:"timezone"`
	AsOf      string   `json:"as_of"`
	Memories  []Memory `json:"memories"`
	Tasks     []Task   `json:"tasks"`
}

func Load(path string) (Suite, error) {
	var s Suite
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	return s, s.Validate()
}
func (s Suite) ByID() map[string]Memory {
	out := map[string]Memory{}
	for _, m := range s.Memories {
		out[m.ID] = m
	}
	return out
}

// Full synthetic requirements apply only to the frozen public suite. Reviewed
// private subsets still require structural and reference integrity.
func (s Suite) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported suite version")
	}
	asof, err := time.Parse(time.RFC3339, s.AsOf)
	if err != nil {
		return fmt.Errorf("invalid as_of")
	}
	if _, err = time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("invalid timezone")
	}
	if s.Synthetic && len(s.Memories) < 600 {
		return fmt.Errorf("synthetic suite needs 600 memories")
	}
	ids := map[string]Memory{}
	for _, m := range s.Memories {
		at, err := time.Parse(time.RFC3339, m.ExpressedAt)
		if err != nil || at.After(asof) || m.ID == "" || m.Text == "" || m.Group == "" {
			return fmt.Errorf("invalid memory %s", m.ID)
		}
		if _, ok := ids[m.ID]; ok {
			return fmt.Errorf("duplicate memory id %s", m.ID)
		}
		if m.RecordedAt != "" {
			if _, err := time.Parse(time.RFC3339, m.RecordedAt); err != nil {
				return fmt.Errorf("invalid recorded_at %s", m.ID)
			}
		}
		ids[m.ID] = m
	}
	for _, m := range s.Memories {
		if m.DuplicateOf != "" {
			src, ok := ids[m.DuplicateOf]
			if !ok || src.Text != m.Text || src.ExpressedAt != m.ExpressedAt {
				return fmt.Errorf("invalid duplicate %s", m.ID)
			}
		}
		for _, id := range m.Supersedes {
			old, ok := ids[id]
			if !ok || old.SupersededBy != m.ID || old.ExpressedAt >= m.ExpressedAt {
				return fmt.Errorf("invalid update %s", m.ID)
			}
		}
		if m.SupersededBy != "" {
			next, ok := ids[m.SupersededBy]
			found := false
			for _, id := range next.Supersedes {
				found = found || id == m.ID
			}
			if !ok || !found {
				return fmt.Errorf("invalid superseded link %s", m.ID)
			}
		}
	}
	categories := map[string]int{}
	for _, c := range Categories {
		categories[c] = 0
	}
	tasks := map[string]bool{}
	for _, t := range s.Tasks {
		if !s.Synthetic && !t.Reviewed {
			return fmt.Errorf("unreviewed private task %s", t.ID)
		}
		if _, ok := categories[t.Category]; !ok {
			return fmt.Errorf("unknown category %s", t.ID)
		}
		if tasks[t.ID] || t.ID == "" || t.Request == "" || t.Handling == "" || len(t.Must) < 3 || len(t.Must) > 6 || len(t.Forbidden) == 0 {
			return fmt.Errorf("invalid task %s", t.ID)
		}
		tasks[t.ID] = true
		categories[t.Category]++
		checks := map[string]bool{}
		for _, group := range [][]Check{t.Must, t.Bonus, t.Forbidden} {
			for _, c := range group {
				if checks[c.ID] || c.ID == "" || c.Text == "" {
					return fmt.Errorf("invalid check %s/%s", t.ID, c.ID)
				}
				checks[c.ID] = true
				if len(c.Evidence) == 0 && t.Category != "irrelevant" {
					return fmt.Errorf("missing evidence %s/%s", t.ID, c.ID)
				}
				for _, id := range c.Evidence {
					if _, ok := ids[id]; !ok {
						return fmt.Errorf("unknown evidence %s/%s", t.ID, id)
					}
				}
			}
		}
	}
	if s.Synthetic {
		for c, n := range categories {
			if n < 20 {
				return fmt.Errorf("category %s needs 20 tasks", c)
			}
		}
	}
	return nil
}

// IdealEvidence never includes the rubric, forbidden or bonus checks. Original
// wording and expression date are retained; metadata labels stay scoring-only.
func IdealEvidence(s Suite, t Task) string {
	by := s.ByID()
	seen := map[string]bool{}
	out := ""
	for _, c := range t.Must {
		for _, id := range c.Evidence {
			if !seen[id] {
				m := by[id]
				out += fmt.Sprintf("[%s / %s] %s\n", id, m.ExpressedAt, m.Text)
				seen[id] = true
			}
		}
	}
	return out
}
