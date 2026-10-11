// Package extraction owns the memory interpretation rules that read
// statements, tasks, and ideas from one source text. The rules here need no
// storage.
package extraction

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

// Long sources are read in windows that overlap, each by its own job.
const (
	// Window is the length of one window, in characters.
	Window = 12000
	// Step is the distance between the starts of two windows.
	Step = 11000
)

// WindowStarts returns the start of each window of a text of length
// characters. A text that fits one window has no windows.
func WindowStarts(length int) []int {
	starts := []int{}
	if length <= Window {
		return starts
	}
	for start := 0; start < length; start += Step {
		starts = append(starts, start)
		if start+Window >= length {
			break
		}
	}
	return starts
}

// WindowText returns the window of text that starts at start.
func WindowText(text []rune, start int) (string, bool) {
	if start < 0 || start >= len(text) {
		return "", false
	}
	return string(text[start:min(start+Window, len(text))]), true
}

// Item is one statement, task, or idea that the model reads from a source.
type Item struct {
	People        []string `json:"people,omitempty"`
	Places        []string `json:"places,omitempty"`
	Organizations []string `json:"organizations,omitempty"`
	When          *When    `json:"when,omitempty"`
	Qualification string   `json:"qualification,omitempty"`
	Kind          string   `json:"kind"`
	Text          string   `json:"text"`
	Nature        string   `json:"nature"`
	Subject       string   `json:"subject"`
	Predicate     string   `json:"predicate"`
	Quote         string   `json:"quote"`
	Confidence    float64  `json:"confidence"`
	Explicit      bool     `json:"explicit"`
	Acquisition   string   `json:"acquisition"`
}

// Signal says that a source text meets the wake condition of an idea.
type Signal struct {
	IdeaID      string  `json:"idea_id"`
	ConditionID string  `json:"condition_id"`
	Quote       string  `json:"quote"`
	Explanation string  `json:"explanation"`
	Confidence  float64 `json:"confidence"`
}

// Output is the model's answer for one source text.
type Output struct {
	Signals []Signal `json:"signals"`
	Items   []Item   `json:"items"`
}

// ParseOutput reads the model's answer. A code fence around the answer is
// removed. An answer with an unknown field or trailing text is not valid.
func ParseOutput(text string) (Output, bool) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	var out Output
	if modelcall.DecodeStrict([]byte(strings.TrimSpace(text)), &out) != nil {
		return Output{}, false
	}
	return out, true
}

// Normalize applies the rules of the source's speaker to one item. An unknown
// acquisition is an inference. What an assistant, a system, a tool, or a
// historical branch said is never an explicit statement of the owner.
func Normalize(item Item, context *memory.SourceContext) Item {
	if !oneOf(item.Acquisition, "direct", "reported", "inferred") {
		item.Acquisition = "inferred"
	}
	if context != nil && (context.Role == "assistant" || context.Role == "system" || context.Role == "tool" || context.Branch == "historical") {
		item.Explicit = false
		item.Acquisition = "inferred"
		if context.Role != "user" {
			item.Subject = "AI 或工具（非用户）"
			item.Text = "AI 或工具当时的表达：" + item.Text
		}
	}
	return item
}

// Grounded reports whether an item can be used: it has text, a known kind, a
// confidence between zero and one, and a quotation that the source contains.
func Grounded(item Item, source string) bool {
	return strings.TrimSpace(item.Text) != "" && len(item.Text) <= 1<<20 && item.Quote != "" && strings.Contains(source, item.Quote) && oneOf(item.Kind, "memory", "task", "idea", "unknown") && item.Confidence >= 0 && item.Confidence <= 1
}

// Statement reports whether the item is a statement that becomes a memory.
func Statement(item Item) bool {
	return item.Kind == "memory" && oneOf(item.Nature, "fact", "preference", "decision", "intention", "plan")
}

// SelfSubject reports whether the subject names the owner.
func SelfSubject(subject string) bool {
	return oneOf(strings.TrimSpace(subject), "我", "我们", "用户", "本人", "用户本人")
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}

// Direct user expressions are source-backed, not user-confirmed. Imports,
// quotations, tentative statements and model inferences still require review.
// Confirmation is "adopted" for a direct, confident statement of the
// owner's current input, and "candidate" for each other statement.
func Confirmation(source memory.SourceResult, item Item) string {
	if item.Qualification != "" && item.Qualification != "asserted" || QualifiedCapture(qualificationContext(source.Source.Text, item.Quote)) {
		return "candidate"
	}
	currentInput := oneOf(source.Source.Connector, "capture", "desk", "telegram", "desk-incomplete") &&
		(source.Context == nil || source.Context.Role == "user" && source.Context.Branch != "historical")
	if currentInput && item.Acquisition == "direct" && item.Confidence >= 0.8 {
		return "adopted"
	}
	return "candidate"
}

