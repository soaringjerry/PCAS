package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

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
		items := []any{}
		for _, m := range prompt.Memories {
			items = append(items, map[string]any{"n": m.N, "category": "rule", "durable": true, "unrestricted": true, "scope": "", "deadlines": []cardDeadline{{Kind: "recurring", Recurrence: "每周二7点", Title: "虚构课程", TimeNote: "未说明上午还是下午"}, {Kind: "recurring", Recurrence: "每周二7点", Title: "虚构课程", TimeNote: "未说明上午还是下午"}}})
		}
		secretaryModelReply(w, map[string]any{"items": items})
	})
	refs := statusTestMemories(t, s, scope, 3)
	for _, ref := range refs {
		if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET value=to_jsonb('每周二7点有课'::text) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ProcessOrganize(context.Background(), organizeTestJob(t, s, scope)); err != nil {
		t.Fatal(err)
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
