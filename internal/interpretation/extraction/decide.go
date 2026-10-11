package extraction

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Mention is an entity that a statement names.
type Mention struct {
	Name string
	Role string
}

// GroundedMentions keeps the names that the text contains. It drops a name
// that is empty, longer than 40 characters, repeated, or ambiguous, and it
// keeps eight names at most.
func GroundedMentions(names []string, role, text string) []Mention {
	out := []Mention{}
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if name == "" || utf8.RuneCountInString(name) > 40 || seen[key] || !strings.Contains(text, name) || memory.AmbiguousEntityName(name) || role == "person" && oneOf(name, "我", "我们") {
			continue
		}
		seen[key] = true
		out = append(out, Mention{Name: name, Role: role})
		if len(out) == 8 {
			break
		}
	}
	return out
}

// Facts are what is known about the source when its items are written.
type Facts struct {
	// Source is the source with the text that the model read.
	Source memory.SourceResult
	// Imported is true for a source of an uploaded archive.
	Imported bool
	// Undone is true when the owner undid the whole secretary turn of this
	// source. Remembered is true when that turn also asked to remember.
	Undone, Remembered bool
	// MentionText is the text in which a named entity must occur.
	MentionText string
	Location    *time.Location
}

// Action is what one item becomes.
type Action int

const (
	// Skip writes nothing.
	Skip Action = iota
	// Capture makes a task or an idea.
	Capture
	// Candidate saves the item for the owner's review.
	Candidate
	// Remember writes a statement and saves the item as accepted.
	Remember
)

// Decision is the action for one item, with the item after the speaker rules.
type Decision struct {
	Action Action
	Item   Item
	// The fields below are set for Remember.
	Confirmation, Subject, SubjectType string
	EventFrom, EventTo                 *time.Time
	EventPrecision                     string
	Mentions                           []Mention
}

// Decide applies the rules of one item.
// An undone turn leaves no plan or intention, and it leaves a statement only
// when the turn asked to remember. The secretary owns the tasks and ideas of
// its own turn. A source of an archive is history: its statements wait for
// review.
func Decide(item Item, facts Facts) Decision {
	source := facts.Source
	if facts.Undone && item.Kind == "memory" && (oneOf(item.Nature, "plan", "intention") || !facts.Remembered) {
		return Decision{Item: item}
	}
	if source.Source.Connector == "desk" && oneOf(item.Kind, "task", "idea") {
		return Decision{Item: item}
	}
	item = Normalize(item, source.Context)
	d := Decision{Item: item}
	if !Grounded(item, source.Source.Text) {
		return d
	}
	if oneOf(item.Kind, "task", "idea") {
		d.Action = Capture
		return d
	}
	if !Statement(item) {
		d.Action = Candidate
		return d
	}
	d.Action = Remember
	d.Confirmation = Confirmation(source, item)
	if facts.Imported {
		d.Confirmation = "candidate"
	}
	d.Subject = item.Subject
	d.EventFrom, d.EventTo, d.EventPrecision = Event(item.When, source, facts.Location)
	d.Mentions = append(d.Mentions, GroundedMentions(item.People, "person", facts.MentionText)...)
	d.Mentions = append(d.Mentions, GroundedMentions(item.Places, "place", facts.MentionText)...)
	d.Mentions = append(d.Mentions, GroundedMentions(item.Organizations, "organization", facts.MentionText)...)
	if source.Context == nil || source.Context.Role == "user" {
		if SelfSubject(item.Subject) {
			d.SubjectType = "self"
		} else if names := GroundedMentions([]string{item.Subject}, "person", facts.MentionText); len(names) > 0 {
			d.Subject, d.SubjectType = names[0].Name, "person"
			for _, org := range d.Mentions {
				if org.Role == "organization" && strings.EqualFold(org.Name, d.Subject) {
					d.SubjectType = "organization"
				}
			}
		}
	}
	return d
}

// UsableSignals returns the wake signals of an answer. Only the owner's
// current words can wake an idea.
func UsableSignals(output Output, source memory.SourceResult, imported bool) []Signal {
	if imported || source.Context != nil && (source.Context.Role != "user" || source.Context.Branch == "historical") {
		return nil
	}
	return output.Signals
}
