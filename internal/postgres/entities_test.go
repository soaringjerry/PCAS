package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestGroundedMentionsBoundsAndLiteralNames(t *testing.T) {
	names := []string{" 我 ", "我们", "杜撰", strings.Repeat("长", 41), " 成都 ", "成都", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	got := groundedMentions(names, "person", "我和我们在成都。一二三四五六七八九"+strings.Repeat("长", 41))
	if len(got) != 8 || got[0].Name != "成都" || got[7].Name != "七" {
		t.Fatalf("unexpected grounded mentions: %#v", got)
	}
	if got := groundedMentions([]string{"Alice"}, "person", "alice"); len(got) != 0 {
		t.Fatal("grounding must be verbatim even though identity is case insensitive")
	}
}

func TestEntityIdentityIsTypedAndConcurrent(t *testing.T) {
	s := testStore(t)
	ownerID := memory.NewID()
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make(chan memory.ID, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var id memory.ID
			err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				name := "Old Wang"
				if i%2 == 0 {
					name = "\u3000old wang\u3000"
				}
				var err error
				id, err = entityTx(ctx, tx, ownerID, "person", name)
				return err
			})
			ids <- id
			errs <- err
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var person memory.ID
	for id := range ids {
		if person == "" {
			person = id
		}
		if id != person {
			t.Fatal("duplicate entity identities")
		}
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		place, err := entityTx(ctx, tx, ownerID, "place", "Old Wang")
		if err != nil {
			return err
		}
		if place == person {
			t.Fatal("different entity types merged")
		}
		self, err := selfEntityTx(ctx, tx, ownerID)
		if err != nil {
			return err
		}
		again, err := selfEntityTx(ctx, tx, ownerID)
		if err == nil && self != again {
			t.Fatal("duplicate self")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExtractionEventWholeCalendarIntervals(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		date  string
		hours time.Duration
	}{{"2026-10-04", 23}, {"2026-04-05", 25}} {
		source := memory.SourceResult{Source: memory.Source{Text: tc.date}}
		from, to, precision := extractionEvent(&extractedWhen{From: tc.date, To: tc.date, Precision: "day", Quote: tc.date}, source, loc)
		if precision != "day" || from == nil || to == nil || to.Sub(*from) != tc.hours*time.Hour {
			t.Fatalf("DST day %s was not a complete calendar day: %v %v %s", tc.date, from, to, precision)
		}
	}
	unknown := memory.SourceResult{Source: memory.Source{Connector: "archive", Text: "下周去成都"}}
	if from, _, _ := extractionEvent(&extractedWhen{From: "2026-10-05", To: "2026-10-12", Precision: "range", Quote: "下周"}, unknown, loc); from != nil {
		t.Fatal("unknown expression time used to resolve a relative date")
	}
	for _, when := range []*extractedWhen{
		{From: "not a date", To: "2026-01-01", Precision: "day", Quote: "2026"},
		{From: "2026-01-02", To: "2026-01-01", Precision: "range", Quote: "2026"},
		{From: "2026-01-01", To: "2046-01-01", Precision: "range", Quote: "2026"},
		{From: "2026-01-01", To: "2026-01-02", Precision: "day", Quote: "杜撰"},
	} {
		if from, _, _ := extractionEvent(when, memory.SourceResult{Source: memory.Source{Text: "2026"}}, loc); from != nil {
			t.Fatal("invalid event date accepted")
		}
	}
}
