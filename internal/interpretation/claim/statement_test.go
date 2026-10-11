package claim

import (
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

var source = memory.Ref{ID: "11111111-1111-4111-8111-111111111111", Version: 1, Kind: memory.SourceKind}

func TestAnAbsentFieldOfAStatementHasOneMeaning(t *testing.T) {
	got, err := Normalize(Statement{Text: "喜欢茶", Nature: "preference", Source: source})
	if err != nil || got.Subject != "未解析主体" || got.Predicate != "描述" || got.Actor != "user" || got.Confirmation != "confirmed" || got.Acquisition != "direct" {
		t.Fatalf("%+v %v", got, err)
	}
	// What the AI wrote without a stated source is an inference.
	if got, _ := Normalize(Statement{Text: "喜欢茶", Nature: "fact", Actor: "ai", Source: source}); got.Acquisition != "inferred" {
		t.Fatal(got.Acquisition)
	}
	// A structured subject that names no one in particular is dropped with its type.
	if got, _ := Normalize(Statement{Text: "他喜欢茶", Nature: "fact", Structured: true, Subject: "他", SubjectType: "person", Source: source}); got.Subject != "未解析主体" || got.SubjectType != "" {
		t.Fatalf("%+v", got)
	}
	if got, _ := Normalize(Statement{Text: "他喜欢茶", Nature: "fact", Subject: "他", SubjectType: "person", Source: source}); got.Subject != "他" {
		t.Fatal("a subject that the owner wrote is kept")
	}
}

func TestAStatementNeedsTextAKnownNatureAndASourceVersion(t *testing.T) {
	valid := Statement{Text: "喜欢茶", Nature: "fact", Source: source}
	for name, change := range map[string]func(*Statement){
		"no text":                func(s *Statement) { s.Text = "  " },
		"an unknown nature":      func(s *Statement) { s.Nature = "question" },
		"an unknown acquisition": func(s *Statement) { s.Acquisition = "guessed" },
		"no source":              func(s *Statement) { s.Source = memory.Ref{} },
		"no source version":      func(s *Statement) { s.Source.Version = 0 },
	} {
		in := valid
		change(&in)
		if _, err := Normalize(in); err != memory.ErrInvalid {
			t.Fatal(name, err)
		}
	}
	if _, err := Normalize(Statement{Text: "执行了一步", Nature: "fact", Acquisition: "execution", Source: source}); err != nil {
		t.Fatal(err)
	}
}
