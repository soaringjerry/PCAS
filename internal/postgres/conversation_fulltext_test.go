package postgres

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestConversationFullTextCoverage(t *testing.T) {
	texts := []string{
		"开头用户消息",
		strings.Repeat("长方案🙂", 6500) + "AI方案末尾标记",
		strings.Repeat("用户资料🙂", 6500) + "用户资料末尾标记",
		"最后的用户更正",
	}
	sources := []memory.SourceResult{}
	for i, text := range texts {
		role := "user"
		if i == 1 {
			role = "assistant"
		}
		sources = append(sources, memory.SourceResult{Source: memory.Source{Text: text}, Context: &memory.SourceContext{Role: role, Branch: "current"}})
	}
	visible := map[int]string{}
	for _, segment := range splitConversation(sources) {
		seen := map[int]bool{}
		for _, message := range segment.Messages {
			if seen[message.Index] {
				t.Fatal("two fragments of one message in the same segment")
			}
			seen[message.Index] = true
			if message.Role == "user" && utf8.RuneCountInString(message.Text) > 12000 {
				t.Fatal("user fragment exceeds budget")
			}
			visible[message.Index] += message.Text
		}
	}
	for i, original := range texts {
		if visible[i+1] != original {
			t.Fatalf("message %d lost or repeated text: original=%d supplied=%d", i+1, utf8.RuneCountInString(original), utf8.RuneCountInString(visible[i+1]))
		}
		if sources[i].Source.Text != original {
			t.Fatal("stored original changed")
		}
	}
}

func TestConversationLongAIContextReachesFollowingAgreement(t *testing.T) {
	proposal := strings.Repeat("方案正文", 8000) + "最后决定去成都。"
	sources := []memory.SourceResult{
		{Source: memory.Source{Text: "帮我安排一次旅行。"}, Context: &memory.SourceContext{Role: "user"}},
		{Source: memory.Source{Text: proposal}, Context: &memory.SourceContext{Role: "assistant"}},
		{Source: memory.Source{Text: "就按这个方案。"}, Context: &memory.SourceContext{Role: "user"}},
	}
	for _, segment := range splitConversation(sources) {
		for _, message := range segment.Messages {
			if message.Index != 3 {
				continue
			}
			for _, previous := range segment.Context {
				if previous.Index == 2 && previous.Text == proposal {
					return
				}
			}
			t.Fatal("agreement did not receive the complete preceding AI proposal")
		}
	}
	t.Fatal("agreement was not submitted")
}

func TestConversationFullTextProgressAndTailEvidence(t *testing.T) {
	s, scope := testStore(t), owner()
	const head = "原先想去大理。"
	const tail = "最后改成去成都。"
	text := head + strings.Repeat("填", 12000-utf8.RuneCountInString(head)) + tail
	calls := 0
	var archive b4bArchive
	f := b4bModel(t, s, func(in b4bInput) any {
		calls++
		if calls == 1 {
			return map[string]any{"items": []any{b4bItem(1, "想去大理", head, "plan")}}
		}
		if got := b4bProgress(t, s, scope, archive.Batch).Organized; got != 0 {
			t.Fatalf("incomplete long message marked organized: %d", got)
		}
		if len(in.Earlier) != 1 || in.Earlier[0].Text != "想去大理" {
			t.Fatal("earlier fragment memory missing", in.Earlier)
		}
		return map[string]any{"items": []any{
			b4bItem(1, "改成去成都", tail, "plan"),
			b4bItem(1, "错误引用前一片", head, "plan"),
		}, "withdraw": []int{in.Earlier[0].Ref}}
	})
	archive = b4bImport(t, s, scope, b4bMessages(t, []string{"user"}, []string{text}))
	b4bOperation(t, s, scope, archive.Batch, "organize")
	b4bDrain(t, s)
	if len(f.all()) != 2 {
		t.Fatalf("tail was not processed: calls=%d", len(f.all()))
	}
	memories := b2Snapshot(t, s, scope).Memories
	if len(memories) != 1 || memories[0].Text != "改成去成都" {
		t.Fatal("tail correction/evidence failed", memories)
	}
	b4bEvidence(t, s, scope, memories[0], archive.Sources[0], tail, archive.Messages[0].At)
	if got := b4bProgress(t, s, scope, archive.Batch).Organized; got != 1 {
		t.Fatal("message must count once after all fragments", got)
	}
	original, err := s.GetSource(context.Background(), scope, archive.Sources[0].ID, archive.Sources[0].Version)
	if err != nil || original.Source.Text != text {
		t.Fatal("original was changed", err)
	}
}
