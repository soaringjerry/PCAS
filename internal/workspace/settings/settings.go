// Package settings owns the workspace rules for a change of the owner's
// settings and of an agent's settings. The rules need no storage.
package settings

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/itemcommand"
)

// cityLimit is the longest name of a city, in characters. The city is one
// line of each secretary input.
const cityLimit = 60

// budgetLimit is the largest daily budget that can be set.
const budgetLimit = 1e6

// CheckZone returns workspace.ErrTimezone when a patch names a time zone
// that is not valid. The caller can make this check before it reads.
func CheckZone(patch json.RawMessage) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(patch, &fields) == nil {
		if raw, present := fields["timezone"]; present {
			var zone string
			if json.Unmarshal(raw, &zone) != nil || workspace.ValidateTimezone(zone) != nil {
				return workspace.ErrTimezone
			}
		}
	}
	return nil
}

// Apply returns the owner's settings after a patch. replan is true when the
// patch changes the time zone or the time of the daily review: the start
// days of the tasks then need a new calculation.
// A time zone that is not valid returns workspace.ErrTimezone. The retired
// field autoAccept is ignored and is always false.
func Apply(current workspace.Settings, patch json.RawMessage) (next workspace.Settings, replan bool, err error) {
	if err := CheckZone(patch); err != nil {
		return current, false, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(patch, &fields) != nil || len(fields) == 0 {
		return current, false, memory.ErrInvalid
	}
	delete(fields, "autoAccept")
	next = current
	if len(fields) != 0 {
		rest, _ := json.Marshal(fields)
		if err := itemcommand.Patch(&next, rest, "wakeIdeas", "followUps", "dailyReviewAt", "dailyBudget", "timezone", "city"); err != nil {
			return current, false, err
		}
	}
	next.AutoAccept = false
	next.City = strings.TrimSpace(next.City)
	if utf8.RuneCountInString(next.City) > cityLimit || strings.ContainsAny(next.City, "\r\n") {
		return current, false, memory.ErrInvalid
	}
	if next.DailyBudget < 0 || next.DailyBudget > budgetLimit {
		return current, false, memory.ErrInvalid
	}
	if _, err := time.Parse("15:04", next.DailyReviewAt); err != nil {
		return current, false, memory.ErrInvalid
	}
	if err := workspace.ValidateTimezone(next.Timezone); err != nil {
		return current, false, err
	}
	_, zone := fields["timezone"]
	_, clock := fields["dailyReviewAt"]
	return next, zone || clock, nil
}

// ApplyAgent returns an agent after a patch. initialize is true when the
// agent is enabled for the first time: its memory selection is then made
// once, and the caller sets MemoryInitialized after it.
func ApplyAgent(current workspace.Agent, patch json.RawMessage) (next workspace.Agent, initialize bool, err error) {
	next = current
	if err := itemcommand.Patch(&next, patch, "enabled", "memoryKinds", "includeInferred", "note", "name"); err != nil {
		return current, false, err
	}
	if strings.TrimSpace(next.Name) == "" || len(next.Name) > 1<<20 {
		return current, false, memory.ErrInvalid
	}
	for _, kind := range next.MemoryKinds {
		switch kind {
		case "fact", "preference", "decision", "intention", "plan":
		default:
			return current, false, memory.ErrInvalid
		}
	}
	return next, next.Enabled && !next.MemoryInitialized, nil
}

// Defaults are the settings of a new owner: ideas wake, finished work is
// reported, the daily review is at nine, and the daily budget is ten.
func Defaults(timezone string) workspace.Settings {
	return workspace.Settings{WakeIdeas: true, FollowUps: true, DailyReviewAt: "09:00", DailyBudget: 10, Timezone: timezone}
}

// defaultKinds are the kinds of memory that a new agent reads.
var defaultKinds = []string{"fact", "preference", "decision", "intention", "plan"}

// Agents are the agents of a new owner: the manual handoff, and one agent
// for each configured text model with a note on its channel.
func Agents(models []memory.ModelInfo) []workspace.Agent {
	agents := []workspace.Agent{{ID: "manual", Name: "手动交接", Channel: "manual", Enabled: true, MemoryKinds: append([]string{}, defaultKinds...), Note: "复制上下文到任意 AI，再粘贴结果"}}
	for _, p := range models {
		if p.Embedding || p.Transcription {
			continue
		}
		note := "通用 API · " + p.Model
		switch p.Protocol {
		case "codex":
			note = "ChatGPT 订阅 · 在设置中登录"
		case "siwc":
			note = "消耗你的 ChatGPT 套餐 · 官方直接授权"
		}
		agents = append(agents, workspace.Agent{ID: p.ID, Name: p.Name, Channel: "api", Note: note, Enabled: true, MemoryKinds: append([]string{}, defaultKinds...)})
	}
	return agents
}