func qualificationContext(source, quote string) string {
	at := strings.Index(source, quote)
	if at < 0 || quote == "" {
		return quote
	}
	// Inspect the surrounding statement so extraction cannot drop its leading
	// qualifier, without making an unrelated tentative sentence on the same
	// line add a confirmation burden to a direct assertion.
	const boundaries = "\n.!?。！？;；"
	start := strings.LastIndexAny(source[:at], boundaries)
	if start < 0 {
		start = 0
	} else {
		// The boundary can be multibyte; only the following statement matters.
		_, size := utf8.DecodeRuneInString(source[start:])
		start += size
	}
	end := at + len(quote)
	last, _ := utf8.DecodeLastRuneInString(strings.TrimSpace(quote))
	if strings.ContainsRune(boundaries, last) {
		return source[start:end]
	}
	if boundary := strings.IndexAny(source[end:], boundaries); boundary >= 0 {
		end += boundary
	} else {
		end = len(source)
	}
	return source[start:end]
}

// QualificationMarkers are the substrings that mark a qualified statement.
var QualificationMarkers = []string{"可能", "也许", "大概", "不确定", "考虑", "假如", "假设", "如果", "据说", "听说", "他说", "她说", "更正", "纠正", "不再", "原以为", "暂时", "试试", "“", "”", "\""}

// QualificationWords are the whole words that mark a qualified statement.
var QualificationWords = []string{"maybe", "perhaps", "probably", "possibly", "likely", "might", "could", "not sure", "i think", "considering", "if", "said", "correction", "actually", "no longer"}

// QualifiedCapture reports whether the text carries a marker of doubt,
// report, correction, or condition.
func QualifiedCapture(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range QualificationMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	words := " " + strings.Join(strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), " ") + " "
	for _, marker := range QualificationWords {
		if strings.Contains(words, " "+marker+" ") {
			return true
		}
	}
	return false
}

// ExpressedAt is the time at which the source was said. A source of the
// current input without its own time uses the time of its record.
func ExpressedAt(source memory.SourceResult) *time.Time {
	if source.Source.ExpressedAt != nil {
		return source.Source.ExpressedAt
	}
	if oneOf(source.Source.Connector, "desk", "capture", "telegram", "desk-incomplete") {
		return &source.Source.RecordedAt
	}
	return nil
}

// When is the event time that the model reads from a statement.
type When struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Precision string `json:"precision"`
	Quote     string `json:"quote"`
}

// Event returns the half-open interval of a valid event time and its
// precision. A relative expression needs the time at which it was said.
func Event(when *When, source memory.SourceResult, loc *time.Location) (*time.Time, *time.Time, string) {
	if when == nil || when.Quote == "" || !strings.Contains(source.Source.Text, when.Quote) ||
		!oneOf(when.Precision, "day", "month", "year", "range") {
		return nil, nil, "unknown"
	}
	if ExpressedAt(source) == nil && relativeEventQuote(when.Quote) {
		return nil, nil, "unknown"
	}
	from, err := time.ParseInLocation("2006-01-02", when.From, loc)
	if err != nil {
		return nil, nil, "unknown"
	}
	to, err := time.ParseInLocation("2006-01-02", when.To, loc)
	if err != nil || to.Before(from) || to.After(from.AddDate(10, 0, 0)) || when.Precision == "range" && !to.After(from) {
		return nil, nil, "unknown"
	}
	switch when.Precision {
	case "day":
		to = from.AddDate(0, 0, 1)
	case "month":
		from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 1, 0)
	case "year":
		from = time.Date(from.Year(), 1, 1, 0, 0, 0, 0, loc)
		to = from.AddDate(1, 0, 0)
	}
	return &from, &to, when.Precision
}

func relativeEventQuote(quote string) bool {
	for _, word := range []string{"今天", "明天", "昨天", "后天", "前天", "今晚", "明晚", "下周", "上周", "本周", "这周", "下星期", "上星期", "这星期", "本星期", "下个月", "上个月", "这个月", "本月", "去年", "今年", "明年", "前年", "后年", "下半年", "上半年", "这半年", "本季度", "下季度", "上季度", "next", "last", "this", "today", "tomorrow", "yesterday", "ago"} {
		if strings.Contains(strings.ToLower(quote), word) {
			return true
		}
	}
	return relativeEventPattern.MatchString(quote)
}

var relativeEventPattern = regexp.MustCompile(`[0-9一二两三四五六七八九十几]+(个)?[天日周月年](前|后)|[周星期礼拜]+[一二三四五六日天]`)
