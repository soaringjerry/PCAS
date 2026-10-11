package claim

import (
	"testing"
	"time"

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

func TestAStatementRestsOnItsQuotationOrOnTheWholeSource(t *testing.T) {
	text := "虚构的云杉喜欢茶。她住在虚构城。"
	if start, end, ok := QuoteSpan(text, "喜欢茶"); !ok || start != 5 || end != 8 {
		t.Fatal(start, end, ok)
	}
	if start, end, ok := QuoteSpan(text, ""); !ok || start != 0 || end != 16 {
		t.Fatal(start, end, ok)
	}
	if _, _, ok := QuoteSpan(text, "喜欢咖啡"); ok {
		t.Fatal("a quotation that the source does not contain is not valid")
	}
}

func TestTheSameAssertionHasTheSameKeyAndItsOwnTime(t *testing.T) {
	a := Statement{Subject: "我", Predicate: "喜欢", Text: "喜欢茶"}
	if Key(a) != Key(Statement{Subject: "我", Predicate: "喜欢", Text: "喜欢茶", Quote: "别的引文"}) || Key(a) == Key(Statement{Subject: "我", Predicate: "喜欢", Text: "喜欢咖啡"}) {
		t.Fatal("key")
	}
	said, recorded := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	if got := ExpressedAt(&said, nil, recorded, "archive"); got != &said {
		t.Fatal("the statement's own time")
	}
	if got := ExpressedAt(nil, &said, recorded, "archive"); got != &said {
		t.Fatal("the time of the source")
	}
	if got := ExpressedAt(nil, nil, recorded, "desk"); got == nil || !got.Equal(recorded) {
		t.Fatal("the record time of the owner's own input")
	}
	if ExpressedAt(nil, nil, recorded, "archive") != nil {
		t.Fatal("an import without a time has none")
	}
	scope := Scope(Statement{ProjectID: "p1", ExtractionRun: "run", ExtractionRef: 4})
	if scope["project_id"] != "p1" || scope["conversation_extraction"] != "run" || scope["conversation_ref"] != "4" || len(Scope(Statement{})) != 0 {
		t.Fatalf("%v", scope)
	}
}
