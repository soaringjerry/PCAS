package memory

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// QueryPlan contains the deterministic hints extracted from one user message.
type QueryPlan struct {
	Time    *PlanTime
	Natures []string
	Recall  bool
}

// PlanTime is a half-open interval in the user's calendar. Axis is said when
// the phrase immediately precedes a speech verb, and either otherwise.
type PlanTime struct {
	From, To  time.Time
	Precision string
	Axis      string
	Phrase    string
}

// Longer date forms precede their prefixes so a day wins over its month/year.
var queryTimePattern = regexp.MustCompile(`[0-9]+\s*年(?:\s*[0-9]+\s*月(?:\s*[0-9]+\s*日)?)?|[0-9]+\s*月(?:\s*[0-9]+\s*日)?|今天|昨天|前天|明天|后天|(?:这|上|下)周[一二三四五六日天]?|这个月|上个月|下个月|今年|去年|前年|明年|最近|这几天`)
var queryDatePattern = regexp.MustCompile(`^(?:([0-9]{4})\s*年\s*)?(?:([0-9]{1,2})\s*月\s*)?(?:([0-9]{1,2})\s*日)?$`)

// PlanQuery uses only text, now and loc; a nil location deterministically means
// UTC. It neither consults the machine's clock nor its local timezone.
func PlanQuery(text string, now time.Time, loc *time.Location) QueryPlan {
	if loc == nil {
		loc = time.UTC
	}
	localNow := now.In(loc)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	plan := QueryPlan{}
	if queryContainsAny(text, "想", "打算", "计划", "准备", "要去", "要做", "要干") {
		plan.Natures = append(plan.Natures, "intention", "plan")
	}
	if queryContainsAny(text, "喜欢", "爱吃", "不吃", "讨厌", "习惯") {
		plan.Natures = append(plan.Natures, "preference")
	}
	if queryContainsAny(text, "决定", "定了") {
		plan.Natures = append(plan.Natures, "decision")
	}
	plan.Recall = queryContainsAny(text, "说过", "提过", "来着", "之前", "上次", "记得", "当时")

	for offset := 0; offset < len(text); {
		match := queryTimePattern.FindStringIndex(text[offset:])
		if match == nil {
			break
		}
		start, end := offset+match[0], offset+match[1]
		offset = end
		phrase := text[start:end]
		interval := queryTimeInterval(phrase, today)
		if interval == nil {
			continue
		}
		interval.Phrase = phrase
		interval.Axis = "either"
		after := strings.TrimLeftFunc(text[end:], unicode.IsSpace)
		for _, verb := range []string{"说", "提", "聊", "讲", "问", "告诉", "记"} {
			if strings.HasPrefix(after, verb) {
				interval.Axis = "said"
				break
			}
		}
		plan.Time = interval
		break
	}
	return plan
}

func queryContainsAny(text string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func queryTimeInterval(phrase string, today time.Time) *PlanTime {
	interval := func(from time.Time, years, months, days int, precision string) *PlanTime {
		return &PlanTime{From: from, To: from.AddDate(years, months, days), Precision: precision}
	}
	switch phrase {
	case "今天", "昨天", "前天", "明天", "后天":
		days := map[string]int{"今天": 0, "昨天": -1, "前天": -2, "明天": 1, "后天": 2}[phrase]
		return interval(today.AddDate(0, 0, days), 0, 0, 1, "day")
	case "这个月", "上个月", "下个月":
		months := map[string]int{"这个月": 0, "上个月": -1, "下个月": 1}[phrase]
		from := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location()).AddDate(0, months, 0)
		return interval(from, 0, 1, 0, "month")
	case "今年", "去年", "前年", "明年":
		years := map[string]int{"今年": 0, "去年": -1, "前年": -2, "明年": 1}[phrase]
		from := time.Date(today.Year()+years, time.January, 1, 0, 0, 0, 0, today.Location())
		return interval(from, 1, 0, 0, "year")
	case "最近", "这几天":
		days := 30
		if phrase == "这几天" {
			days = 7
		}
		return interval(today.AddDate(0, 0, 1-days), 0, 0, days, "range")
	}
	if strings.Contains(phrase, "周") {
		days := (int(today.Weekday()) + 6) % 7
		from := today.AddDate(0, 0, -days)
		if strings.HasPrefix(phrase, "上") {
			from = from.AddDate(0, 0, -7)
		} else if strings.HasPrefix(phrase, "下") {
			from = from.AddDate(0, 0, 7)
		}
		runes := []rune(phrase)
		if len(runes) == 3 {
			weekday := map[rune]int{'一': 0, '二': 1, '三': 2, '四': 3, '五': 4, '六': 5, '日': 6, '天': 6}[runes[2]]
			return interval(from.AddDate(0, 0, weekday), 0, 0, 1, "day")
		}
		return interval(from, 0, 0, 7, "week")
	}
	parts := queryDatePattern.FindStringSubmatch(phrase)
	if parts == nil {
		return nil
	}
	year, month, day := today.Year(), 1, 1
	precision := "year"
	if parts[1] != "" {
		year, _ = strconv.Atoi(parts[1])
		if year == 0 {
			return nil
		}
	}
	if parts[2] != "" {
		month, _ = strconv.Atoi(parts[2])
		precision = "month"
	}
	if parts[3] != "" {
		day, _ = strconv.Atoi(parts[3])
		precision = "day"
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return nil
	}
	// Reject impossible dates before searching for a yearless leap day.
	check := time.Date(2000, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if int(check.Month()) != month || check.Day() != day {
		return nil
	}
	// A valid month/day occurs within the previous eight Gregorian years,
	// including the non-leap century boundary (for example, 2100).
	for attempts := 0; attempts <= 8; attempts++ {
		from := time.Date(year, time.Month(month), day, 0, 0, 0, 0, today.Location())
		valid := int(from.Month()) == month && from.Day() == day
		if valid && (parts[1] != "" || !from.After(today)) {
			switch precision {
			case "year":
				return interval(from, 1, 0, 0, precision)
			case "month":
				return interval(from, 0, 1, 0, precision)
			default:
				return interval(from, 0, 0, 1, precision)
			}
		}
		if parts[1] != "" {
			return nil
		}
		year--
	}
	return nil
}
