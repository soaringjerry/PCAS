package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
	"github.com/soaringjerry/PCAS/internal/ai"
)

// This mode is for the coordinator AFTER user consent. It never connects to a
// database. Candidate rubrics cannot be evaluated until the review script marks
// individual tasks approved.
func runDoingPropose(args []string) error {
	f := flag.NewFlagSet("doing-propose", flag.ContinueOnError)
	input := f.String("input", "", "private exported memory snapshot outside any repository")
	output := f.String("output", "/var/tmp/pcas-v2-private/proposals.json", "private candidate suite")
	home := f.String("codex-home", os.Getenv("PCAS_EVAL_CODEX_HOME"), "existing dedicated signed-in home")
	model := f.String("model", "gpt-6.1-sol", "candidate author model, requires human review")
	per := f.Int("per-category", 4, "candidate tasks per category")
	budget := f.Int("max-memory-chars", 100000, "explicit bounded recent-memory proposal sample")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *input == "" || *home == "" || *per < 1 || *per > 10 || *budget < 1000 {
		return fmt.Errorf("invalid propose arguments")
	}
	if err := outsideRepository(*input); err != nil {
		return err
	}
	if err := outsideRepository(*output); err != nil {
		return err
	}
	s, err := doing.Load(*input)
	if err != nil {
		return err
	}
	if s.Synthetic || len(s.Tasks) != 0 {
		return fmt.Errorf("expected private memory-only export")
	}
	selected := []doing.Memory{}
	count := 0
	// Export sorts by expression time. This is an explicit sample, NOT exhaustive
	// coverage; a memory is never chopped in half and selection is recorded.
	for i := len(s.Memories) - 1; i >= 0; i-- {
		n := utf8.RuneCountInString(s.Memories[i].Text)
		if count+n > *budget {
			continue
		}
		selected = append(selected, s.Memories[i])
		count += n
	}
	if len(selected) == 0 {
		return fmt.Errorf("no memory fits proposal budget")
	}
	c, err := ai.NewCodex("", *home)
	if err != nil {
		return err
	}
	defer c.Close()
	registry := &ai.Registry{Codex: c, HTTP: &http.Client{Timeout: 3 * time.Minute}, Config: ai.Configuration{Providers: []ai.Provider{{ID: "v2-answer", Name: "本机候选题", Protocol: "codex", Model: *model, MaxOutput: 4096, CostMode: "free"}}}}
	author := evalModel{registry}
	system := `只根据提供的记忆为六类办事评测写待人核对的候选题。不得使用工具、读文件、联网或发送。不要出缺少输入材料的题。标准是候选，不是最终答案。每题必须3–6条原子条件；加分可空；不许至少1条。每条记忆依据用提供的id，只有irrelevant类的请求内约束可用evidence=[]。不能猜未记录的事实；做不完实际动作的题写可先拟草稿和缺什么。返回JSON对象{"tasks":[...]}，任务字段为id,category,request,must,bonus,forbidden,can_complete_without_followup,reasonable_handling。检查字段为id,text,evidence。不要设置reviewed字段。类别必须与请求指定的category一致，任务id和检查id不重复，不能跨题复用task id。`
	for _, category := range doing.Categories {
		raw, err := author.Generate(context.Background(), system, stringJSON(map[string]any{"category": category, "count": *per, "as_of": s.AsOf, "memories": selected}))
		if err != nil {
			return fmt.Errorf("candidate model call failed category=%s", category)
		}
		var response struct {
			Tasks []doing.Task `json:"tasks"`
		}
		if json.Unmarshal([]byte(raw), &response) != nil || len(response.Tasks) != *per {
			return fmt.Errorf("invalid candidates category=%s", category)
		}
		for i, t := range response.Tasks {
			if t.Category != category {
				return fmt.Errorf("wrong candidate category")
			}
			t.ID = fmt.Sprintf("PRIVATE-%s-%02d", category, i+1)
			for groupIndex, group := range [][]doing.Check{t.Must, t.Bonus, t.Forbidden} {
				for checkIndex := range group {
					group[checkIndex].ID = fmt.Sprintf("%s%d", []string{"M", "B", "F"}[groupIndex], checkIndex+1)
				}
			}
			t.Reviewed = false
			if t.Bonus == nil {
				t.Bonus = []doing.Check{}
			}
			s.Tasks = append(s.Tasks, t)
		}
	}
	// Validate reference/shape integrity on a copy; approvals stay FALSE on disk.
	candidate := s
	candidate.Tasks = append([]doing.Task{}, s.Tasks...)
	for i := range candidate.Tasks {
		candidate.Tasks[i].Reviewed = true
	}
	if err = candidate.Validate(); err != nil {
		return fmt.Errorf("candidate integrity check failed; no proposal saved")
	}
	if err = os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		return err
	}
	if err = doing.WriteJSON(*output, s); err != nil {
		return err
	}
	fmt.Printf("candidate tasks=%d selected_memories=%d total_memories=%d memory_chars=%d; human review required\n", len(s.Tasks), len(selected), len(s.Memories), count)
	return nil
}
