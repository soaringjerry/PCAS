package organize

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Areas are the fixed life areas that every owner has.
var Areas = []string{"学业", "工作", "创业", "技术", "健康", "财务", "居住", "饮食", "出行", "关系", "兴趣"}

// Memory is one statement of a batch. N is local to the batch.
type Memory struct {
	N           int        `json:"n"`
	Text        string     `json:"text"`
	ExpressedAt *time.Time `json:"expressedAt"`
	Ref         memory.Ref `json:"-"`
}

// Group is a project, a topic, or an area that statements belong to.
type Group struct {
	Type        string    `json:"type"`
	Name        string    `json:"name"`
	Description string    `json:"desc"`
	ID          memory.ID `json:"-"`
}

// Item is the model's valid answer for one statement.
type Item struct {
	Category     string
	Durable      *bool
	Groups       []Group
	Deadlines    []Deadline
	Unrestricted bool
	Scope        string
}

// ValidCategory reports whether category is a statement category.
func ValidCategory(category string) bool {
	return oneOf(category, "identity", "taste", "rule", "goal", "progress", "event", "opinion", "other_person", "unknown")
}

// ValidName reports whether a group name can be used.
func ValidName(name string) bool { return strings.TrimSpace(name) != "" }

// ParseOutput reads the model's answer for a batch of size statements. It
// returns the valid items by their batch number and the groups that the model
// declared as new.
// Validate the whole item before replacing existing labels, membership, or dates.
// The first occurrence of a known n wins even when that occurrence is invalid.
func ParseOutput(text string, size int) (map[int]Item, []Group) {
	items := map[int]Item{}
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &envelope) != nil {
		return items, nil
	}
	var entries []json.RawMessage
	_ = json.Unmarshal(envelope["items"], &entries)
	seen := map[int]bool{}
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		if json.Unmarshal(entry, &fields) != nil {
			continue
		}
		var n int
		if json.Unmarshal(fields["n"], &n) != nil || n < 1 || n > size || seen[n] {
			continue
		}
		seen[n] = true
		var item Item
		if json.Unmarshal(fields["category"], &item.Category) != nil || !ValidCategory(item.Category) {
			continue
		}
		if json.Unmarshal(fields["durable"], &item.Durable) != nil || item.Durable == nil {
			continue
		}
		validGroups := true
		for _, kind := range []string{"project", "area"} {
			var name string
			if len(fields[kind]) > 0 && json.Unmarshal(fields[kind], &name) != nil {
				validGroups = false
				break
			}
			if strings.TrimSpace(name) != "" {
				item.Groups = append(item.Groups, Group{Type: kind, Name: strings.TrimSpace(name)})
			}
		}
		var topics []string
		if len(fields["topics"]) > 0 && (string(fields["topics"]) == "null" || json.Unmarshal(fields["topics"], &topics) != nil) {
			validGroups = false
		}
		for _, name := range topics {
			if strings.TrimSpace(name) != "" {
				item.Groups = append(item.Groups, Group{Type: "topic", Name: strings.TrimSpace(name)})
			}
		}
		if !validGroups {
			continue
		}

		if len(fields["deadlines"]) == 0 || string(fields["deadlines"]) == "null" || json.Unmarshal(fields["deadlines"], &item.Deadlines) != nil {
			continue
		}
		if item.Category == "rule" {
			var unrestricted *bool
			if json.Unmarshal(fields["unrestricted"], &unrestricted) != nil || unrestricted == nil || json.Unmarshal(fields["scope"], &item.Scope) != nil {
				continue
			}
			item.Unrestricted = *unrestricted
			if !item.Unrestricted && strings.TrimSpace(item.Scope) == "" {
				continue
			}
			if item.Unrestricted {
				item.Scope = ""
			}
		}
		validDeadlines := true
		for _, d := range item.Deadlines {
			if !oneOf(d.Kind, "deadline", "appointment", "recurring", "unclear") || strings.TrimSpace(d.Title) == "" || d.ScheduleRule != nil && !d.ScheduleRule.Valid() {
				validDeadlines = false
			}
		}
		if !validDeadlines {
			continue
		}
		items[n] = item
	}
	var declarations []json.RawMessage
	_ = json.Unmarshal(envelope["new"], &declarations)
	groups := []Group{}
	for _, entry := range declarations {
		var fields map[string]json.RawMessage
		if json.Unmarshal(entry, &fields) != nil {
			continue
		}
		var group Group
		_ = json.Unmarshal(fields["type"], &group.Type)
		_ = json.Unmarshal(fields["name"], &group.Name)
		_ = json.Unmarshal(fields["desc"], &group.Description)
		groups = append(groups, group)
	}
	return items, groups
}

// UsedDeclarations returns the declared groups that a new group can be made
// from: a known type, a usable name, and a reference from an item whose
// statement is still current. A group that no current statement uses is not
// created.
func UsedDeclarations(declared []Group, items map[int]Item, current map[int]bool) []Group {
	out := []Group{}
	for _, g := range declared {
		g.Name = strings.TrimSpace(g.Name)
		if !oneOf(g.Type, "topic", "project", "area") || !ValidName(g.Name) {
			continue
		}
		used := false
		for n, item := range items {
			if !current[n] {
				continue
			}
			for _, ref := range item.Groups {
				used = used || ref.Type == g.Type && strings.EqualFold(ref.Name, g.Name)
			}
		}
		if used {
			out = append(out, g)
		}
	}
	return out
}
