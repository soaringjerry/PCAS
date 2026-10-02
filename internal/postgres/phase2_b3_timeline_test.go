package postgres

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2B3_K1_SaidChronologyEventAndMentions(t *testing.T) {
	s, scope := b1Store(t), owner()
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	wang := b3Entity(t, s, scope, "person", "老王", "老王")
	from := b3Year(now, -1, time.June, 1)
	to := from.AddDate(0, 1, 0)
	old := b3Year(now, -1, time.March, 1)
	newer := b3Year(now, -1, time.May, 1)
	refs := []memory.Ref{}
	for i, at := range []time.Time{old, newer} {
		// Source expression dates intentionally differ from claim expression dates.
		src := b3Source(t, s, scope, fmt.Sprintf("成都原始记录%d", i), now)
		refs = append(refs, b3Claim(t, s, scope, b3ClaimSpec{Text: fmt.Sprintf("时间轴计划%d", i), Subject: self, Said: &at, Source: src, EventFrom: &from, EventTo: &to, Precision: "month", Mentions: []b3Mention{{place, "place"}, {wang, "person"}}}))
	}
	b1Model(t, s, b3Used(refs...))
	out := mustTurn(t, s, scope, turnRequest("去年说去成都要干什么来着"))
	items := b3Timeline(t, out)
	if len(items) != 2 {
		t.Fatalf("timeline count=%d want 2", len(items))
	}
	for i, at := range []time.Time{old, newer} {
		b3Date(t, items[i]["at"], at)
		b3MustRef(t, items[i]["memoryId"], refs[i])
		b3Date(t, items[i]["eventFrom"], from)
		b3Date(t, items[i]["eventTo"], to)
		if items[i]["eventPrecision"] != "month" {
			t.Errorf("precision=%v", items[i]["eventPrecision"])
		}
		var mentions []struct{ EntityID, Name, Role string }
		if err := json.Unmarshal(asJSON(items[i]["mentions"]), &mentions); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, m := range mentions {
			got[m.Name] = m.Role + ":" + m.EntityID
		}
		if len(got) != 2 || got["成都"] != "place:"+string(place) || got["老王"] != "person:"+string(wang) {
			t.Errorf("mentions=%v", got)
		}
	}
}

// Create genuine desk actions and derive a synthetic claim from that exact
// turn's original, preserving its real creation receipt for the association.
func b3TurnPlan(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, self, place memory.ID, now time.Time, n int, label string) (memory.Ref, []string, memory.Ref) {
	t.Helper()
	actions := []map[string]any{}
	for i := 0; i < n; i++ {
		actions = append(actions, map[string]any{"op": "create_task", "title": fmt.Sprintf("%s事项%d", label, i)})
	}
	f.set(string(asJSON(map[string]any{"reply": "安排好了。", "actions": actions})))
	req := turnRequest("成都安排原话" + label)
	out := mustTurn(t, s, scope, req)
	src := b1TurnSource(t, s, scope, req.RequestID)
	ids := []string{}
	for _, r := range out.Turn.Receipts {
		if r.Op == "create_task" && r.Status == "done" && r.ThingID != nil {
			ids = append(ids, *r.ThingID)
		}
	}
	if len(ids) != n {
		t.Fatalf("real turn created %d items, want %d: %+v", len(ids), n, out.Turn.Receipts)
	}
	said := b3Year(now, -1, time.March, 1)
	b3Exec(t, s, `UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, src.ID, said)
	ref := b3Claim(t, s, scope, b3ClaimSpec{Text: label + "计划", Subject: self, Said: &said, Source: src, Mentions: []b3Mention{{place, "place"}}})
	return ref, ids, src
}

func TestPhase2B3_K2_CompletedAndCancelledTurnItems(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	done, ids, _ := b3TurnPlan(t, s, scope, f, self, place, now, 1, "做完")
	workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: ids[0], Status: "done"})
	dropped, ids, _ := b3TurnPlan(t, s, scope, f, self, place, now, 1, "取消")
	workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: ids[0], Status: "cancelled"})
	f.set(b3Used(done, dropped))
	out := mustTurn(t, s, scope, turnRequest("去年说去成都的计划来着"))
	for text, status := range map[string]string{"做完计划": "done", "取消计划": "dropped"} {
		item := b3TimelineText(t, out, text)
		if item["status"] != status {
			t.Errorf("%s status=%v want %s", text, item["status"], status)
		}
	}
}

func TestPhase2B3_K3_CorrectionAndChangeUseCurrentText(t *testing.T) {
	for _, change := range []string{"correction", "change"} {
		t.Run(change, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			now := b3Zone(t, s, scope, "Asia/Shanghai")
			self := b3Entity(t, s, scope, "self", "本人")
			place := b3Entity(t, s, scope, "place", "成都", "成都")
			at := b3Year(now, -1, time.March, 1)
			ref := b3Claim(t, s, scope, b3ClaimSpec{Text: "以前的安排", Subject: self, Said: &at, Mentions: []b3Mention{{place, "place"}}})
			text := "更改后的打算：改去音乐厅"
			replacement := memory.Claim{SubjectID: self, Predicate: "验收事项", Value: asJSON(text), Nature: "plan", Acquisition: "direct", Confirmation: "confirmed"}
			updated, err := s.Correct(t.Context(), scope, memory.CorrectRequest{ChangeType: change, Target: ref, Replacement: replacement, Reason: "验收更改"})
			if err != nil {
				t.Fatal(err)
			}
			// Structured columns are fixtures for the replacement version as well.
			b3Exec(t, s, `UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2 AND version=$4`, scope.OwnerID, updated.ID, at, updated.Version)
			b3Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,'place') ON CONFLICT DO NOTHING`, scope.OwnerID, updated.ID, updated.Version, place)
			b1Model(t, s, b3Used(updated))
			out := mustTurn(t, s, scope, turnRequest("去年说去成都有什么计划来着"))
			item := b3TimelineText(t, out, text)
			if item["status"] != "changed" {
				t.Errorf("status=%v want changed", item["status"])
			}
			b3MustRef(t, item["memoryId"], updated)
			for _, other := range b3Timeline(t, out) {
				if other["text"] == "以前的安排" {
					t.Error("superseded text entered current timeline")
				}
			}
		})
	}
}

