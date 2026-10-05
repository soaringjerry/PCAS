package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

var statusISODate = regexp.MustCompile(`\b([0-9]{4})-([0-9]{1,2})-([0-9]{1,2})\b`)
var statusCNDate = regexp.MustCompile(`(?:([0-9]{4})年)?([0-9]{1,2})月([0-9]{1,2})[日号]?`)
var statusWeekday = regexp.MustCompile(`(上|下|本|这)?(?:周|星期|礼拜)([一二三四五六日天])`)
var statusDayOffset = regexp.MustCompile(`([0-9一二两三四五六七八九十]+)天后`)
var statusClock = regexp.MustCompile(`(凌晨|早上|上午|中午|下午|傍晚|晚上|夜里)?\s*([0-9]{1,2}|[一二两三四五六七八九十]+)\s*(?:点|时|:)\s*(半|一刻|三刻|[0-9]{1,2}(?:分)?|[一二两三四五六七八九十]+分)?`)
var statusCycle = regexp.MustCompile(`每(?:周|星期|礼拜|月|天|日|年|[0-9一二两三四五六七八九十]+天)`)

func statusNumber(raw string) (int, bool) {
	if n, err := strconv.Atoi(raw); err == nil {
		return n, true
	}
	units := map[rune]int{'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	runes := []rune(raw)
	if len(runes) == 1 {
		if runes[0] == '十' {
			return 10, true
		}
		n, ok := units[runes[0]]
		return n, ok
	}
	if len(runes) > 3 {
		return 0, false
	}
	tens := strings.Split(raw, "十")
	if len(tens) != 2 {
		return 0, false
	}
	a, b := 1, 0
	if tens[0] != "" {
		var ok bool
		a, ok = units[[]rune(tens[0])[0]]
		if !ok || len([]rune(tens[0])) != 1 {
			return 0, false
		}
	}
	if tens[1] != "" {
		var ok bool
		b, ok = units[[]rune(tens[1])[0]]
		if !ok || len([]rune(tens[1])) != 1 {
			return 0, false
		}
	}
	return a*10 + b, true
}

// Resolve only dates supported by the actual claim and its expression date.
// Unsupported language is conservatively omitted instead of trusting the model.
func statusDate(text, expressed string, loc *time.Location) (time.Time, bool) {
	said, err := time.Parse(time.RFC3339Nano, expressed)
	hasSaid := err == nil
	if hasSaid {
		said = said.In(loc)
	}
	makeDate := func(year, month, day int) (time.Time, bool) {
		d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
		return d, d.Year() == year && int(d.Month()) == month && d.Day() == day
	}
	if p := statusISODate.FindStringSubmatch(text); p != nil {
		y, _ := strconv.Atoi(p[1])
		m, _ := strconv.Atoi(p[2])
		d, _ := strconv.Atoi(p[3])
		return makeDate(y, m, d)
	}
	if p := statusCNDate.FindStringSubmatch(text); p != nil {
		year := said.Year()
		if p[1] != "" {
			year, _ = strconv.Atoi(p[1])
		} else if !hasSaid {
			return time.Time{}, false
		}
		m, _ := strconv.Atoi(p[2])
		d, _ := strconv.Atoi(p[3])
		return makeDate(year, m, d)
	}
	if !hasSaid {
		return time.Time{}, false
	}
	today := time.Date(said.Year(), said.Month(), said.Day(), 0, 0, 0, 0, loc)
	for _, p := range []struct {
		word string
		days int
	}{{"大后天", 3}, {"后天", 2}, {"明天", 1}, {"明晚", 1}, {"今天", 0}, {"今晚", 0}, {"昨天", -1}, {"前天", -2}} {
		if strings.Contains(text, p.word) {
			return today.AddDate(0, 0, p.days), true
		}
	}
	if p := statusDayOffset.FindStringSubmatch(text); p != nil {
		n, ok := statusNumber(p[1])
		if ok && n <= 366 {
			return today.AddDate(0, 0, n), true
		}
	}
	if p := statusWeekday.FindStringSubmatch(text); p != nil {
		weekday := strings.Index("一二三四五六日", p[2]) / 3
		if p[2] == "天" {
			weekday = 6
		}
		monday := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
		d := monday.AddDate(0, 0, weekday)
		switch p[1] {
		case "下":
			d = d.AddDate(0, 0, 7)
		case "上":
			d = d.AddDate(0, 0, -7)
		case "":
			if d.Before(today) {
				d = d.AddDate(0, 0, 7)
			}
		}
		return d, true
	}
	return time.Time{}, false
}

func statusTime(text string) (hour, minute int, note string, ok bool) {
	p := statusClock.FindStringSubmatch(text)
	if p == nil {
		return 0, 0, "未说明具体时间", true
	}
	hour, ok = statusNumber(p[2])
	if !ok || hour > 23 {
		return 0, 0, "", false
	}
	switch p[3] {
	case "":
	case "半":
		minute = 30
	case "一刻":
		minute = 15
	case "三刻":
		minute = 45
	default:
		minute, ok = statusNumber(strings.TrimSuffix(p[3], "分"))
		if !ok || minute > 59 {
			return 0, 0, "", false
		}
	}
	if p[1] == "" && hour >= 1 && hour <= 12 && !strings.Contains(p[0], ":") {
		return 0, 0, "未说明上午还是下午", true
	}
	if oneOf(p[1], "下午", "傍晚", "晚上", "夜里") && hour < 12 {
		hour += 12
	}
	if p[1] == "凌晨" && hour == 12 {
		hour = 0
	}
	if p[1] == "中午" && hour < 11 {
		hour += 12
	}
	return hour, minute, "", true
}

func validateCardDeadline(d cardDeadline, m cardMemory, loc *time.Location, now time.Time) (cardDeadline, bool) {
	if !oneOf(d.Kind, "deadline", "appointment", "recurring") || strings.TrimSpace(d.Title) == "" || utf8.RuneCountInString(d.Title) > 60 {
		return d, false
	}
	if d.Kind == "recurring" {
		d.Recurrence = strings.TrimSpace(d.Recurrence)
		if d.At != nil || !statusCycle.MatchString(m.Text) || !statusCycle.MatchString(d.Recurrence) || !strings.Contains(m.Text, d.Recurrence) {
			return d, false
		}
		_, _, note, ok := statusTime(m.Text)
		if !ok {
			return d, false
		}
		// A broad period such as evening is explicit in recurring schedules.
		if note == "未说明具体时间" {
			for _, period := range []string{"凌晨", "早上", "上午", "中午", "下午", "傍晚", "晚上", "夜里"} {
				if strings.Contains(m.Text, period) {
					note = ""
					break
				}
			}
		}
		d.TimeNote = note
		return d, true
	}
	day, ok := statusDate(m.Text, m.ExpressedAt, loc)
	if !ok || d.At == nil {
		return d, false
	}
	hour, minute, note, ok := statusTime(m.Text)
	if !ok {
		return d, false
	}
	expected := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
	// Reject DST gaps and invented instants, rather than silently normalizing them.
	if expected.Hour() != hour || expected.Minute() != minute {
		return d, false
	}
	at, err := time.Parse(time.RFC3339Nano, *d.At)
	if err != nil || !at.Equal(expected) || at.Before(now) {
		return d, false
	}
	v := expected.UTC().Format(time.RFC3339Nano)
	d.At = &v
	d.Recurrence = ""
	d.TimeNote = note
	return d, true
}

func statusDeadlineID(owner memory.ID, m cardMemory, d cardDeadline) string {
	at := ""
	if d.At != nil {
		at = *d.At
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%s/%s/%s", owner, m.Ref.ID, m.Ref.Version, d.Kind, at, d.Recurrence)))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
func writeCardDeadlinesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, memories []cardMemory, output []cardDeadline, timezone string, now time.Time) error {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, m := range memories {
		ids = append(ids, string(m.Ref.ID))
	}
	if _, err := tx.Exec(ctx, "DELETE FROM deadlines WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])", string(owner), ids); err != nil {
		return err
	}
	for _, d := range output {
		if d.N < 1 || d.N > len(memories) {
			continue
		}
		m := memories[d.N-1]
		d, ok := validateCardDeadline(d, m, loc, now)
		if !ok {
			continue
		}
		if utf8.RuneCountInString(d.TimeNote) > 120 {
			d.TimeNote = string([]rune(d.TimeNote)[:120])
		}
		if _, err := tx.Exec(ctx, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title,time_note)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(owner_id,id) DO NOTHING`, string(owner), statusDeadlineID(owner, m, d), string(m.Ref.ID), m.Ref.Version, d.Kind, d.At, d.Recurrence, d.Title, d.TimeNote); err != nil {
			return err
		}
	}
	return nil
}
