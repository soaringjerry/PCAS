// Package claim owns the memory interpretation rules of one statement that
// is about to be written as a memory: what it must hold and what an absent
// field means. The rules need no storage.
package claim

import (
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Mention is an entity that a statement names.
type Mention struct {
	Name string
	Role string
}

// Statement is one statement with its source, as a caller gives it to the
// memory write.
type Statement struct {
	ExtractionRun   string
	ExtractionRef   int
	AllowNewAtQuote bool
	Structured      bool
	SubjectType     string
	Mentions        []Mention
	ExpressedAt     *time.Time
	EventFrom       *time.Time
	EventTo         *time.Time
	EventPrecision  string
	Text            string
	Nature          string
	ProjectID       string
	Subject         string
	Predicate       string
	Confirmation    string
	Acquisition     string
	Actor           string
	Quote           string
	Source          memory.Ref
}

// Normalize checks a statement and fills its absent fields. A statement
// needs text, a known nature, and a source version. A subject that names no
// one in particular is dropped. Without other word, the owner said it, it is
// confirmed, and it was said directly; what the AI wrote is an inference.
func Normalize(in Statement) (Statement, error) {
	if strings.TrimSpace(in.Text) == "" || len(in.Text) > 1<<20 {
		return in, memory.ErrInvalid
	}
	if in.Structured && memory.AmbiguousEntityName(in.Subject) {
		in.Subject = ""
		in.SubjectType = ""
	}
	if !oneOf(in.Nature, "fact", "preference", "intention", "plan", "decision") {
		return in, memory.ErrInvalid
	}
	if in.Subject == "" {
		in.Subject = "未解析主体"
	}
	if in.Predicate == "" {
		in.Predicate = "描述"
	}
	if in.Actor == "" {
		in.Actor = "user"
	}
	if in.Confirmation == "" {
		in.Confirmation = "confirmed"
	}
	if in.Acquisition == "" {
		in.Acquisition = "direct"
		if in.Actor == "ai" {
			in.Acquisition = "inferred"
		}
	}
	if !oneOf(in.Acquisition, "direct", "reported", "inferred", "execution") {
		return in, memory.ErrInvalid
	}
	if !in.Source.ID.Valid() || in.Source.Version < 1 {
		return in, memory.ErrInvalid
	}
	return in, nil
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
