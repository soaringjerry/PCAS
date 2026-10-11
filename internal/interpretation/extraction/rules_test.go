package extraction

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestLongSourceIsReadInOverlappingWindowsThatCoverIt(t *testing.T) {
	if len(WindowStarts(Window)) != 0 {
		t.Fatal("a text that fits one window has no windows")
	}
	for length, want := range map[int][]int{Window + 1: {0, Step}, 23000: {0, Step}, 23001: {0, Step, 2 * Step}} {
		got := WindowStarts(length)
		if !reflect.DeepEqual(got, want) {
			t.Fatal(length, got)
		}
		if last := got[len(got)-1]; last+Window < length || last >= length {
			t.Fatal("the last window must reach the end", length, got)
		}
	}
	text := []rune(strings.Repeat("云", Window+5))
	if w, ok := WindowText(text, Step); !ok || len([]rune(w)) != Window+5-Step {
		t.Fatal(len([]rune(w)), ok)
	}
	if _, ok := WindowText(text, len(text)); ok {
		t.Fatal("a start after the text is not a window")
	}
}

func TestOutputIsReadStrictlyWithOrWithoutACodeFence(t *testing.T) {
	answer := `{"signals":[],"items":[{"kind":"memory","text":"虚构的云杉喜欢茶。","nature":"preference","subject":"我","predicate":"喜欢","quote":"喜欢茶","confidence":0.9,"explicit":true,"acquisition":"direct"}]}`
	for _, text := range []string{answer, "```json\n" + answer + "\n```", "  ```\n" + answer + "```"} {
		out, ok := ParseOutput(text)
		if !ok || len(out.Items) != 1 || out.Items[0].Quote != "喜欢茶" {
			t.Fatal(text, ok)
		}
	}
	for _, text := range []string{"虚构的散文。", `{"items":[],"signals":[],"extra":1}`, answer + answer} {
		if _, ok := ParseOutput(text); ok {
			t.Fatal(text)
		}
	}
}

func TestOnlyTheOwnersCurrentWordsStayExplicit(t *testing.T) {
	item := Item{Kind: "memory", Text: "喜欢茶", Subject: "我", Explicit: true, Acquisition: "direct"}
	if got := Normalize(item, nil); !got.Explicit || got.Acquisition != "direct" {
		t.Fatal(got)
	}
	if got := Normalize(Item{Acquisition: "guessed"}, &memory.SourceContext{Role: "user"}); got.Acquisition != "inferred" {
		t.Fatal(got)
	}
	if got := Normalize(item, &memory.SourceContext{Role: "user", Branch: "historical"}); got.Explicit || got.Acquisition != "inferred" || got.Subject != "我" {
		t.Fatal(got)
	}
	if got := Normalize(item, &memory.SourceContext{Role: "assistant"}); got.Explicit || got.Subject != "AI 或工具（非用户）" || !strings.HasPrefix(got.Text, "AI 或工具当时的表达：") {
		t.Fatal(got)
	}
}

func TestAnItemNeedsAQuotationThatTheSourceContains(t *testing.T) {
	source := "虚构的云杉说她喜欢茶。"
	valid := Item{Kind: "memory", Text: "云杉喜欢茶", Quote: "喜欢茶", Confidence: 0.7}
	if !Grounded(valid, source) {
		t.Fatal("valid item")
	}
	for name, change := range map[string]func(*Item){
		"no text":          func(i *Item) { i.Text = " " },
		"no quotation":     func(i *Item) { i.Quote = "" },
		"quotation absent": func(i *Item) { i.Quote = "喜欢咖啡" },
		"unknown kind":     func(i *Item) { i.Kind = "note" },
		"confidence":       func(i *Item) { i.Confidence = 1.1 },
	} {
		item := valid
		change(&item)
		if Grounded(item, source) {
			t.Fatal(name)
		}
	}
	if !Statement(Item{Kind: "memory", Nature: "plan"}) || Statement(Item{Kind: "memory", Nature: "question"}) || Statement(Item{Kind: "task", Nature: "plan"}) {
		t.Fatal("statement natures")
	}
	if !SelfSubject(" 我们 ") || SelfSubject("云杉") {
		t.Fatal("self subject")
	}
}

func TestEventTimeNeedsItsQuotationAndAKnownTimeForARelativeDay(t *testing.T) {
	loc := time.FixedZone("fictitious", 8*3600)
	said := time.Date(2026, 3, 1, 9, 0, 0, 0, loc)
	source := memory.SourceResult{Source: memory.Source{Text: "虚构：三月去了一趟海边，明天去爬山。"}}
	source.Source.ExpressedAt = &said
	from, to, precision := Event(&When{From: "2026-03-05", To: "2026-03-05", Precision: "month", Quote: "三月"}, source, loc)
	if precision != "month" || !from.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, loc)) {
		t.Fatal(from, to, precision)
	}
	if _, _, precision := Event(&When{From: "2026-03-02", To: "2026-03-02", Precision: "day", Quote: "明天"}, source, loc); precision != "day" {
		t.Fatal(precision)
	}
	source.Source.ExpressedAt = nil
	source.Source.Connector = "archive"
	for name, when := range map[string]*When{
		"relative day without a time": {From: "2026-03-02", To: "2026-03-02", Precision: "day", Quote: "明天"},
		"quotation absent":            {From: "2026-03-02", To: "2026-03-02", Precision: "day", Quote: "后年"},
		"range that does not advance": {From: "2026-03-02", To: "2026-03-02", Precision: "range", Quote: "三月"},
		"none":                        nil,
	} {
		if from, _, precision := Event(when, source, loc); from != nil || precision != "unknown" {
			t.Fatal(name, precision)
		}
	}
}

func TestOnlyADirectConfidentCurrentStatementIsAdopted(t *testing.T) {
	source := memory.SourceResult{Source: memory.Source{Connector: "desk", Text: "我喜欢茶。也许以后会搬家。"}}
	direct := Item{Quote: "我喜欢茶", Acquisition: "direct", Confidence: 0.9}
	if Confirmation(source, direct) != "adopted" {
		t.Fatal("direct statement")
	}
	for name, item := range map[string]Item{
		"doubt in the same sentence": {Quote: "以后会搬家", Acquisition: "direct", Confidence: 0.9},
		"low confidence":             {Quote: "我喜欢茶", Acquisition: "direct", Confidence: 0.5},
		"reported":                   {Quote: "我喜欢茶", Acquisition: "reported", Confidence: 0.9},
		"model's qualification":      {Quote: "我喜欢茶", Acquisition: "direct", Confidence: 0.9, Qualification: "tentative"},
	} {
		if Confirmation(source, item) != "candidate" {
			t.Fatal(name)
		}
	}
	source.Source.Connector = "archive"
	if Confirmation(source, direct) != "candidate" {
		t.Fatal("an import is reviewed")
	}
}
