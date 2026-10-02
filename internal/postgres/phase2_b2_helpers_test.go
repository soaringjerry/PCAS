package postgres

// These tests are written against the batch2 contract, before implementation.
// Only the existing batch1 public entry points and fixture helpers are reused.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type b2Oracle struct {
	Time struct {
		Monday []int    `json:"next_week_monday_days_by_weekday_sun0"`
		Friday []int    `json:"friday_days_by_weekday_sun0"`
		Week   int      `json:"next_week_duration_calendar_days"`
		Day    int      `json:"day_duration_calendar_days"`
		Zones  []string `json:"zones"`
	} `json:"time_oracle"`
	Fixtures  map[string]json.RawMessage `json:"fixtures"`
	Sequences map[string]json.RawMessage `json:"sequences"`
}

func b2Gold(t *testing.T) b2Oracle {
	t.Helper()
	data, err := os.ReadFile("../../testdata/phase2/b2-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var out b2Oracle
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func b2Want[T any](t *testing.T, seq, key string) T {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b2Gold(t).Sequences[seq], &fields); err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(fields[key], &out); err != nil {
		t.Fatal(seq, key, err)
	}
	return out
}
func b2Fixture(t *testing.T, key, field string) string {
	t.Helper()
	data := b2Gold(t).Fixtures[key]
	if field != "" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		data = fields[field]
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		t.Fatal(err)
	}
	return text
}
func b2Equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}
func b2Exec(t *testing.T, s *Store, sql string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func b2Count(t *testing.T, s *Store, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func b2Zone(t *testing.T, s *Store, scope memory.Scope, zone string) {
	t.Helper()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"timezone": zone})})
}
func b2Anchor(t *testing.T, zone string) time.Time {
	t.Helper()
	return testsupport.DateFromToday(t, zone, -7, 9, 30)
}
func b2Interval(t *testing.T, zone, kind string) (time.Time, time.Time) {
	t.Helper()
	a := b2Anchor(t, zone)
	g := b2Gold(t)
	offset, days := g.Time.Monday[int(a.Weekday())], g.Time.Week
	if kind == "day" {
		offset, days = g.Time.Friday[int(a.Weekday())], g.Time.Day
	}
	d := a.AddDate(0, 0, offset)
	from := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, a.Location())
	return from, from.AddDate(0, 0, days)
}
func b2When(from, to time.Time, precision, quote string) map[string]any {
	return map[string]any{"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "precision": precision, "quote": quote}
}
func b2Item(text, quote, nature string) map[string]any {
	i := b1ExtractItem(text, nature)
	i["quote"] = quote
	i["subject"] = "我"
	i["people"] = []string{}
	i["places"] = []string{}
	i["organizations"] = []string{}
	return i
}
func b2TripItem(t *testing.T, zone string) map[string]any {
	t.Helper()
	from, to := b2Interval(t, zone, "range")
	i := b2Item(b2Fixture(t, "trip", "memory"), b2Fixture(t, "trip", "text"), "plan")
	i["people"] = b2Want[[]string](t, "X1", "people")
	i["places"] = b2Want[[]string](t, "X1", "places")
	i["when"] = b2When(from, to, "range", "下周")
	return i
}
func b2Source(t *testing.T, s *Store, scope memory.Scope, connector, role, text string, at *time.Time) memory.Ref {
	t.Helper()
	ref := mustIngest(t, s, scope, memory.IngestRequest{Connector: connector, ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: text, MediaType: "text/plain", ExpressedAt: at}).Ref
	if role != "" {
		b2Exec(t, s, `INSERT INTO source_contexts(owner_id,source_id,source_version,role) VALUES($1,$2,$3,$4)`, string(scope.OwnerID), string(ref.ID), ref.Version, role)
	}
	return ref
}
func b2Extract(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, source memory.Ref, items ...map[string]any) []workspace.Memory {
	t.Helper()
	return b1Extract(t, s, scope, f, source, items...)
}
func b2One(t *testing.T, items []workspace.Memory) workspace.Memory {
	t.Helper()
	if len(items) != 1 {
		t.Fatalf("want exactly one memory, got %d: %+v", len(items), items)
	}
	return items[0]
}
func b2Ref(m workspace.Memory) memory.Ref {
	return memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
}
func b2Subject(t *testing.T, s *Store, scope memory.Scope, m workspace.Memory) (string, string) {
	t.Helper()
	var id, kind string
	if err := s.pool.QueryRow(context.Background(), `SELECT c.subject_id::text,e.entity_type FROM claim_revisions c JOIN memory_records r ON (r.owner_id,r.id)=(c.owner_id,c.subject_id) JOIN entity_versions e ON (e.owner_id,e.entity_id,e.version)=(r.owner_id,r.id,r.version) WHERE c.owner_id=$1 AND c.claim_id=$2 AND c.version=$3`, string(scope.OwnerID), m.ID, m.Version).Scan(&id, &kind); err != nil {
		t.Fatal(err)
	}
	return id, kind
}

type b2Mention struct {
	EntityID string `json:"entityId"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

func b2Mentions(t *testing.T, m workspace.Memory) []b2Mention {
	t.Helper()
	var wire struct {
		Mentions []b2Mention `json:"mentions"`
	}
	if err := json.Unmarshal(asJSON(m), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Mentions == nil {
		t.Error("mentions must be an array, including when empty")
	}
	return wire.Mentions
}
func b2MentionNames(t *testing.T, m workspace.Memory, role string) []string {
	t.Helper()
	names := []string{}
	for _, x := range b2Mentions(t, m) {
		if x.Role == role {
			if x.EntityID == "" {
				t.Error("mention missing entityId")
			}
			names = append(names, x.Name)
		}
	}
	sort.Strings(names)
	return names
}
func b2Names(t *testing.T, m workspace.Memory, role string, want []string) {
	t.Helper()
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	b2Equal(t, b2MentionNames(t, m, role), sorted)
}
func b2MentionID(t *testing.T, m workspace.Memory, name, role string) string {
	t.Helper()
	for _, x := range b2Mentions(t, m) {
		if x.Name == name && x.Role == role {
			return x.EntityID
		}
	}
	t.Fatalf("missing mention %s/%s", name, role)
	return ""
}
func b2Time(t *testing.T, m workspace.Memory, key string, want *time.Time) {
	t.Helper()
	wire := b1Map(t, m)
	raw, ok := wire[key]
	if want == nil {
		if ok {
			t.Errorf("unknown %s must be omitted: %v", key, raw)
		}
		return
	}
	str, ok := raw.(string)
	if !ok {
		t.Errorf("missing %s", key)
		return
	}
	got, err := time.Parse(time.RFC3339, str)
	if err != nil || !got.Equal(*want) {
		t.Errorf("%s=%s want %s (%v)", key, str, want.Format(time.RFC3339), err)
	}
}
func b2Event(t *testing.T, m workspace.Memory, from, to *time.Time, precision string) {
	t.Helper()
	b2Time(t, m, "eventFrom", from)
	b2Time(t, m, "eventTo", to)
	w := b1Map(t, m)
	if precision == "" {
		if _, ok := w["eventPrecision"]; ok {
			t.Error("unknown eventPrecision must be omitted")
		}
	} else {
		b2Equal(t, w["eventPrecision"], precision)
	}
}
func b2Confirmation(t *testing.T, s *Store, scope memory.Scope, m workspace.Memory, want string) {
	t.Helper()
	var got string
	if err := s.pool.QueryRow(context.Background(), `SELECT confirmation FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, string(scope.OwnerID), m.ID, m.Version).Scan(&got); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, got, want)
}
func b2ExtractionRecord(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref, state string, items int) {
	t.Helper()
	var got string
	var count, extractor int
	if err := s.pool.QueryRow(context.Background(), `SELECT state,items,extractor FROM source_extractions WHERE owner_id=$1 AND source_id=$2 AND source_version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&got, &count, &extractor); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, got, state)
	b2Equal(t, count, items)
	b2Equal(t, extractor, 2)
}

type b2Page struct {
	Items []workspace.Memory `json:"items"`
	Next  string             `json:"next"`
	Total int                `json:"total"`
}

func b2List(t *testing.T, s *Store, scope memory.Scope, query string) b2Page {
	t.Helper()
	w := b1HTTP(t, s, scope, "GET", "/v1/workspace/memories"+query, nil)
	if w.Code != 200 {
		t.Fatalf("memory list: %d %s", w.Code, w.Body.String())
	}
	var page b2Page
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
func b2Detail(t *testing.T, s *Store, scope memory.Scope, id string) workspace.Memory {
	t.Helper()
	w := b1HTTP(t, s, scope, "GET", "/v1/workspace/memories/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("memory detail: %d %s", w.Code, w.Body.String())
	}
	var m workspace.Memory
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, m.ID, id)
	return m
}
func b2Snapshot(t *testing.T, s *Store, scope memory.Scope) workspace.State {
	t.Helper()
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
func b2ParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func b2Archive(t *testing.T, s *Store, scope memory.Scope, at time.Time, user, assistant string) (memory.Ref, memory.Ref, memory.Ref) {
	t.Helper()
	mapping := map[string]any{"u": map[string]any{"parent": nil, "message": map[string]any{"id": "u", "author": map[string]string{"role": "user"}, "create_time": at.Unix(), "content": map[string]any{"parts": []string{user}}}}}
	current := "u"
	if assistant != "" {
		current = "a"
		mapping["a"] = map[string]any{"parent": "u", "message": map[string]any{"id": "a", "author": map[string]string{"role": "assistant"}, "create_time": at.Add(time.Minute).Unix(), "content": map[string]any{"parts": []string{assistant}}}}
	}
	data := asJSON([]any{map[string]any{"id": string(memory.NewID()), "title": "合成历史", "current_node": current, "mapping": mapping}})
	out, err := s.ImportArchive(context.Background(), scope, "conversations.json", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Refs) != 1 {
		t.Fatal("expected one archive root", out.Refs)
	}
	root := out.Refs[0]
	if err = s.ProcessAttachment(context.Background(), leaseStage(t, s, scope, root, "source.parse")); err != nil {
		t.Fatal(err)
	}
	find := func(text string) memory.Ref {
		ref := memory.Ref{Kind: memory.SourceKind}
		if text != "" {
			if err := s.pool.QueryRow(context.Background(), `SELECT v.source_id::text,v.version FROM source_versions v JOIN archive_entries e ON (e.owner_id,e.source_id,e.source_version)=(v.owner_id,v.source_id,v.version) WHERE v.owner_id=$1 AND e.archive_id=$2 AND v.body=$3`, string(scope.OwnerID), string(root.ID), text).Scan(&ref.ID, &ref.Version); err != nil {
				t.Fatal(err)
			}
		}
		return ref
	}
	return root, find(user), find(assistant)
}
func b2MemoryIDs(items []workspace.Memory) []string {
	out := []string{}
	for _, m := range items {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out
}
func b2SQLIDs(t *testing.T, s *Store, query string, args ...any) []string {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}
func b2Label(n int) string { return fmt.Sprintf("合成记忆%03d", n) }

func jsonUnmarshalOracle(t *testing.T, seq, key string, out any) error {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b2Gold(t).Sequences[seq], &fields); err != nil {
		return err
	}
	return json.Unmarshal(fields[key], out)
}
func b2Supplement(t *testing.T, key string, out any) {
	t.Helper()
	b2SupplementFrom(t, "supplement_5865411", key, out)
}
func b2SupplementFrom(t *testing.T, supplement, key string, out any) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/phase2/b2-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err = json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(all[supplement], &fields); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(fields[key], out); err != nil {
		t.Fatal(err)
	}
}