func TestPhase2B3_K4_SingleRecallGetsTimelineOnlyWithSaidDate(t *testing.T) {
	s, scope := b1Store(t), owner()
	now := b3Zone(t, s, scope, "Australia/Sydney")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	at := b3Year(now, -1, time.March, 1)
	ref := b3Claim(t, s, scope, b3ClaimSpec{Text: "单条回忆计划", Subject: self, Said: &at, Mentions: []b3Mention{{place, "place"}}})
	b1Model(t, s, b3Used(ref))
	recall := mustTurn(t, s, scope, turnRequest("成都的计划来着"))
	if len(b3Timeline(t, recall)) != 1 {
		t.Error("one dated recalled memory must emit timeline")
	}
	normal := mustTurn(t, s, scope, turnRequest("成都有什么计划"))
	if len(b3Timeline(t, normal)) != 0 {
		t.Error("one memory without recall must not emit timeline")
	}
	b3Exec(t, s, `UPDATE record_versions SET expressed_at=NULL WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, ref.ID)
	undated := mustTurn(t, s, scope, turnRequest("成都的计划来着"))
	if len(b3Timeline(t, undated)) != 0 {
		t.Error("one undated recalled memory must not emit timeline")
	}
}

func TestPhase2B3_K5_UnknownSaidTimeLastAndEmpty(t *testing.T) {
	s, scope := b1Store(t), owner()
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	old := b3Year(now, -1, time.January, 1)
	newer := b3Year(now, -1, time.June, 1)
	var fallback struct {
		K5 struct {
			Connector, Title, SourceText string
			RecordedDaysAgo              int
		}
	}
	if err := json.Unmarshal(b3Gold(t)["timelineSaidFallback"], &fallback); err != nil {
		t.Fatal(err)
	}
	if fallback.K5.Connector == "" || fallback.K5.SourceText == "" || fallback.K5.RecordedDaysAgo == 0 {
		t.Fatal("missing frozen K5 import fixture")
	}
	imported := mustIngest(t, s, scope, memory.IngestRequest{Connector: fallback.K5.Connector, ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: fallback.K5.Title, Text: fallback.K5.SourceText, MediaType: "text/plain"}).Ref
	importedAt := now.AddDate(0, 0, -fallback.K5.RecordedDaysAgo)
	b3Exec(t, s, `UPDATE record_versions SET expressed_at=NULL,actor='import',recorded_at=$3 WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, imported.ID, importedAt)
	refs := []memory.Ref{}
	for i, at := range []*time.Time{nil, &newer, &old} {
		spec := b3ClaimSpec{Text: fmt.Sprintf("未知排序计划%d", i), Subject: self, Said: at, Mentions: []b3Mention{{place, "place"}}}
		if i == 0 {
			spec.Source = imported
		}
		refs = append(refs, b3Claim(t, s, scope, spec))
	}
	b1Model(t, s, b3Used(refs...))
	out := mustTurn(t, s, scope, turnRequest("成都的计划来着"))
	items := b3Timeline(t, out)
	if len(items) != 3 {
		t.Fatalf("timeline count=%d", len(items))
	}
	b3MustRef(t, items[0]["memoryId"], refs[2])
	b3MustRef(t, items[1]["memoryId"], refs[1])
	b3MustRef(t, items[2]["memoryId"], refs[0])
	if items[2]["at"] != nil && items[2]["at"] != "" {
		t.Errorf("unknown at=%v must be empty", items[2]["at"])
	}
}

