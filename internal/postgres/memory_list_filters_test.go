package postgres

import (
	"context"
	"net/url"
	"testing"
	"unicode"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestMemoryListFiltersQualifiedCaptureSQLMatchesGo(t *testing.T) {
	s := testStore(t)
	texts := []string{"虚构人物云杉喜欢松林。", "MAYBE virtual cedar", "somewhere perhaps", "certainly not-sure", "I\tthink it works", "if_only", "gift", "saidness", "unqualified", "我不确定虚构计划", "He COULD go", "no\nlonger", "İF virtual", "maybe青松", "青松if", "if\U0001f600cedar", "if½cedar", "if\u0301cedar"}
	texts = append(texts, qualifiedCaptureMarkers...)
	texts = append(texts, qualifiedCaptureWords...)
	// Range boundaries exercise the actual Unicode tokenizer, including Chinese,
	// non-ASCII numbers and astral letters without relying on PostgreSQL locale.
	for _, table := range []*unicode.RangeTable{unicode.L, unicode.N} {
		for _, r := range table.R16 {
			texts = append(texts, "if"+string(rune(r.Lo))+"cedar", "if"+string(rune(r.Hi))+"cedar")
		}
		for _, r := range table.R32 {
			texts = append(texts, "if"+string(rune(r.Lo))+"cedar", "if"+string(rune(r.Hi))+"cedar")
		}
	}
	rows, err := s.pool.Query(context.Background(), "SELECT input,"+qualifiedCaptureSQL("input")+" FROM unnest($1::text[]) input", texts)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var text string
		var got bool
		if err := rows.Scan(&text, &got); err != nil {
			t.Fatal(err)
		}
		if want := qualifiedCapture(text); got != want {
			t.Errorf("SQL=%v Go=%v for %q", got, want, text)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryListFiltersTrustHTTPPaginationAndConjunction(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{}`)
	// Put non-matching memories between matching rows: filtering after LIMIT
	// would create short/missing pages and a wrong total.
	specs := []struct {
		text, acquisition, role string
		groups                  []string
		trust                   string
	}{
		{"虚构松林甲", "direct", "user", []string{"a"}, "stated"},
		{"虚构松林乙可能改期", "direct", "user", []string{"a", "b"}, "tentative"},
		{"虚构松林丙", "direct", "user", []string{"a", "b"}, "repeated"},
		{"虚构松林丁 maybe 改期", "reported", "user", []string{"a", "b"}, "reported"},
		{"虚构松林戊可能改期", "reported", "assistant", []string{"a", "b"}, "inferred"},
		{"虚构松林己", "direct", "user", []string{"same", "same"}, "stated"},
		{"虚构松林庚", "inferred", "user", []string{"a"}, "inferred"},
		{"虚构松林辛", "direct", "system", []string{"a"}, "inferred"},
		{"虚构松林壬", "direct", "tool", []string{"a"}, "inferred"},
		{"虚构松林癸", "direct", "user", []string{"a"}, "stated"},
	}
	wants := map[string]map[string]bool{}
	refs := []memory.Ref{}
	for _, spec := range specs {
		sources := []memory.Ref{}
		for _, group := range spec.groups {
			src := b1Source(t, s, scope, "虚构筛选资料", spec.text, "archive")
			sources = append(sources, src)
			if _, err := s.pool.Exec(context.Background(), "INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,branch) VALUES($1,$2,1,$3,$4,'active')", string(scope.OwnerID), string(src.ID), group, spec.role); err != nil {
				t.Fatal(err)
			}
		}
		ref := b1Claim(t, s, scope, spec.text, "fact", "candidate", sources...)
		refs = append(refs, ref)
		if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET acquisition=$3,category='rule' WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(ref.ID), spec.acquisition); err != nil {
			t.Fatal(err)
		}
		if wants[spec.trust] == nil {
			wants[spec.trust] = map[string]bool{}
		}
		wants[spec.trust][string(ref.ID)] = true
	}
	for trust, want := range wants {
		seen := map[string]bool{}
		cursor := ""
		for round := 0; round <= len(specs); round++ {
			query := "?trust=" + trust + "&limit=1&category=rule&nature=fact&q=" + url.QueryEscape("虚构松林")
			if cursor != "" {
				query += "&cursor=" + url.QueryEscape(cursor)
			}
			page := b2List(t, s, scope, query)
			if page.Total != len(want) || len(page.Items) != 1 {
				t.Fatal(trust, page)
			}
			item := page.Items[0]
			if !want[item.ID] || seen[item.ID] || item.Trust != trust {
				t.Fatal(trust, item)
			}
			seen[item.ID] = true
			if page.Next == "" {
				break
			}
			if page.Next == cursor {
				t.Fatal("nonadvancing cursor")
			}
			cursor = page.Next
		}
		if len(seen) != len(want) {
			t.Fatal(trust, seen, want)
		}
	}
	if page := b2List(t, s, scope, "?trust=stated&category=goal"); page.Total != 0 || len(page.Items) != 0 {
		t.Fatal("filters must intersect", page)
	}
	// Source role changes must affect the filter immediately; no stored trust.
	if _, err := s.pool.Exec(context.Background(), "UPDATE source_contexts SET role='assistant' WHERE owner_id=$1 AND source_id IN(SELECT source_id FROM evidence WHERE owner_id=$1 AND target_id=$2)", string(scope.OwnerID), string(refs[0].ID)); err != nil {
		t.Fatal(err)
	}
	if page := b2List(t, s, scope, "?trust=stated"); page.Total != 2 {
		t.Fatal("trust was cached", page)
	}
	if page := b2List(t, s, scope, "?trust=inferred&q="+url.QueryEscape("虚构松林甲")); page.Total != 1 || page.Items[0].ID != string(refs[0].ID) {
		t.Fatal(page)
	}
	// Current version changes also affect trust immediately.
	b1Correct(t, s, scope, refs[9], "虚构松林癸可能改期")
	if page := b2List(t, s, scope, "?trust=tentative&q="+url.QueryEscape("虚构松林癸")); page.Total != 1 || page.Items[0].Version != 2 {
		t.Fatal(page)
	}
}

func TestMemoryListFiltersRetiredByAndTrust(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{}`)
	refs := compareFixture(t, s, scope, "虚构保留甲", "虚构退出甲一", "虚构退出甲二", "虚构退出甲三可能改期", "虚构保留乙", "虚构退出乙")
	for _, spec := range []struct {
		i, target int
		kind      string
	}{{1, 0, "duplicate"}, {2, 0, "superseded"}, {3, 0, "duplicate"}, {5, 4, "duplicate"}} {
		if _, err := s.pool.Exec(context.Background(), "UPDATE claims SET retired=$3,retired_by=$4,retired_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(refs[spec.i].ID), spec.kind, string(refs[spec.target].ID)); err != nil {
			t.Fatal(err)
		}
	}
	query := "?retired=1&retiredBy=" + string(refs[0].ID) + "&trust=stated&limit=1"
	first := b2List(t, s, scope, query)
	if first.Total != 2 || len(first.Items) != 1 || first.Next == "" {
		t.Fatal(first)
	}
	second := b2List(t, s, scope, query+"&cursor="+url.QueryEscape(first.Next))
	if second.Total != 2 || len(second.Items) != 1 || second.Next != "" || first.Items[0].ID == second.Items[0].ID {
		t.Fatal(second)
	}
	for _, m := range append(first.Items, second.Items...) {
		if m.RetiredBy != string(refs[0].ID) || m.Trust != "stated" || m.Retired == "" {
			t.Fatal(m)
		}
	}
	if page := b2List(t, s, scope, "?retired=1&retiredBy="+string(refs[0].ID)); page.Total != 3 {
		t.Fatal(page)
	}
	if page := b2List(t, s, scope, "?retired=1&retiredBy="+string(refs[4].ID)); page.Total != 1 {
		t.Fatal(page)
	}
	if page := b2List(t, s, scope, "?retired=1&retiredBy="+string(memory.NewID())); page.Total != 0 || len(page.Items) != 0 {
		t.Fatal(page)
	}
	// IDs from another owner cannot select their history.
	if page := b2List(t, s, memory.Scope{OwnerID: memory.NewID(), PrincipalID: "fictional-other-owner", IsOwner: true}, "?retired=1&retiredBy="+string(refs[0].ID)); page.Total != 0 {
		t.Fatal(page)
	}
	for _, query := range []string{"?trust=unknown", "?trust=STATED", "?retired=1&retiredBy=invalid", "?retiredBy=" + string(refs[0].ID)} {
		if w := b1HTTP(t, s, scope, "GET", "/v1/workspace/memories"+query, nil); w.Code != 400 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	if w := b1HTTP(t, s, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}, "GET", "/v1/workspace/memories?trust=stated", nil); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := s.ListMemories(context.Background(), scope, workspace.MemoryQuery{Trust: "invalid"}); err == nil {
		t.Fatal("direct call accepted bad trust")
	}
}
