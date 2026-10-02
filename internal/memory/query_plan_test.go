package memory

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func queryTestLocation(t *testing.T, zone string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func queryTestDate(t *testing.T, value string, loc *time.Location) time.Time {
	t.Helper()
	date, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func TestPlanQueryTimeExpressions(t *testing.T) {
	loc := queryTestLocation(t, "Australia/Sydney")
	now := queryTestDate(t, "2026-10-02 13:15", loc)
	cases := []struct {
		phrase, from, to, precision string
	}{
		{"今天", "2026-10-02", "2026-10-03", "day"},
		{"昨天", "2026-10-01", "2026-10-02", "day"},
		{"前天", "2026-09-30", "2026-10-01", "day"},
		{"明天", "2026-10-03", "2026-10-04", "day"},
		{"后天", "2026-10-04", "2026-10-05", "day"},
		{"这周", "2026-09-28", "2026-10-05", "week"},
		{"上周", "2026-09-21", "2026-09-28", "week"},
		{"下周", "2026-10-05", "2026-10-12", "week"},
		{"上周五", "2026-09-25", "2026-09-26", "day"},
		{"这周一", "2026-09-28", "2026-09-29", "day"},
		{"这周二", "2026-09-29", "2026-09-30", "day"},
		{"这周三", "2026-09-30", "2026-10-01", "day"},
		{"这周四", "2026-10-01", "2026-10-02", "day"},
		{"这周六", "2026-10-03", "2026-10-04", "day"},
		{"下周日", "2026-10-11", "2026-10-12", "day"},
		{"下周天", "2026-10-11", "2026-10-12", "day"},
		{"这个月", "2026-10-01", "2026-11-01", "month"},
		{"上个月", "2026-09-01", "2026-10-01", "month"},
		{"下个月", "2026-11-01", "2026-12-01", "month"},
		{"今年", "2026-01-01", "2027-01-01", "year"},
		{"去年", "2025-01-01", "2026-01-01", "year"},
		{"前年", "2024-01-01", "2025-01-01", "year"},
		{"明年", "2027-01-01", "2028-01-01", "year"},
		{"2025 年", "2025-01-01", "2026-01-01", "year"},
		{"2025 年 3 月", "2025-03-01", "2025-04-01", "month"},
		{"2025年3月12日", "2025-03-12", "2025-03-13", "day"},
		{"3 月 12 日", "2026-03-12", "2026-03-13", "day"},
		{"3 月", "2026-03-01", "2026-04-01", "month"},
		{"12月31日", "2025-12-31", "2026-01-01", "day"},
		{"2月29日", "2024-02-29", "2024-03-01", "day"},
		{"最近", "2026-09-03", "2026-10-03", "range"},
		{"这几天", "2026-09-26", "2026-10-03", "range"},
	}
	for _, tc := range cases {
		t.Run(tc.phrase, func(t *testing.T) {
			got := PlanQuery("我"+tc.phrase+"去了哪里", now, loc).Time
			want := &PlanTime{
				From: queryTestDate(t, tc.from+" 00:00", loc), To: queryTestDate(t, tc.to+" 00:00", loc),
				Precision: tc.precision, Axis: "either", Phrase: tc.phrase,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestPlanQueryCalendarBoundaries(t *testing.T) {
	cases := []struct {
		zone, now, phrase, from, to string
	}{
		{"Australia/Sydney", "2026-10-05 00:10", "这周", "2026-10-05", "2026-10-12"},
		{"Australia/Sydney", "2026-10-04 23:50", "下周", "2026-10-05", "2026-10-12"},
		{"Asia/Shanghai", "2026-10-05 00:10", "上周", "2026-09-28", "2026-10-05"},
		{"UTC", "2026-10-04 23:50", "这周", "2026-09-28", "2026-10-05"},
		{"Asia/Shanghai", "2026-01-01 00:10", "昨天", "2025-12-31", "2026-01-01"},
		{"Australia/Sydney", "2026-01-01 00:10", "去年", "2025-01-01", "2026-01-01"},
		{"UTC", "2026-01-01 00:10", "上个月", "2025-12-01", "2026-01-01"},
		{"Asia/Shanghai", "2026-12-31 23:50", "下个月", "2027-01-01", "2027-02-01"},
		{"UTC", "2026-02-15 12:00", "3 月", "2025-03-01", "2025-04-01"},
		{"UTC", "2026-05-15 12:00", "3 月", "2026-03-01", "2026-04-01"},
		{"UTC", "2026-03-12 00:00", "3月12日", "2026-03-12", "2026-03-13"},
		{"UTC", "2026-03-11 23:59", "3月12日", "2025-03-12", "2025-03-13"},
		{"UTC", "2103-03-01 00:00", "2月29日", "2096-02-29", "2096-03-01"},
		{"Australia/Sydney", "2026-10-04 13:00", "今天", "2026-10-04", "2026-10-05"},
		{"Australia/Sydney", "2026-04-05 13:00", "今天", "2026-04-05", "2026-04-06"},
		{"Australia/Sydney", "2026-10-05 00:10", "昨天", "2026-10-04", "2026-10-05"},
		{"Australia/Sydney", "2026-10-05 00:10", "这几天", "2026-09-29", "2026-10-06"},
	}
	for _, tc := range cases {
		t.Run(tc.zone+"/"+tc.now+"/"+tc.phrase, func(t *testing.T) {
			loc := queryTestLocation(t, tc.zone)
			// Pass a UTC instant: all calendar work must still use loc.
			now := queryTestDate(t, tc.now, loc).UTC()
			got := PlanQuery(tc.phrase, now, loc).Time
			from := queryTestDate(t, tc.from+" 00:00", loc)
			to := queryTestDate(t, tc.to+" 00:00", loc)
			if got == nil || !got.From.Equal(from) || !got.To.Equal(to) || got.From.Location() != loc || got.To.Location() != loc {
				t.Fatalf("got %+v, want [%v, %v)", got, from, to)
			}
		})
	}
}

func TestPlanQueryAxesAndFirstExpression(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	cases := []struct{ text, phrase, axis string }{
		{"上周提过", "上周", "said"}, {"上周去了", "上周", "either"}, {"上周", "上周", "either"},
		{"去年说", "去年", "said"}, {"昨天聊", "昨天", "said"}, {"前天讲", "前天", "said"},
		{"今天问", "今天", "said"}, {"去年告诉", "去年", "said"}, {"去年记", "去年", "said"},
		{"去年  说", "去年", "said"}, {"去年，说", "去年", "either"}, {"去年我说", "去年", "either"},
		{"昨天说下周去", "昨天", "said"}, {"3 月 12 日说去年", "3 月 12 日", "said"},
		{"明年会更好吗", "明年", "either"}, {"今天天气", "今天", "either"},
		{"2025年13月，昨天说", "昨天", "said"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := PlanQuery(tc.text, now, time.UTC).Time
			if got == nil || got.Phrase != tc.phrase || got.Axis != tc.axis {
				t.Fatalf("got %+v, want phrase %q, axis %q", got, tc.phrase, tc.axis)
			}
		})
	}
}

func TestPlanQueryNatureAndRecallWords(t *testing.T) {
	cases := []struct {
		words   []string
		natures []string
		recall  bool
	}{
		{[]string{"想", "打算", "计划", "准备", "要去", "要做", "要干"}, []string{"intention", "plan"}, false},
		{[]string{"喜欢", "爱吃", "不吃", "讨厌", "习惯"}, []string{"preference"}, false},
		{[]string{"决定", "定了"}, []string{"decision"}, false},
		{[]string{"说过", "提过", "来着", "之前", "上次", "记得", "当时"}, nil, true},
		{[]string{"成都有啥好玩的", "", "！？……"}, nil, false},
		{[]string{"当时想计划准备要去，喜欢爱吃，决定定了"}, []string{"intention", "plan", "preference", "decision"}, true},
	}
	for _, tc := range cases {
		for _, word := range tc.words {
			t.Run(word, func(t *testing.T) {
				got := PlanQuery(word, time.Time{}, time.UTC)
				if !reflect.DeepEqual(got.Natures, tc.natures) || got.Recall != tc.recall {
					t.Fatalf("got %+v, want natures %v, recall %v", got, tc.natures, tc.recall)
				}
			})
		}
	}
}

func TestPlanQueryRecallExample(t *testing.T) {
	loc := queryTestLocation(t, "Australia/Sydney")
	now := queryTestDate(t, "2026-10-02 12:00", loc)
	got := PlanQuery("我去年说去成都要干什么来着", now, loc)
	want := QueryPlan{
		Time: &PlanTime{
			From:      queryTestDate(t, "2025-01-01 00:00", loc),
			To:        queryTestDate(t, "2026-01-01 00:00", loc),
			Precision: "year", Axis: "said", Phrase: "去年",
		},
		Natures: []string{"intention", "plan"}, Recall: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPlanQueryUnrecognizedAndLongInput(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, text := range []string{"", "！？……", "成都有什么", "春天", "三月十二日", "2025年13月", "2月30日", "2025年2月29日", "0000年", "12025年", "100月", "2025年100月", "3月123日", strings.Repeat("？", 100000)} {
		t.Run(text[:min(len(text), 30)], func(t *testing.T) {
			if got := PlanQuery(text, now, time.UTC); got.Time != nil {
				t.Fatalf("unexpected time for unsupported expression: %+v", got.Time)
			}
		})
	}
	text := strings.Repeat("？", 100000) + "昨天说，明天去"
	first := PlanQuery(text, now, nil)
	second := PlanQuery(text, now, time.UTC)
	if first.Time == nil || first.Time.Phrase != "昨天" || !reflect.DeepEqual(first, second) {
		t.Fatalf("long input or deterministic UTC fallback failed: %+v / %+v", first, second)
	}
	first.Natures = append(first.Natures, "changed")
	first.Time.Axis = "changed"
	if got := PlanQuery(text, now, nil); !reflect.DeepEqual(got, second) {
		t.Fatalf("a previous result's mutation affected the next call: %+v", got)
	}
}
