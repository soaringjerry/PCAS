package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
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
		deadlines := []cardDeadline{}
		for _, m := range memories {
			d := cardDeadline{N: m.N, Kind: "recurring", Recurrence: strings.TrimSuffix(m.Text, "有课"), Title: "虚构课表"}
			if m.Text == texts[0] {
				d.TimeNote = "没说上午还是下午"
			}
			deadlines = append(deadlines, d)
		}
		secretaryModelReply(w, cardOutput{Fields: map[string][]int{"deadline": {1, 2, 3}}, Deadlines: deadlines})
	})
	refs := statusTestMemories(t, s, scope, len(texts))
	for i, ref := range refs {
		if _, err := s.pool.Exec(ctx, "UPDATE claim_revisions SET value=to_jsonb($3::text) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID, texts[i]); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := s.ProcessCard(ctx, statusTestJob(t, s, scope)); err != nil {
			t.Fatal(err)
		}
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

func TestStatusInitialCardsSelfFirstWithEqualReadyTimes(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	calls := make(chan string, 6)
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		key, _ := statusRegressionInput(t, r)
		calls <- key
		secretaryModelReply(w, cardOutput{Fields: map[string][]int{"status": {1, 2, 3}}})
	})
	statusTestMemories(t, s, scope, 7) // Seven project members and three self:rule members.
	for i, category := range []string{"identity", "taste", "goal"} {
		for n := range i + 4 {
			ref := organizeTestMemory(t, s, scope, fmt.Sprintf("虚构自我资料 %s %d。", category, n))
			if _, err := s.pool.Exec(ctx, "UPDATE claim_revisions SET category=$3,durable=true WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID, category); err != nil {
				t.Fatal(err)
			}
		}
	}
	var topic memory.ID
	for i := range 3 {
		ref := organizeTestMemory(t, s, scope, fmt.Sprintf("虚构主题资料 %d。", i))
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			var err error
			topic, err = entityTx(ctx, tx, scope.OwnerID, "topic", "虚构小主题")
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, "INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'topic')", scope.OwnerID, ref.ID, topic)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var project string
	if err := s.pool.QueryRow(ctx, "SELECT key FROM status_current_members WHERE owner_id=$1 AND kind='project' LIMIT 1", scope.OwnerID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	want := []string{"self:goal", "self:taste", "self:identity", "self:rule", project, "entity:" + string(topic)}
	if _, err := s.ScheduleStatus(ctx, time.Now().Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'memory.card:%'", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	// Clock alignment can collapse ready times. IDs deliberately favor the
	// reverse order so the test cannot pass by a lucky random UUID tie-break.
	for i, key := range want {
		if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET id=$3,available_at=now() WHERE owner_id=$1 AND stage=$2", scope.OwnerID, fmt.Sprintf("%s:%d:%s", CardStage, CardVersion, key), fmt.Sprintf("00000000-0000-4000-8000-%012d", len(want)-i)); err != nil {
			t.Fatal(err)
		}
	}
	// Make every ready time exactly equal, across the preceding statements.
	if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET available_at=now() WHERE owner_id=$1", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	for _, key := range want {
		j, err := s.Claim(ctx, time.Minute)
		if err != nil || j == nil {
			t.Fatal(j, err)
		}
		if err := s.ProcessCard(ctx, *j); err != nil {
			t.Fatal(err)
		}
		if got := <-calls; got != key {
			t.Fatalf("first build order: got %s, want %s", got, key)
		}
	}
	about, err := s.About(ctx, scope, "")
	if err != nil || about.Building.Done != len(want) || about.Building.Total != len(want) {
		t.Fatal(about, err)
	}
}
