package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func statusRegressionInput(t *testing.T, r *http.Request) (string, []cardMemory) {
	t.Helper()
	var req struct {
		Messages []struct{ Role, Content string }
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
		return "", nil
	}
	for _, message := range req.Messages {
		if message.Role == "user" {
			var prompt struct {
				Key      string
				Memories []cardMemory
			}
			if err := json.Unmarshal([]byte(message.Content), &prompt); err != nil {
				t.Error(err)
			}
			return prompt.Key, prompt.Memories
		}
	}
	t.Error("missing card input")
	return "", nil
}

func TestStatusRecurringSpacedHourKeepsSpecificAmbiguity(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	texts := []string{"每周二 7 点有课", "每周二晚上有课", "每周二上午 7 点有课"}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		_, memories := statusRegressionInput(t, r)
		items := []any{}
		for _, m := range memories {
			d := cardDeadline{N: m.N, Kind: "recurring", Recurrence: strings.TrimSuffix(m.Text, "有课"), Title: "虚构课表"}
			if m.Text == texts[0] {
				d.TimeNote = "没说上午还是下午"
			}
			items = append(items, map[string]any{"n": m.N, "category": "rule", "durable": true, "unrestricted": true, "scope": "", "deadlines": []cardDeadline{d}})
		}
		secretaryModelReply(w, map[string]any{"items": items})
	})
	refs := statusTestMemories(t, s, scope, len(texts))
	for i, ref := range refs {
		if _, err := s.pool.Exec(ctx, "UPDATE claim_revisions SET value=to_jsonb($3::text) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID, texts[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ProcessOrganize(ctx, organizeTestJob(t, s, scope)); err != nil {
		t.Fatal(err)
	}

	about, err := s.About(ctx, scope, "")
	if err != nil || len(about.Deadlines) != 3 {
		t.Fatal(about, err)
	}
	for _, d := range about.Deadlines {
		if d.MemoryID == string(refs[0].ID) {
			if !strings.Contains(d.TimeNote, "上午") || !strings.Contains(d.TimeNote, "下午") {
				t.Fatalf("lost AM/PM ambiguity: %+v", d)
			}
		} else if d.TimeNote != "" {
			t.Fatalf("fabricated ambiguity for explicit period: %+v", d)
		}
	}
}