func TestPhase2B3_K6_AllItemsDetermineStatus(t *testing.T) {
	for _, statuses := range [][]string{{"done", "todo"}, {"done", "cancelled"}, {"done", "done"}, {"cancelled", "cancelled"}} {
		t.Run(fmt.Sprint(statuses), func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, b3Used())
			now := b3Zone(t, s, scope, "Asia/Shanghai")
			self := b3Entity(t, s, scope, "self", "本人")
			place := b3Entity(t, s, scope, "place", "成都", "成都")
			ref, ids, _ := b3TurnPlan(t, s, scope, f, self, place, now, 2, "混合")
			for i, status := range statuses {
				if status != "todo" {
					workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: ids[i], Status: status})
				}
			}
			f.set(b3Used(ref))
			out := mustTurn(t, s, scope, turnRequest("成都的计划来着"))
			want := "open"
			if statuses[0] == "done" && statuses[1] == "done" {
				want = "done"
			}
			if statuses[0] == "cancelled" && statuses[1] == "cancelled" {
				want = "dropped"
			}
			if got := b3TimelineText(t, out, "混合计划")["status"]; got != want {
				t.Errorf("status=%v want %s", got, want)
			}
		})
	}
}

func TestPhase2B3_K7_RawOriginalStaysInEvidenceList(t *testing.T) {
	s, scope := b1Store(t), owner()
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	at := b3Year(now, -1, time.March, 1)
	text := "成都原话独立暗号：当时想去天府广场。"
	source := b3Source(t, s, scope, text, at)
	ref := b3Claim(t, s, scope, b3ClaimSpec{Text: "时间轴的打算", Subject: self, Said: &at, Source: source, Mentions: []b3Mention{{place, "place"}}})
	f := b1Model(t, s, `{"reply":"有一条打算和原话。","used":["M1","S1"],"actions":[]}`)
	out := mustTurn(t, s, scope, turnRequest("成都的计划来着"))
	b1Contains(t, f.last(t).Prompt, text)
	items := b3Timeline(t, out)
	if len(items) != 1 {
		t.Fatalf("timeline count=%d want 1", len(items))
	}
	b3MustRef(t, items[0]["memoryId"], ref)
	found := false
	for _, card := range out.Turn.Cards {
		if card.Kind == "sources" {
			var sources []workspace.DeskSourceItem
			if err := json.Unmarshal(asJSON(card.Items), &sources); err != nil {
				t.Fatal(err)
			}
			for _, item := range sources {
				if item.Kind == "source" && item.SourceID == string(source.ID) && item.Text == text {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("cited original missing from separate evidence list")
	}
}

func TestPhase2B3_K8_UnrelatedItemFromSameOriginalDoesNotChangeStatus(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	ref, ids, source := b3TurnPlan(t, s, scope, f, self, place, now, 1, "真正关联")
	workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: ids[0], Status: "done"})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "另一次建的不相干事项"})
	other := ""
	for _, task := range st.Tasks {
		if task.ID != ids[0] {
			other = task.ID
		}
	}
	if other == "" {
		t.Fatal("missing unrelated item")
	}
	refs := []workspace.SourceRef{{SourceID: string(source.ID), Version: source.Version}}
	// Synthetic association with the same original, outside the secretary turn's
	// action log; the real creation action belongs to the workspace command.
	b3Exec(t, s, `UPDATE work_items SET document=jsonb_set(document,'{sources}',$3::jsonb) WHERE owner_id=$1 AND id=$2`, scope.OwnerID, other, string(asJSON(refs)))
	for _, status := range []string{"todo", "cancelled"} {
		workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: other, Status: status})
		f.set(b3Used(ref))
		out := mustTurn(t, s, scope, turnRequest("成都的计划来着"))
		item := b3TimelineText(t, out, "真正关联计划")
		if item["status"] != "done" {
			t.Errorf("unrelated %s item changed associated done status to %v", status, item["status"])
		}
	}
}

