package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestFormatCardsAndLimits(t *testing.T) {
	at := "2026-10-01T07:00:00Z"
	id := "11111111-1111-4111-8111-111111111111"
	turn := workspace.SecretaryTurn{Reply: strings.Repeat("文字", 5000), Receipts: []workspace.DeskReceipt{
		{Text: "跳过", Status: "skipped"}, {ActionID: &id, Text: "已经撤销", Undoable: true, Undone: true},
	}, Cards: []workspace.DeskCard{
		{Kind: "sources", Items: []workspace.DeskSourceItem{{MemoryID: id}, {MemoryID: id}}},
		{Kind: "links", Items: []workspace.DeskLinkItem{{Host: "example.com"}}},
		{Kind: "timeline", Items: []workspace.DeskTimelineItem{{At: &at, Text: "过去的计划"}}},
		{Kind: "tasks", Items: []workspace.DeskTaskItem{{Due: &at, Title: "开会"}}},
		{Kind: "unknown", Items: "ignored"},
	}, Ask: &workspace.DeskAsk{Question: "选哪个？", Options: []string{"一", "二"}}}
	text, rows := format(turn, "", "Asia/Shanghai")
	for _, want := range []string{"· 跳过", "（已撤销）", "依据 2 条记录", "example.com", "10-01 15:00 · 过去的计划", "○ 开会 · 10-01 15:00", "选哪个？"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing", want, text)
		}
	}
	if len(rows) != 2 || rows[1][0].Data != "a:1" || utf8.RuneCountInString(text) > 4096 {
		t.Fatal(rows)
	}
	turn.Cards = []workspace.DeskCard{{Kind: "tasks", Items: []workspace.DeskTaskItem{{Title: strings.Repeat("📚", 10000)}}}}
	text, _ = format(turn, strings.Repeat("🎤", 5000), "")
	if utf8.RuneCountInString(text) > 4096 || !utf8.ValidString(text) || !strings.HasSuffix(text, "选哪个？") {
		t.Fatal("bad truncation")
	}
}
