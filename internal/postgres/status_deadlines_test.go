package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestStatusDeadlineGrounding(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	said := "2026-10-05T09:00:00+08:00"
	now, _ := time.Parse(time.RFC3339, said)
	cases := []struct {
		text, kind, at, recurrence, note string
		valid                            bool
	}{
		{"周五上午十点前把汇报发给主管", "deadline", "2026-10-09T10:00:00+08:00", "", "", true},
		{"下周二下午三点去看展", "appointment", "2026-10-13T15:00:00+08:00", "", "", true},
		{"明天早上八点半去看展", "appointment", "2026-10-06T08:30:00+08:00", "", "", true},
		{"2026年10月7日下午三点半寄信", "deadline", "2026-10-07T15:30:00+08:00", "", "", true},
		{"2026-10-07 15:30 寄信", "deadline", "2026-10-07T15:30:00+08:00", "", "", true},
		{"每周二晚上有课", "recurring", "", "每周二晚上", "", true},
		{"每周二7点有课", "recurring", "", "每周二7点", "未说明上午还是下午", true},
		{"每周二晚上有课", "recurring", "", "每周二晚上9点", "", false},
		{"云杉喜欢蓝色", "deadline", "2026-10-09T10:00:00+08:00", "", "", false},
		{"周五上午十点交稿", "deadline", "2026-10-09T11:00:00+08:00", "", "", false},
		{"昨天上午十点交稿", "deadline", "2026-10-04T10:00:00+08:00", "", "", false},
		{"2026年2月30日上午十点交稿", "deadline", "2026-03-02T10:00:00+08:00", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.text+c.at, func(t *testing.T) {
			d := cardDeadline{N: 1, Kind: c.kind, Title: "虚构安排", Recurrence: c.recurrence}
			if c.at != "" {
				d.At = &c.at
			}
			got, valid := validateCardDeadline(d, cardMemory{Text: c.text, ExpressedAt: said}, loc, now)
			if valid != c.valid || valid && got.TimeNote != c.note {
				t.Fatal(got, valid, c.valid)
			}
		})
	}
	ny, _ := time.LoadLocation("America/New_York")
	gap := "2027-03-14T03:30:00-04:00"
	if _, ok := validateCardDeadline(cardDeadline{Kind: "appointment", At: &gap, Title: "虚构"}, cardMemory{Text: "2027年3月14日凌晨两点半上课"}, ny, now); ok {
		t.Fatal("accepted DST gap")
	}
}
func TestStatusDeadlineWritesDeduplicateAndDelete(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var prompt struct{ Memories []cardMemory }
		for _, m := range req.Messages {
			if m.Role == "user" {
				_ = json.Unmarshal([]byte(m.Content), &prompt)
			}
		}
		ds := []cardDeadline{}
		for _, m := range prompt.Memories {
			ds = append(ds, cardDeadline{N: m.N, Kind: "recurring", Recurrence: "每周二7点", Title: "虚构课程"}, cardDeadline{N: m.N, Kind: "recurring", Recurrence: "每周二7点", Title: "虚构课程"})
		}
		secretaryModelReply(w, cardOutput{Fields: map[string][]int{"deadline": {1, 2, 3}}, Deadlines: ds})
	})
	refs := statusTestMemories(t, s, scope, 3)
	for _, ref := range refs {
		if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET value=to_jsonb('每周二7点有课'::text) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	about, err := s.About(context.Background(), scope, "")
	if err != nil || len(about.Deadlines) != 3 {
		t.Fatal(about, err)
	}
	for _, d := range about.Deadlines {
		if d.TimeNote != "未说明上午还是下午" {
			t.Fatal(d)
		}
	}
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM memory_records WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[0].ID); err != nil {
		t.Fatal(err)
	}
	about, err = s.About(context.Background(), scope, "")
	if err != nil || len(about.Deadlines) != 2 {
		t.Fatal(about, err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE claims SET retired='superseded',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[1].ID, memory.NewID()); err != nil {
		t.Fatal(err)
	}
	about, err = s.About(context.Background(), scope, "")
	if err != nil || len(about.Deadlines) != 1 {
		t.Fatal(about, err)
	}
}
