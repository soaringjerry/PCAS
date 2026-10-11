package extraction

import (
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func message(role, text string) memory.SourceResult {
	s := memory.SourceResult{Context: &memory.SourceContext{Role: role}}
	s.Source.Text = text
	return s
}

func TestConversationIsSplitWithTwoMessagesOfContextAndLongUserTextInParts(t *testing.T) {
	long := strings.Repeat("云", 2*segmentCharacters+1)
	sources := []memory.SourceResult{message("user", "虚构的第一句"), message("assistant", "虚构的回答"), message("user", long), message("user", "虚构的最后一句")}
	segments := SplitConversation(sources)
	if len(segments) != 5 {
		t.Fatal(len(segments))
	}
	if len(segments[0].Messages) != 2 || len(segments[0].Context) != 0 {
		t.Fatal("the first segment holds the short messages")
	}
	for part := 1; part <= 3; part++ {
		m := segments[part].Messages[0]
		if m.Index != 3 || m.Part != part || m.Parts != 3 || len(segments[part].Context) == 0 {
			t.Fatalf("%d %+v", part, m)
		}
	}
	if got := segments[4]; got.Messages[0].Index != 4 || len(got.Context) != 2 || got.Context[0].Index != 2 || got.Context[1].Index != 3 {
		t.Fatalf("%+v", got.Context)
	}
	historical := message("user", "虚构的旧分支")
	historical.Context.Branch = "historical"
	if len(CurrentBranch(append(sources, historical))) != 4 {
		t.Fatal("a historical branch is not read")
	}
}

func TestConversationRunChangesWithItsMessagesAndStageKeepsItsNumbers(t *testing.T) {
	a, b := message("user", "虚构的一句"), message("assistant", "虚构的回答")
	run := ConversationRun([]memory.SourceResult{a, b})
	if len(run) != 64 || run != ConversationRun([]memory.SourceResult{a, b}) || run == ConversationRun([]memory.SourceResult{b, a}) {
		t.Fatal("the run names the messages and their order")
	}
	changed := a
	changed.Source.Text += "。"
	if run == ConversationRun([]memory.SourceResult{changed, b}) {
		t.Fatal("a changed text gives a new run")
	}
	gotRun, segment, next, err := ParseConversationStage(ConversationStage(run, 3, 17))
	if err != nil || gotRun != run || segment != 3 || next != 17 {
		t.Fatal(gotRun, segment, next, err)
	}
	for _, stage := range []string{ConversationPrefix + "short:0:1", ConversationPrefix + run + ":0:0", ConversationPrefix + run + ":x:1"} {
		if _, _, _, err := ParseConversationStage(stage); err != memory.ErrInvalid {
			t.Fatal(stage, err)
		}
	}
}

func TestOnlyAUsersMessageOfTheSegmentGivesAnItemAndItWaitsForReview(t *testing.T) {
	sources := []memory.SourceResult{message("user", "我喜欢虚构的茶。明天交报告。"), message("assistant", "你喜欢茶。")}
	messages := map[int]ConversationMessage{1: {Index: 1, Role: "user", Text: sources[0].Source.Text}, 2: {Index: 2, Role: "assistant", Text: sources[1].Source.Text}}
	grounding := sources[0].Source.Text + "\n" + sources[1].Source.Text
	statement := ConversationItem{MessageIndex: 1, Item: Item{Kind: "memory", Nature: "preference", Text: "喜欢茶", Subject: "我", Quote: "我喜欢虚构的茶", Confidence: 0.9, Acquisition: "stated"}}
	d, source := DecideConversation(statement, messages, sources, grounding, time.UTC)
	if d.Action != Remember || d.Confirmation != "candidate" || d.SubjectType != "self" || d.Item.Acquisition != "inferred" || source.Source.Text != sources[0].Source.Text {
		t.Fatalf("%+v", d)
	}
	task := ConversationItem{MessageIndex: 1, Item: Item{Kind: "task", Text: "交报告", Quote: "明天交报告", Confidence: 0.9}}
	if d, _ := DecideConversation(task, messages, sources, grounding, time.UTC); d.Action != Capture {
		t.Fatal(d.Action)
	}
	for name, change := range map[string]func(*ConversationItem){
		"the assistant's message":        func(i *ConversationItem) { i.MessageIndex = 2; i.Quote = "你喜欢茶" },
		"a message outside the segment":  func(i *ConversationItem) { i.MessageIndex = 9 },
		"a quotation of another message": func(i *ConversationItem) { i.Quote = "你喜欢茶" },
		"an unknown nature":              func(i *ConversationItem) { i.Nature = "question" },
		"an unknown kind":                func(i *ConversationItem) { i.Kind = "unknown" },
	} {
		item := statement
		change(&item)
		if d, _ := DecideConversation(item, messages, sources, grounding, time.UTC); d.Action != Skip {
			t.Fatal(name)
		}
	}
	if out, ok := ParseConversationOutput("```json\n{\"items\":[],\"withdraw\":[2]}\n```"); !ok || len(out.Withdraw) != 1 {
		t.Fatal(out, ok)
	}
	if _, ok := ParseConversationOutput(`{"items":[],"other":1}`); ok {
		t.Fatal("an unknown field is not valid")
	}
}
