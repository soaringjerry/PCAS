package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/worker"
	"strings"
	"time"
)

type phase25B3Deadline struct {
	claim                             memory.ID
	kind, title, recurrence, timeNote string
	at                                *time.Time
}
type phase25B3CardOutput struct {
	fields        map[string][]memory.ID
	deadlines     []phase25B3Deadline
	applicability map[memory.ID]string
}
type phase25B3Adapter struct {
	encodeCard  func(phase25B3CardOutput, map[memory.ID]int) (string, error)
	schedule    func(context.Context, time.Time) (int, error)
	process     func(context.Context, worker.Job) error
	setVersions func(card, handover int) func()
}

func phase25B3DefaultAdapter(f *phase25B234Fixture) phase25B3Adapter {
	return phase25B3Adapter{
		schedule: f.store.ScheduleStatus,
		process: func(ctx context.Context, j worker.Job) error {
			if strings.HasPrefix(j.Stage, "memory.card:") {
				return f.store.ProcessCard(ctx, j)
			}
			if strings.HasPrefix(j.Stage, "memory.handover:") {
				return f.store.ProcessHandover(ctx, j)
			}
			return fmt.Errorf("unexpected status stage %s", j.Stage)
		},
		setVersions: func(c, h int) func() {
			oldC, oldH := postgres.CardVersion, postgres.HandoverVersion
			postgres.CardVersion, postgres.HandoverVersion = c, h
			return func() { postgres.CardVersion, postgres.HandoverVersion = oldC, oldH }
		},
		encodeCard: func(result phase25B3CardOutput, numbers map[memory.ID]int) (string, error) {
			fields := map[string][]int{}
			for field, ids := range result.fields {
				for _, id := range ids {
					n, ok := numbers[id]
					if !ok {
						return "", fmt.Errorf("memory %s absent from captured batch", id)
					}
					fields[field] = append(fields[field], n)
				}
			}
			var rules []map[string]any
			for id, applies := range result.applicability {
				n, ok := numbers[id]
				if !ok {
					return "", fmt.Errorf("rule %s absent", id)
				}
				rules = append(rules, map[string]any{"n": n, "appliesTo": applies})
			}
			var deadlines []map[string]any
			for _, d := range result.deadlines {
				n, ok := numbers[d.claim]
				if !ok {
					return "", fmt.Errorf("deadline memory %s absent", d.claim)
				}
				deadlines = append(deadlines, map[string]any{"n": n, "kind": d.kind, "at": d.at, "recurrence": d.recurrence, "title": d.title, "timeNote": d.timeNote})
			}
			data, err := json.Marshal(map[string]any{"fields": fields, "rules": rules, "deadlines": deadlines})
			return string(data), err
		},
	}
}

func phase25B3ModelOutput(a phase25B3Adapter, result phase25B3CardOutput, numbers map[memory.ID]int) (string, error) {
	if a.encodeCard == nil {
		return "", phase25B234AwaitingProtocol
	}
	return a.encodeCard(result, numbers)
}

type phase25B3InputMemory struct {
	N           int    `json:"n"`
	Text        string `json:"text"`
	ExpressedAt string `json:"expressedAt"`
	Category    string `json:"category"`
	Trust       string `json:"trust"`
	Durable     *bool  `json:"durable"`
}
type phase25B3Input struct {
	Key      string                 `json:"key"`
	Memories []phase25B3InputMemory `json:"memories"`
	Timezone string                 `json:"timezone"`
	Now      string                 `json:"now"`
}

// This input decoder is the single adaptation point for captured prompt data.
// Fields were observed on the local fake endpoint, not in backend source.
func phase25B3Prompt(request phase25B234ModelRequest) (string, error) {
	for _, m := range request.Messages {
		if m.Role == "user" {
			var text string
			if err := json.Unmarshal(m.Content, &text); err != nil {
				return "", err
			}
			return text, nil
		}
	}
	return "", fmt.Errorf("no user prompt")
}
func phase25B3CardInput(request phase25B234ModelRequest) (phase25B3Input, error) {
	text, err := phase25B3Prompt(request)
	if err != nil {
		return phase25B3Input{}, err
	}
	var input phase25B3Input
	err = json.Unmarshal([]byte(text), &input)
	if err == nil && (input.Key == "" || len(input.Memories) == 0) {
		err = fmt.Errorf("missing card key or numbered memories")
	}
	return input, err
}

var phase25B3Titles = []string{"他是谁和现在的处境", "怎么跟他配合", "现在手上的事", "时间和节奏", "资源和限制", "口味和标准", "重要的人", "他的叫法", "他看重什么"}

func phase25B3HandoverJSON(bodies map[string]string, refs map[string][]int) (string, error) {
	var sections []map[string]any
	for _, title := range phase25B3Titles {
		body := bodies[title]
		if body == "" {
			body = "（暂无依据）"
		}
		r := refs[title]
		if r == nil {
			r = []int{}
		}
		sections = append(sections, map[string]any{"title": title, "body": body, "refs": r})
	}
	data, err := json.Marshal(map[string]any{"sections": sections})
	return string(data), err
}
