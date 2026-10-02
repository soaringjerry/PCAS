package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b3Gold(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("../../testdata/phase2/b3-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]json.RawMessage
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return g
}
func b3Text(t *testing.T, key string) string {
	t.Helper()
	var fields map[string]string
	if err := json.Unmarshal(b3Gold(t)["fixtures"], &fields); err != nil {
		t.Fatal(err)
	}
	if fields[key] == "" {
		t.Fatal("missing frozen fixture", key)
	}
	return fields[key]
}
func b3Exec(t *testing.T, s *Store, sql string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func b3Zone(t *testing.T, s *Store, scope memory.Scope, zone string) time.Time {
	t.Helper()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": zone})})
	return testsupport.DateFromToday(t, zone, 0, 12, 0)
}
func b3Year(now time.Time, offset int, month time.Month, day int) time.Time {
	return time.Date(now.Year()+offset, month, day, 12, 0, 0, 0, now.Location())
}

// Entities and aliases are direct synthetic records, unrelated to extraction.
func b3Entity(t *testing.T, s *Store, scope memory.Scope, kind, name string, aliases ...string) memory.ID {
	t.Helper()
	id := memory.NewID()
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'entity',1)`, []any{scope.OwnerID, id}},
		{`INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,1)`, []any{scope.OwnerID, id}},
		{`INSERT INTO entities(owner_id,id) VALUES($1,$2)`, []any{scope.OwnerID, id}},
		{`INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,$3,$4)`, []any{scope.OwnerID, id, kind, name}},
	} {
		if _, err := tx.Exec(context.Background(), q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, alias := range aliases {
		if _, err := tx.Exec(context.Background(), `INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)`, scope.OwnerID, id, alias); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return id
}

type b3Mention struct {
	ID   memory.ID
	Role string
}
type b3ClaimSpec struct {
	Text, Nature, Acquisition, Project string
	Subject                            memory.ID
	Said                               *time.Time
	EventFrom, EventTo                 *time.Time
	Precision                          string
	Mentions                           []b3Mention
	Source                             memory.Ref
	Agents                             []string
}

// Claims, ordinary evidence and structured metadata are all synthetic records.
// Direct inserts keep Q2/K acceptance independent of batch2 extraction and
// Commit validation. Real DeskTurn/requestRun/Correct/Undo remain under test.
func b3Claim(t *testing.T, s *Store, scope memory.Scope, spec b3ClaimSpec) memory.Ref {
	t.Helper()
	if spec.Nature == "" {
		spec.Nature = "plan"
	}
	if spec.Acquisition == "" {
		spec.Acquisition = "direct"
	}
	precision := spec.Precision
	if precision == "" {
		precision = "unknown"
	}
	id := memory.NewID()
	claimScope := map[string]string{}
	if spec.Project != "" {
		claimScope["project_id"] = spec.Project
	}
	tx, err := s.pool.Begin(t.Context())
	if err != nil {
		t.Fatal("begin b3 claim fixture:", err)
	}
	defer tx.Rollback(t.Context())
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(t.Context(), q, args...); err != nil {
			t.Fatal("insert b3 claim fixture:", err)
		}
	}
	exec(`INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'claim',1)`, scope.OwnerID, id)
	exec(`INSERT INTO record_versions(owner_id,record_id,version,expressed_at) VALUES($1,$2,1,$3)`, scope.OwnerID, id, spec.Said)
	exec(`INSERT INTO claims(owner_id,id) VALUES($1,$2)`, scope.OwnerID, id)
	exec(`INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,scope,nature,acquisition,confirmation,change_type,event_from,event_to,event_precision) VALUES($1,$2,1,$3,'验收事项',$4::jsonb,$5::jsonb,$6,$7,'adopted','initial',$8,$9,$10)`, scope.OwnerID, id, spec.Subject, string(asJSON(spec.Text)), string(asJSON(claimScope)), spec.Nature, spec.Acquisition, spec.EventFrom, spec.EventTo, precision)
	exec(`INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3)`, scope.OwnerID, id, scope.PrincipalID)
	exec(`INSERT INTO activity(owner_id,record_id,last_effective_use_at) VALUES($1,$2,now())`, scope.OwnerID, id)
	if spec.Source.ID != "" {
		exec(`INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,1,'{}',$6,'supports')`, scope.OwnerID, memory.NewID(), spec.Source.ID, spec.Source.Version, id, spec.Acquisition)
	}
	for _, m := range spec.Mentions {
		exec(`INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,$4)`, scope.OwnerID, id, m.ID, m.Role)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal("commit b3 claim fixture:", err)
	}
	agents := spec.Agents
	if agents == nil {
		agents = []string{"model", "manual"}
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(id), AgentIDs: agents})
	return memory.Ref{ID: id, Version: 1, Kind: memory.ClaimKind}
}
func b3Source(t *testing.T, s *Store, scope memory.Scope, text string, said time.Time) memory.Ref {
	t.Helper()
	return mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: text, MediaType: "text/plain", ExpressedAt: &said}).Ref
}

type b3Fixture struct {
	Now                       time.Time
	Self, Chengdu, Dali, Wang memory.ID
	Refs                      map[string]memory.Ref
}

func b3FixtureData(t *testing.T, s *Store, scope memory.Scope, zone string) b3Fixture {
	t.Helper()
	d := b3Fixture{Now: b3Zone(t, s, scope, zone), Refs: map[string]memory.Ref{}}
	d.Self = b3Entity(t, s, scope, "self", "用户本人", "本人")
	d.Chengdu = b3Entity(t, s, scope, "place", "成都", "成都", "Chengdu")
	d.Dali = b3Entity(t, s, scope, "place", "大理", "大理")
	d.Wang = b3Entity(t, s, scope, "person", "老王", "老王")
	for _, key := range []string{"self_old_a", "self_old_b", "self_current", "dali", "wang", "hidden"} {
		month := map[string]time.Month{"self_old_a": time.December, "self_old_b": time.November, "self_current": time.January, "dali": time.January, "wang": time.December, "hidden": time.October}[key]
		said := b3Year(d.Now, -1, month, 3)
		if key == "wang" {
			said = b3Year(d.Now, -1, time.December, 30)
		}
		if key == "self_current" {
			said = b3Year(d.Now, 0, time.January, 3)
		}
		subject, place := d.Self, d.Chengdu
		if key == "dali" {
			place = d.Dali
		}
		if key == "wang" {
			subject = d.Wang
		}
		event := b3Year(d.Now, -1, time.November, 1)
		end := event.AddDate(0, 1, 0)
		spec := b3ClaimSpec{Text: b3Text(t, key), Subject: subject, Said: &said, EventFrom: &event, EventTo: &end, Precision: "month", Mentions: []b3Mention{{place, "place"}, {d.Wang, "person"}}}
		if key == "self_old_b" {
			spec.Nature = "intention"
		}
		if key == "hidden" {
			spec.Agents = []string{"manual"}
		}
		d.Refs[key] = b3Claim(t, s, scope, spec)
	}
	d.Refs["raw_old"] = b3Source(t, s, scope, b3Text(t, "raw_old"), b3Year(d.Now, -1, time.December, 1))
	d.Refs["raw_current"] = b3Source(t, s, scope, b3Text(t, "raw_current"), b3Year(d.Now, 0, time.January, 1))
	return d
}

func b3Order(t *testing.T, text string, fragments ...string) {
	t.Helper()
	prev := -1
	for _, part := range fragments {
		n := strings.Index(text, part)
		if n < 0 {
			t.Errorf("missing ordered fragment %q", part)
		} else if n <= prev {
			t.Errorf("fragment %q at %d follows %d incorrectly", part, n, prev)
		}
		prev = n
	}
}
func b3MemoryLine(t *testing.T, prompt, text string) string {
	t.Helper()
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("no memory line for %q", text)
	return ""
}
func b3Timeline(t *testing.T, out workspace.DeskTurnResponse) []map[string]any {
	t.Helper()
	for _, card := range out.Turn.Cards {
		if card.Kind == "timeline" {
			var items []map[string]any
			if err := json.Unmarshal(asJSON(card.Items), &items); err != nil {
				t.Fatal(err)
			}
			return items
		}
	}
	return nil
}
func b3TimelineText(t *testing.T, out workspace.DeskTurnResponse, text string) map[string]any {
	t.Helper()
	for _, item := range b3Timeline(t, out) {
		if item["text"] == text {
			return item
		}
	}
	t.Fatalf("timeline lacks %q: %s", text, asJSON(out.Turn.Cards))
	return nil
}
func b3Used(refs ...memory.Ref) string {
	used := []string{}
	for i := range refs {
		used = append(used, fmt.Sprintf("M%d", i+1))
	}
	return string(asJSON(map[string]any{"reply": "验收时间轴固定回答", "used": used, "actions": []any{}}))
}
func b3MustRef(t *testing.T, actual any, ref memory.Ref) {
	t.Helper()
	if actual != string(ref.ID) {
		t.Errorf("memory id=%v want %s", actual, ref.ID)
	}
}
func b3Date(t *testing.T, raw any, want time.Time) {
	t.Helper()
	text, ok := raw.(string)
	if !ok {
		t.Errorf("date=%v want %s", raw, want)
		return
	}
	at, err := time.Parse(time.RFC3339, text)
	if err != nil || !at.Equal(want) {
		t.Errorf("date=%q want %s (%v)", text, want.Format(time.RFC3339), err)
	}
}
func b3RefLine(ref memory.Ref) string { return fmt.Sprintf("%s@%d", ref.ID, ref.Version) }
