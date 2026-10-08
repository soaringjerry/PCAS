package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Routing adapter from the backend's skeleton signatures (2026-10-07).
// Expectations remain those in the independently committed phase3_contract.json.
// These wire-only structs let this test PR compile before the skeleton is merged.
type phase3Evidence struct {
	Kind, ID string
	Version  int
}
type phase3Sentence struct {
	Text     string
	Evidence []phase3Evidence
}
type phase3Handover struct {
	ProjectID                       string
	Conclusion, Blockers, NextSteps []phase3Sentence
	WrittenAt                       *string
	Stale                           bool
}
type phase3Version struct {
	DocumentID, Title, Body, Author, WrittenAt string
	Version                                    int
	BasedOn                                    *int
	RunID                                      *string
}
type phase3VersionList struct {
	Items          []phase3Version
	CurrentVersion int
}
type phase3Change struct {
	Kind, Before, After     string
	BeforeIndex, AfterIndex *int
}
type phase3Diff struct {
	DocumentID             string
	FromVersion, ToVersion int
	Changes                []phase3Change
}
type phase3TimelineItem struct {
	ID, Title, Due, Status string
	StartDate              *string
	EstimatedHours         *float64
	Overdue                bool
}
type phase3Timeline struct {
	ProjectID  string
	DailyHours float64
	Items      []phase3TimelineItem
	WithoutDue []workspace.Item
}
type phase3HTTP struct {
	Base   string
	Client *http.Client
}

