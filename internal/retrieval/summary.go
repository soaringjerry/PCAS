package retrieval

import (
	"fmt"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// SummaryEntries is the most records that one summary covers.
const SummaryEntries = 500

// SummaryEntry is one record that a summary can hold.
type SummaryEntry struct {
	Ref                                      memory.Ref
	Text, Nature, Confirmation, Role, Branch string
	Gaps                                     []string
	From, To                                 any
}

// Summary writes the summary of a record within its budget: about two
// characters a token. Each line names the record and what it is; an
// original keeps its words. What does not fit, what is still pending, and
// what the record set leaves out are said as gaps. more is true when there
// were more records than a summary covers.
func Summary(entries []SummaryEntry, more, pending bool, root memory.ID, tokens int) (string, []memory.Ref, memory.Coverage) {
	coverage := NewCoverage()
	deps := []memory.Ref{}
	if more {
		coverage.Gaps = append(coverage.Gaps, "相关材料超过摘要覆盖范围，请展开经历或完整历史继续查询")
	}
	if pending {
		coverage.Gaps = append(coverage.Gaps, "来源仍有待处理或失败步骤；摘要只使用目前可读内容")
		coverage.PendingSources = append(coverage.PendingSources, root)
	}
	var text strings.Builder
	remaining := tokens * 2
	for _, e := range entries {
		deps = append(deps, e.Ref)
		coverage.Gaps = append(coverage.Gaps, e.Gaps...)
		label := string(e.Ref.Kind)
		if e.Ref.Kind == memory.SourceKind {
			label = "原文摘录，保留当时表达"
		}
		if e.Nature != "" {
			label = e.Nature + " / " + e.Confirmation
		}
		if e.Role != "" {
			label += " / " + e.Role
		}
		if e.Branch != "" {
			label += " / " + e.Branch
		}
		line := fmt.Sprintf("[%s v%d] %s：%s\n", e.Ref.ID, e.Ref.Version, label, e.Text)
		runes := []rune(line)
		if len(runes) > remaining {
			coverage.Gaps = append(coverage.Gaps, "摘要受上下文预算限制，展开来源可查看全文")
			continue
		}
		text.WriteString(line)
		remaining -= len(runes)
	}
	coverage.Complete = len(coverage.Gaps) == 0
	return text.String(), deps, coverage
}
