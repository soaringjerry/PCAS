package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func clip(s string, n int) string {
	// Telegram measures entity offsets in UTF-16. Counting supplementary
	// characters twice also keeps emoji-heavy messages below its text limit.
	units := 0
	for _, r := range s {
		units++
		if r > 0xffff {
			units++
		}
	}
	if units <= n {
		return s
	}
	units = 0
	for i, r := range s {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if units+size > n-1 {
			return s[:i] + "…"
		}
		units += size
	}
	return s
}

func cardItems[T any](items any) []T {
	b, _ := json.Marshal(items)
	var out []T
	_ = json.Unmarshal(b, &out)
	return out
}

func date(s *string, loc *time.Location) string {
	if s == nil || *s == "" {
		return "未定"
	}
	if t, err := time.Parse(time.RFC3339, *s); err == nil {
		return t.In(loc).Format("01-02 15:04")
	}
	return clip(*s, 20)
}

func format(turn workspace.SecretaryTurn, prefix, timezone string) (string, [][]button) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	var lines []string
	var rows [][]button
	if prefix != "" {
		lines = append(lines, clip(prefix, 1000))
	}
	if turn.Reply != "" {
		lines = append(lines, clip(turn.Reply, 1000))
	}
	for _, r := range turn.Receipts {
		sign := "✓ "
		if r.Status == "skipped" {
			sign = "· "
		}
		text := r.Text
		if r.Undone {
			text += "（已撤销）"
		}
		lines = append(lines, sign+clip(text, 240))
		if r.Undoable && !r.Undone && r.ActionID != nil && len(*r.ActionID) <= 62 {
			rows = append(rows, []button{{Text: "撤销 " + clip(r.Text, 20), Data: "u:" + *r.ActionID}})
		}
	}
	for _, card := range turn.Cards {
		switch card.Kind {
		case "sources":
			lines = append(lines, fmt.Sprintf("依据 %d 条记录", len(cardItems[workspace.DeskSourceItem](card.Items))))
		case "links":
			for _, item := range cardItems[workspace.DeskLinkItem](card.Items) {
				lines = append(lines, clip(item.Host, 100))
			}
		case "timeline":
			for _, item := range cardItems[workspace.DeskTimelineItem](card.Items) {
				lines = append(lines, date(item.At, loc)+" · "+clip(item.Text, 160))
			}
		case "tasks":
			for _, item := range cardItems[workspace.DeskTaskItem](card.Items) {
				lines = append(lines, "○ "+clip(item.Title, 160)+" · "+date(item.Due, loc))
			}
		}
	}
	// Keep the question visible even when cards fill the message.
	text := clip(strings.Join(lines, "\n"), 3300)
	if turn.Ask != nil {
		text += "\n" + clip(turn.Ask.Question, 500)
		for i, option := range turn.Ask.Options {
			if len(rows) >= 90 {
				break
			}
			rows = append(rows, []button{{Text: clip(option, 60), Data: fmt.Sprintf("a:%d", i)}})
		}
	}
	if strings.TrimSpace(text) == "" {
		text = "收到。"
	}
	return strings.TrimSpace(text), rows
}