func phase3NewHTTP(t *testing.T, f *phase3LoadedFixture) *phase3HTTP {
	t.Helper()
	s := f.Store
	api := httpapi.New(s, s, b1Auth{f.Scope}, s.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Editor: s, Writer: s, Attachments: s})
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	return &phase3HTTP{server.URL, server.Client()}
}
func (h *phase3HTTP) call(ctx context.Context, method, path string, body any) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, h.Base+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer phase3-fictitious-test")
	r, err := h.Client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer r.Body.Close()
	out, err := io.ReadAll(r.Body)
	return r.StatusCode, out, err
}
func (h *phase3HTTP) get(t *testing.T, ctx context.Context, path string, out any) time.Duration {
	t.Helper()
	start := time.Now()
	code, raw, err := h.call(ctx, "GET", path, nil)
	elapsed := time.Since(start)
	if err != nil || code != 200 {
		t.Fatalf("GET %s status=%d body=%s err=%v", path, code, raw, err)
	}
	decodePhase3(t, raw, out)
	return elapsed
}
func (h *phase3HTTP) command(t *testing.T, ctx context.Context, c map[string]any) workspace.State {
	t.Helper()
	if _, ok := c["requestId"]; !ok {
		c["requestId"] = string(memory.NewID())
	}
	code, raw, err := h.call(ctx, "POST", "/v1/workspace/commands", c)
	if err != nil || code != 200 {
		t.Fatalf("command %v status=%d body=%s err=%v", c["type"], code, raw, err)
	}
	var state workspace.State
	decodePhase3(t, raw, &state)
	return state
}
func phase3AllTableDigest(t *testing.T, f *phase3LoadedFixture) map[string]string {
	t.Helper()
	ctx := f.Context
	rows, err := f.Store.pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	result := map[string]string{}
	for _, table := range tables {
		name := pgx.Identifier{table}.Sanitize()
		var digest string
		// xmin catches UPDATE-of-identical-values as a write. Sort full row JSON;
		// this disposable database contains no other user or unrelated worker.
		err := f.Store.pool.QueryRow(ctx, `SELECT md5(coalesce(string_agg(to_jsonb(t)::text || ':' || t.xmin::text,'|' ORDER BY to_jsonb(t)::text),'')) FROM `+name+` t`).Scan(&digest)
		if err != nil {
			t.Fatalf("digest %s: %v", table, err)
		}
		result[table] = digest
	}
	return result
}
func phase3ProjectPath(id, part string) string   { return "/v1/workspace/projects/" + id + "/" + part }
func phase3DocumentPath(id, part string) string  { return "/v1/workspace/documents/" + id + "/" + part }
func phase3Milliseconds(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
func phase3Latency(t *testing.T, name string, samples []time.Duration) {
	t.Helper()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	n := len(samples)
	t.Logf("T4 %s samples=%d min_ms=%.3f p50_ms=%.3f p95_ms=%.3f max_ms=%.3f", name, n, phase3Milliseconds(samples[0]), phase3Milliseconds(samples[n/2]), phase3Milliseconds(samples[(95*n-1)/100]), phase3Milliseconds(samples[n-1]))
}
func TestPhase3T2G6T4ReadOnlyHTTPScale(t *testing.T) {
	for _, tc := range []struct{ part, finding string }{{"handover", "S-P3-001"}, {"versions", "S-P3-002"}, {"diff", "S-P3-002"}, {"timeline", "S-P3-005"}, {"files", "S-P3-004"}, {"workspace", ""}} {
		t.Run(tc.part, func(t *testing.T) {
			if tc.finding != "" {
				phase3Finding(t, tc.finding)
			}
			f := phase3LoadFixture(t)
			h := phase3NewHTTP(t, f)
			p := f.Gold.Projects[0]
			doc := p.Documents[0]
			path := "/v1/workspace"
			switch tc.part {
			case "handover", "timeline":
				path = phase3ProjectPath(p.ID, tc.part)
			case "versions", "diff":
				path = phase3DocumentPath(doc.ID, tc.part)
			case "files":
				path = "/v1/workspace/items/" + p.ID + "/files"
			}
			before := phase3AllTableDigest(t, f)
			times := []time.Duration{}
			for i := 0; i < 12; i++ {
				var raw json.RawMessage
				elapsed := h.get(t, f.Context, path, &raw)
				times = append(times, elapsed)
				if tc.part == "workspace" && elapsed > time.Second {
					t.Errorf("workspace >1s: %s", elapsed)
				}
			}
			after := phase3AllTableDigest(t, f)
			if fmt.Sprint(before) != fmt.Sprint(after) {
				for table, v := range before {
					if after[table] != v {
						t.Errorf("read path changed %s", table)
					}
				}
			}
			phase3Latency(t, tc.part, times)
		})
	}
}
func TestPhase3T1D1AllVersionsAndBodiesAtScale(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	for _, p := range f.Gold.Projects {
		for _, d := range p.Documents {
			var list phase3VersionList
			h.get(t, f.Context, phase3DocumentPath(d.ID, "versions"), &list)
			if len(list.Items) != len(d.Versions) {
				t.Fatalf("document %s retained=%d want=%d", d.ID, len(list.Items), len(d.Versions))
			}
			seen := map[int]bool{}
			for _, v := range list.Items {
				if v.Version < 1 || v.Version > len(d.Versions) || seen[v.Version] {
					t.Fatal("invalid/repeated version number", v.Version)
				}
				seen[v.Version] = true
				gold := d.Versions[v.Version-1]
				if v.Version != gold.Number || v.Author != gold.Author || v.WrittenAt == "" {
					t.Fatalf("metadata/order %+v want=%+v", v, gold)
				}
				var body phase3Version
				h.get(t, f.Context, phase3DocumentPath(d.ID, fmt.Sprintf("versions/%d", v.Version)), &body)
				if body.Body != gold.Body {
					t.Fatalf("version %d body=%q want=%q", v.Version, body.Body, gold.Body)
				}
			}
		}
	}
}
func TestPhase3D1D2EveryBlurAndReplayRetainsVersions(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	p := f.Gold.Projects[0]
	id := string(memory.NewID())
	h.command(t, f.Context, map[string]any{"type": "createDoc", "doc": workspace.Doc{ID: id, ThingID: p.ID, Title: "虚构连续失焦", Body: "原文", By: "user"}})
	for i := 0; i < 4; i++ {
		c := map[string]any{"type": "updateDoc", "id": id, "patch": map[string]string{"body": fmt.Sprintf("虚构第%d次失焦正文", i)}, "requestId": string(memory.NewID())}
		h.command(t, f.Context, c)
		h.command(t, f.Context, c)
	}
	var list phase3VersionList
	h.get(t, f.Context, phase3DocumentPath(id, "versions"), &list)
	if len(list.Items) != 5 {
		t.Fatalf("five writes/four replays: versions=%d", len(list.Items))
	}
}
func TestPhase3D3T3ParagraphDiffExactAndArbitraryPair(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	doc := f.Gold.Projects[0].Documents[0]
	var diff phase3Diff
	h.get(t, f.Context, phase3DocumentPath(doc.ID, "diff?from=1&to=2"), &diff)
	want := map[string][2]string{"change": {"预算：100虚构单位。", "预算：200虚构单位。"}, "delete": {"删除段：旧运输安排。", ""}, "add": {"", "新增段：改用青湾仓库。"}}
	if diff.FromVersion != 1 || diff.ToVersion != 2 {
		t.Fatal("wrong pair")
	}
	if len(diff.Changes) != 3 {
		t.Fatalf("diff=%+v", diff.Changes)
	}
	for _, ch := range diff.Changes {
		gold, ok := want[ch.Kind]
		if !ok || [2]string{ch.Before, ch.After} != gold {
			t.Fatalf("unplanted edit %+v", ch)
		}
		delete(want, ch.Kind)
	}
	if len(want) != 0 {
		t.Fatal("missing changes", want)
	}
	var defaultDiff phase3Diff
	h.get(t, f.Context, phase3DocumentPath(doc.ID, "diff"), &defaultDiff)
	if !reflect.DeepEqual(diff, defaultDiff) {
		t.Fatal("default is not latest vs previous")
	}
	var identity phase3Diff
	h.get(t, f.Context, phase3DocumentPath(doc.ID, "diff?from=1&to=1"), &identity)
	if len(identity.Changes) != 0 {
		t.Fatal("same version diff is not empty")
	}
}

func TestPhase3D1ConcurrentUserWritesRetainBothVersions(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	id := string(memory.NewID())
	h.command(t, f.Context, map[string]any{"type": "createDoc", "doc": workspace.Doc{ID: id, ThingID: f.Gold.Projects[0].ID, Title: "虚构并发文档", Body: "虚构初始正文", By: "user"}})
	start := make(chan struct{})
	result := make(chan error, 2)
	for _, body := range []string{"虚构并发写入甲", "虚构并发写入乙"} {
		go func(body string) {
			<-start
			code, raw, err := h.call(f.Context, "POST", "/v1/workspace/commands", map[string]any{"type": "updateDoc", "id": id, "patch": map[string]string{"body": body}, "requestId": string(memory.NewID())})
			if err == nil && code != 200 {
				err = fmt.Errorf("concurrent write status=%d body=%s", code, raw)
			}
			result <- err
		}(body)
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	var list phase3VersionList
	h.get(t, f.Context, phase3DocumentPath(id, "versions"), &list)
	if len(list.Items) != 3 {
		t.Fatal("concurrent writes lost a version", list)
	}
	seen := map[string]bool{}
	for _, version := range list.Items {
		var body phase3Version
		h.get(t, f.Context, phase3DocumentPath(id, fmt.Sprintf("versions/%d", version.Version)), &body)
		seen[body.Body] = true
	}
	for _, text := range []string{"虚构初始正文", "虚构并发写入甲", "虚构并发写入乙"} {
		if !seen[text] {
			t.Fatal("concurrent history lost body", text)
		}
	}
}
