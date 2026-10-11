// Package deputy owns the workspace rules of a deputy run that need no
// storage: the checklist of an output, how an output is adopted, and the
// notice of a finished run.
package deputy

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Match the frontend parseChecklist, including ECMAScript whitespace and line terminators.
const checklistSpace = "\\t\\n\\v\\f\\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

var checklistLine = regexp.MustCompile("^[" + checklistSpace + "]*- \\[[ x]\\][" + checklistSpace + "]+([^\r\n\u2028\u2029]+)$")

// TrimChecklist removes the white space around one checklist entry,
// with the wide and the invisible spaces.
func TrimChecklist(text string) string {
	return strings.TrimFunc(text, func(r rune) bool {
		return strings.ContainsRune("\t\n\v\f\r \u00a0\u1680\u2028\u2029\u202f\u205f\u3000\ufeff", r) || r >= '\u2000' && r <= '\u200a'
	})
}

// Checklist returns the entries of the Markdown checklist in a run's output.
func Checklist(output string) []string {
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		if match := checklistLine.FindStringSubmatch(line); match != nil {
			if text := TrimChecklist(match[1]); text != "" {
				lines = append(lines, text)
			}
		}
	}
	return lines
}

// AdoptionFor is how the output of a run is adopted: a revision and plain
// text become a document, a checklist becomes subtasks, and a summary
// becomes a progress note.
func AdoptionFor(run workspace.Run, output string) string {
	if run.Kind == "revise" {
		return "doc"
	}
	if len(Checklist(output)) > 0 {
		return "subtasks"
	}
	if run.Kind == "summary" {
		return "progress"
	}
	return "doc"
}

// Plain lists remain supported by the manual command. Markdown checklists use
// only their parsed entries, preserving their text and excluding surrounding prose.
// AdoptionLines returns the lines that become subtasks.
func AdoptionLines(text string) []string {
	if lines := Checklist(text); len(lines) > 0 {
		return lines
	}
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimLeft(strings.TrimSpace(line), "-*•0123456789.)、 \t")
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// NoticeReason is what the owner reads when a run ends. A long output is
// cut at 600 characters.
func NoticeReason(run workspace.Run) string {
	if run.Status != "done" {
		return "副手没做成：" + run.Error
	}
	text := strings.TrimSpace(run.Output)
	if utf8.RuneCountInString(text) > 600 {
		text = string([]rune(text)[:600]) + "…"
	}
	if text == "" {
		return "副手做完了。"
	}
	return "副手做完了：\n" + text
}

// OutputFormat is the format that a run of this kind must answer in. It is
// the last part of the model input. Other kinds have no format.
func OutputFormat(kind string) string {
	switch kind {
	case "revise":
		return "\n输出格式：只输出 JSON：{\"body\":\"整份新正文\",\"complete\":true}。complete 必须如实表明全文是否完整、未截断；没有写完填 false。不要在 body 之外补说明，不输出省略占位文字。正文不得和基准完全相同。"
	case "breakdown":
		// Automatic adoption turns "- [ ]" lines into subtasks. Real models
		// otherwise answer with a numbered list, which is filed as a document.
		return "\n输出格式：每个步骤单独一行，写成「- [ ] 步骤」；不要编号，不要加粗，步骤之外不写别的内容。"
	}
	return ""
}