func TestPhase2B3_K11_LegacySecretaryMemoryUsesOriginalRecordedTime(t *testing.T) {
	var fallback struct {
		K11 struct {
			SourceText, MemoryText, DatedMemoryText, Query string
			SourceRecordedDaysAgo, DatedMemoryDaysAgo      int
			TimelineOrder                                  []string
		}
	}
	if err := json.Unmarshal(b3Gold(t)["timelineSaidFallback"], &fallback); err != nil {
		t.Fatal(err)
	}
	gold := fallback.K11
	if gold.SourceText == "" || gold.SourceRecordedDaysAgo == 0 || len(gold.TimelineOrder) != 2 || gold.TimelineOrder[0] != "legacy" || gold.TimelineOrder[1] != "dated" {
		t.Fatal("missing frozen K11 secretary fallback oracle")
	}
	for _, zone := range []string{"Australia/Sydney", "Asia/Shanghai"} {
		t.Run(zone, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, b3Used())
			b3Zone(t, s, scope, zone)
			self := b3Entity(t, s, scope, "self", "本人")
			place := b3Entity(t, s, scope, "place", "成都", "成都")
			// Establish a genuine user-entered secretary original, without extraction.
			req := turnRequest(gold.SourceText)
			mustTurn(t, s, scope, req)
			source := b1TurnSource(t, s, scope, req.RequestID)
			recorded := testsupport.DateFromToday(t, zone, -gold.SourceRecordedDaysAgo, 12, 0)
			dated := testsupport.DateFromToday(t, zone, -gold.DatedMemoryDaysAgo, 12, 0)
			b3Exec(t, s, `UPDATE record_versions SET expressed_at=NULL,recorded_at=$3 WHERE owner_id=$1 AND record_id=$2 AND version=$4`, scope.OwnerID, source.ID, recorded, source.Version)
			legacy := b3Claim(t, s, scope, b3ClaimSpec{Text: gold.MemoryText, Nature: "fact", Subject: self, Source: source, Mentions: []b3Mention{{place, "place"}}})
			known := b3Claim(t, s, scope, b3ClaimSpec{Text: gold.DatedMemoryText, Nature: "fact", Subject: self, Said: &dated, Mentions: []b3Mention{{place, "place"}}})
			var legacyUndated, sourceUndated bool
			var savedRecorded time.Time
			if err := s.pool.QueryRow(t.Context(), `SELECT c.expressed_at IS NULL,v.expressed_at IS NULL,v.recorded_at FROM record_versions c JOIN record_versions v ON v.owner_id=c.owner_id AND v.record_id=$3 AND v.version=$4 WHERE c.owner_id=$1 AND c.record_id=$2 AND c.version=$5`, scope.OwnerID, legacy.ID, source.ID, source.Version, legacy.Version).Scan(&legacyUndated, &sourceUndated, &savedRecorded); err != nil {
				t.Fatal(err)
			}
			if !legacyUndated || !sourceUndated || !savedRecorded.Equal(recorded) {
				t.Fatal("legacy/source dates do not match the recorded-time-only fixture")
			}
			f.set(b3Used(legacy, known))
			recall := turnRequest(gold.Query)
			out := mustTurn(t, s, scope, recall)
			b1Contains(t, f.last(t).Prompt, gold.MemoryText, gold.DatedMemoryText)
			for _, ref := range []memory.Ref{legacy, known} {
				b1HasRef(t, b1Refs(t, s, scope, recall.RequestID), ref, true)
			}
			items := b3Timeline(t, out)
			if len(items) != 2 {
				t.Fatalf("timeline count=%d want 2", len(items))
			}
			b3MustRef(t, items[0]["memoryId"], legacy)
			b3Date(t, items[0]["at"], recorded)
			b3MustRef(t, items[1]["memoryId"], known)
			b3Date(t, items[1]["at"], dated)
		})
	}
}
