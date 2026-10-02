package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2B2_X1_SelfMentionsExpressionAndEvent(t *testing.T) {
	for _, zone := range b2Gold(t).Time.Zones {
		t.Run(zone, func(t *testing.T) {
			s, scope := testStore(t), owner()
			b2Zone(t, s, scope, zone)
			f := b1Model(t, s, `{"reply":"收到","actions":[]}`)
			req := turnRequest(b2Fixture(t, "trip", "text"))
			mustTurn(t, s, scope, req)
			src := b1TurnSource(t, s, scope, req.RequestID)
			at := b2Anchor(t, zone)
			b2Exec(t, s, `UPDATE record_versions SET expressed_at=$1 WHERE owner_id=$2 AND record_id=$3 AND version=$4`, at, string(scope.OwnerID), string(src.ID), src.Version)
			m := b2One(t, b2Extract(t, s, scope, f, src, b2TripItem(t, zone)))
			_, typ := b2Subject(t, s, scope, m)
			b2Equal(t, typ, b2Want[string](t, "X1", "subject_type"))
			b2Names(t, m, "person", b2Want[[]string](t, "X1", "people"))
			b2Names(t, m, "place", b2Want[[]string](t, "X1", "places"))
			b2Time(t, m, "expressedAt", &at)
			from, to := b2Interval(t, zone, "range")
			b2Event(t, m, &from, &to, "range")
			b2Confirmation(t, s, scope, m, b2Want[string](t, "X1", "confirmation"))
			b2ExtractionRecord(t, s, scope, src, "done", 1)
			b1Contains(t, f.last(t).Raw, zone, at.Format("2006-01-02"))
			b2Equal(t, b2Count(t, s, `SELECT count(*) FROM record_versions WHERE owner_id=$1 AND record_id=$2 AND (valid_from IS NOT NULL OR valid_to IS NOT NULL)`, string(scope.OwnerID), m.ID), 0)
		})
	}
}
func TestPhase2B2_X2_EntitiesSharedAcrossSourcesAndOwnersIsolated(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b1Model(t, s, nil)
	zone := "Asia/Shanghai"
	b2Zone(t, s, scope, zone)
	at := b2Anchor(t, zone)
	var first workspace.Memory
	for n := 0; n < 2; n++ {
		text := b2Fixture(t, "trip", "text") + strings.Repeat("。", n)
		src := b2Source(t, s, scope, "desk", "user", text, &at)
		items := b2Extract(t, s, scope, f, src, b2TripItem(t, zone))
		m := items[len(items)-1]
		for _, x := range items {
			for _, r := range x.Sources {
				if r.SourceID == string(src.ID) {
					m = x
				}
			}
		}
		if n == 0 {
			first = m
			continue
		}
		a, _ := b2Subject(t, s, scope, first)
		b, _ := b2Subject(t, s, scope, m)
		b2Equal(t, a == b, b2Want[bool](t, "X2", "same_self"))
		b2Equal(t, b2MentionID(t, first, "老王", "person") == b2MentionID(t, m, "老王", "person"), b2Want[bool](t, "X2", "same_person"))
		b2Equal(t, b2MentionID(t, first, "成都", "place") == b2MentionID(t, m, "成都", "place"), b2Want[bool](t, "X2", "same_place"))
	}
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND entity_type='self'`, string(scope.OwnerID)), 1)
	other := owner()
	b2Zone(t, s, other, zone)
	src := b2Source(t, s, other, "desk", "user", b2Fixture(t, "trip", "text"), &at)
	m := b2One(t, b2Extract(t, s, other, f, src, b2TripItem(t, zone)))
	if b2MentionID(t, first, "老王", "person") == b2MentionID(t, m, "老王", "person") {
		t.Error("entity identity crossed owners")
	}
}
func TestPhase2B2_X3_ExactAliasTrimCaseAndType(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b1Model(t, s, nil)
	at := b2Anchor(t, "Asia/Shanghai")
	ids := []string{}
	for _, spec := range []struct{ name, role string }{{" Alice ", "person"}, {"alice", "person"}, {"alice", "place"}, {"Alicia", "person"}} {
		text := "我见过" + strings.TrimSpace(spec.name)
		src := b2Source(t, s, scope, "capture", "user", text, &at)
		i := b2Item(text, text, "fact")
		key := "people"
		if spec.role == "place" {
			key = "places"
		}
		i[key] = []string{spec.name}
		items := b2Extract(t, s, scope, f, src, i)
		var m workspace.Memory
		for _, x := range items {
			if x.Text == text {
				for _, r := range x.Sources {
					if r.SourceID == string(src.ID) {
						m = x
					}
				}
			}
		}
		for _, x := range b2Mentions(t, m) {
			if x.Role == spec.role {
				ids = append(ids, x.EntityID)
			}
		}
	}
	b2Equal(t, len(ids), 4)
	if len(ids) != 4 {
		return
	}
	b2Equal(t, ids[0] == ids[1], b2Want[bool](t, "X3", "trim_case_same"))
	b2Equal(t, ids[1] == ids[2], b2Want[bool](t, "X3", "cross_type_same"))
	if ids[1] == ids[3] {
		t.Error("fuzzy names merged")
	}
}
func TestPhase2B2_X4_HallucinatedNameDroppedWithoutDroppingMemory(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b1Model(t, s, nil)
	at := b2Anchor(t, "Asia/Shanghai")
	text := "我去成都"
	src := b2Source(t, s, scope, "capture", "user", text, &at)
	i := b2Item(text, text, "plan")
	i["people"] = []string{"不存在的赵六"}
	i["places"] = []string{"成都"}
	items := b2Extract(t, s, scope, f, src, i)
	b2Equal(t, len(items), b2Want[int](t, "X4", "memories"))
	m := b2One(t, items)
	b2Names(t, m, "person", b2Want[[]string](t, "X4", "people"))
	b2Names(t, m, "place", []string{"成都"})
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND name='不存在的赵六'`, string(scope.OwnerID)), 0)
}
func TestPhase2B2_X5_UnknownExpressionDoesNotResolveRelativeDate(t *testing.T) {
	for _, zone := range b2Gold(t).Time.Zones {
		t.Run(zone, func(t *testing.T) {
			s, scope := testStore(t), owner()
			b2Zone(t, s, scope, zone)
			f := b1Model(t, s, nil)
			src := b2Source(t, s, scope, "archive", "user", b2Fixture(t, "import_unknown", ""), nil)
			from, to := b2Interval(t, zone, "range")
			relative := b2Item("下周交稿", "下周交稿", "plan")
			relative["when"] = b2When(from, to, "range", "下周")
			absolute := b2Item("2025 年 3 月去过大理", "2025 年 3 月去过大理", "fact")
			absolute["places"] = []string{"大理"}
			absolute["when"] = map[string]any{"from": b2Want[string](t, "X5", "absolute_from"), "to": b2Want[string](t, "X5", "absolute_to"), "precision": "month", "quote": "2025 年 3 月"}
			items := b2Extract(t, s, scope, f, src, relative, absolute)
			b2Equal(t, len(items), 2)
			loc, _ := time.LoadLocation(zone)
			a := time.Date(2025, 3, 1, 0, 0, 0, 0, loc)
			b := a.AddDate(0, 1, 0)
			for _, m := range items {
				b2Time(t, m, "expressedAt", nil)
				if m.Text == relative["text"] {
					b2Event(t, m, nil, nil, "")
				} else {
					b2Event(t, m, &a, &b, b2Want[string](t, "X5", "precision"))
				}
			}
		})
	}
}
func TestPhase2B2_X6_InvalidWhenKeepsClaim(t *testing.T) {
	invalid := []map[string]any{{"from": "2025-04-02", "to": "2025-04-01", "precision": "range", "quote": "四月"}, {"from": "2000-01-01", "to": "2020-01-01", "precision": "range", "quote": "四月"}, {"from": "不是日期", "to": "2025-04-02", "precision": "day", "quote": "四月"}, {"from": "2025-04-01", "to": "2025-04-02", "precision": "day", "quote": "原文里没有"}}
	for n, label := range b2Want[[]string](t, "X6", "invalid") {
		t.Run(label, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b1Model(t, s, nil)
			text := "我四月去成都"
			src := b2Source(t, s, scope, "capture", "user", text, nil)
			i := b2Item(text, text, "plan")
			i["when"] = invalid[n]
			i["places"] = []string{"成都"}
			items := b2Extract(t, s, scope, f, src, i)
			b2Equal(t, len(items), b2Want[int](t, "X6", "memories_each"))
			m := b2One(t, items)
			b2Event(t, m, nil, nil, "")
			b2Names(t, m, "place", []string{"成都"})
		})
	}
}
func TestPhase2B2_X7_ConfirmationBoundary(t *testing.T) {
	for _, tc := range []struct {
		key, connector, role, text, acq string
		confidence                      float64
	}{{"direct", "desk", "user", "我下周去成都", "direct", .8}, {"qualified", "desk", "user", "我可能下周去成都", "direct", 1}, {"imported", "archive", "user", "我下周去成都", "direct", 1}, {"reported", "desk", "user", "老王说他要去成都", "reported", 1}, {"assistant", "capture", "assistant", "我下周去成都", "direct", 1}, {"low_confidence", "desk", "user", "我下周去成都", "direct", .79}, {"inferred", "desk", "user", "我下周去成都", "inferred", 1}} {
		t.Run(tc.key, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b1Model(t, s, nil)
			at := b2Anchor(t, "Asia/Shanghai")
			src := b2Source(t, s, scope, tc.connector, tc.role, tc.text, &at)
			i := b2Item("计划去成都", tc.text, "plan")
			i["acquisition"], i["confidence"] = tc.acq, tc.confidence
			if tc.key == "reported" {
				i["subject"] = "老王"
			}
			m := b2One(t, b2Extract(t, s, scope, f, src, i))
			b2Confirmation(t, s, scope, m, b2Want[string](t, "X7", tc.key))
		})
	}
}
func TestPhase2B2_X8_QuestionAndOperationProduceNoMemory(t *testing.T) {
	for _, tc := range []struct{ key, fixture string }{{"question_memories", "question"}, {"operation_memories", "operation"}} {
		t.Run(tc.key, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b1Model(t, s, nil)
			src := b2Source(t, s, scope, "desk", "user", b2Fixture(t, tc.fixture, ""), nil)
			items := b2Extract(t, s, scope, f, src)
			b2Equal(t, len(items), b2Want[int](t, "X8", tc.key))
			b2ExtractionRecord(t, s, scope, src, "empty", 0)
		})
	}
}
func TestPhase2B2_X9_SecretaryTaskAlsoRetainsPlan(t *testing.T) {
	s, scope := testStore(t), owner()
	zone := "Asia/Shanghai"
	b2Zone(t, s, scope, zone)
	f := b1Model(t, s, b1CreatePlan)
	text := b2Fixture(t, "plan", "text")
	req := turnRequest(text)
	out := mustTurn(t, s, scope, req)
	b2Equal(t, len(out.State.Tasks), 1)
	src := b1TurnSource(t, s, scope, req.RequestID)
	at := b2Anchor(t, zone)
	b2Exec(t, s, `UPDATE record_versions SET expressed_at=$1 WHERE owner_id=$2 AND record_id=$3`, at, string(scope.OwnerID), string(src.ID))
	from, to := b2Interval(t, zone, "day")
	i := b2Item(text, text, "plan")
	i["people"] = []string{"张三"}
	i["when"] = b2When(from, to, "day", "周五")
	task := map[string]any{"kind": "task", "text": text, "quote": text, "explicit": true, "confidence": 1, "acquisition": "direct"}
	items := b2Extract(t, s, scope, f, src, i, task)
	b2Equal(t, len(items), b2Want[int](t, "X9", "memories"))
	m := b2One(t, items)
	b2Equal(t, m.Kind, b2Want[string](t, "X9", "nature"))
	b2Names(t, m, "person", b2Want[[]string](t, "X9", "people"))
	b2Event(t, m, &from, &to, b2Want[string](t, "X9", "precision"))
	st := b2Snapshot(t, s, scope)
	b2Equal(t, len(st.Candidates), b2Want[int](t, "X9", "candidates"))
	b2Equal(t, len(st.Tasks), 1)
}
func TestPhase2B2_X10_ReprocessingEnrichesWithoutRevisionOrOutdated(t *testing.T) {
	s, scope := testStore(t), owner()
	zone := "Asia/Shanghai"
	b2Zone(t, s, scope, zone)
	f := b1Model(t, s, nil)
	at := b2Anchor(t, zone)
	text := b2Fixture(t, "trip", "text")
	src := b2Source(t, s, scope, "capture", "user", text, &at)
	i := b2Item(text, text, "plan")
	m := b2One(t, b2Extract(t, s, scope, f, src, i))
	b2Exec(t, s, `UPDATE record_versions SET expressed_at=NULL WHERE owner_id=$1 AND record_id=$2`, string(scope.OwnerID), m.ID)
	f.set(`{"reply":"保留成都计划回答","used":["M1"],"actions":[]}`)
	req := turnRequest("成都老王有什么安排")
	out := mustTurn(t, s, scope, req)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), b2Ref(m), true)
	enriched := b2TripItem(t, zone)
	after := b2One(t, b2Extract(t, s, scope, f, src, enriched))
	b2Equal(t, after.ID, m.ID)
	b2Equal(t, after.Version, b2Want[int](t, "X10", "version"))
	b2Names(t, after, "person", []string{"老王"})
	from, to := b2Interval(t, zone, "range")
	b2Event(t, after, &from, &to, "range")
	b2Time(t, after, "expressedAt", &at)
	b1Outdated(t, b1History(t, s, scope, out.ConversationID), b2Want[bool](t, "X10", "outdated"))
	b2Equal(t, b1History(t, s, scope, out.ConversationID).Reply, out.Turn.Reply)
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2`, string(scope.OwnerID), m.ID), 1)
}
func TestPhase2B2_X11_UserEditedOrConfirmedMemoryNotEnriched(t *testing.T) {
	for _, operation := range []string{"editMemory", "confirmMemory"} {
		t.Run(operation, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b1Model(t, s, nil)
			at := b2Anchor(t, "Asia/Shanghai")
			text := b2Fixture(t, "trip", "text")
			src := b2Source(t, s, scope, "capture", "user", text, &at)
			m := b2One(t, b2Extract(t, s, scope, f, src, b2Item(text, text, "plan")))
			workspaceCommand(t, s, scope, workspace.Command{Type: operation, ID: m.ID, Text: "用户独立修订成都安排", Reason: "验收合成修订"})
			before := b2One(t, b2Snapshot(t, s, scope).Memories)
			after := b2Extract(t, s, scope, f, src, b2TripItem(t, "Asia/Shanghai"))
			b2Equal(t, len(after), b2Want[int](t, "X11", "memories"))
			b2Equal(t, b1Map(t, b2One(t, after)), b1Map(t, before))
			b2Event(t, b2One(t, after), nil, nil, "")
			b2Names(t, b2One(t, after), "person", []string{})
		})
	}
}
func TestPhase2B2_X12_FullUndoBlocksPlanPartialUndoDoesNot(t *testing.T) {
	TestPhase2B1_N2_ExtractionAfterFullUndoDoesNotRecreatePlan(t)
	TestPhase2B1_N3_PartialUndoKeepsMemoryAndRawSupply(t)
}
func TestPhase2B2_X13_ImportedHistoricalTimeAndNoTodayTask(t *testing.T) {
	s, scope := testStore(t), owner()
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	b2Zone(t, s, scope, "Asia/Shanghai")
	f := b1Model(t, s, nil)
	at := b2ParseTime(t, b2Want[string](t, "X13", "expressed"))
	_, src, _ := b2Archive(t, s, scope, at, b2Fixture(t, "archive_user", ""), "")
	i := b2Item(b2Fixture(t, "archive_user", ""), b2Fixture(t, "archive_user", ""), "plan")
	from := b2ParseTime(t, b2Want[string](t, "X13", "event_from"))
	to := b2ParseTime(t, b2Want[string](t, "X13", "event_to"))
	i["when"] = b2When(from, to, "day", "明天")
	m := b2One(t, b2Extract(t, s, scope, f, src, i))
	b2Time(t, m, "expressedAt", &at)
	b2Event(t, m, &from, &to, "day")
	b2Confirmation(t, s, scope, m, b2Want[string](t, "X13", "confirmation"))
	st := b2Snapshot(t, s, scope)
	b2Equal(t, len(st.Tasks), 0)
	b2Equal(t, len(st.Candidates), b2Want[int](t, "X13", "candidates"))
}
func TestPhase2B2_X14_AllLongSourceSegmentsAndOverlapDeduplicated(t *testing.T) {
	s, scope := testStore(t), owner()
	var spec struct {
		Segment     int    `json:"segment_characters"`
		Step        int    `json:"step_characters"`
		KeyStart    int    `json:"key_start_character"`
		Length      int    `json:"source_characters"`
		Requests    int    `json:"requests"`
		Items       int    `json:"items"`
		KeyMemories int    `json:"key_memories"`
		First       string `json:"first_only"`
		Second      string `json:"second_only"`
	}
	b2SupplementFrom(t, "supplement_32ccb70", "X14", &spec)
	key := b2Fixture(t, "long_key", "")
	filler := []rune(strings.Repeat("合成无关资料没有出行信息。\n", spec.Length))[:spec.Length]
	for pos, marker := range map[int]string{100: spec.First, spec.KeyStart: key, 13000: spec.Second} {
		copy(filler[pos:], []rune(marker))
	}
	text := string(filler)
	at := b2Anchor(t, "Asia/Shanghai")
	src := b2Source(t, s, scope, "capture", "user", text, &at)
	replies := []map[string]any{b2Item(spec.First, spec.First, "preference"), b2Item(key, key, "plan"), b2Item(spec.Second, spec.Second, "decision")}
	var mu sync.Mutex
	requests := []string{}
	extractionTestModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var in struct {
			Messages []struct{ Role, Content string }
		}
		if err = json.Unmarshal(raw, &in); err != nil {
			t.Error(err)
			return
		}
		prompt := ""
		for _, m := range in.Messages {
			if m.Role != "system" {
				prompt += m.Content
			}
		}
		mu.Lock()
		requests = append(requests, prompt)
		mu.Unlock()
		// No finished processing record may be visible while any segment is in flight.
		var premature int
		if err = s.pool.QueryRow(context.Background(), `SELECT count(*) FROM source_extractions WHERE owner_id=$1 AND source_id=$2 AND state IN ('done','empty')`, string(scope.OwnerID), string(src.ID)).Scan(&premature); err != nil {
			t.Error(err)
		}
		if premature != 0 {
			t.Error("source marked finished before all segments completed")
		}
		items := []map[string]any{}
		for _, item := range replies {
			if strings.Contains(prompt, item["quote"].(string)) {
				items = append(items, item)
			}
		}
		secretaryModelReply(w, map[string]any{"items": items})
	})
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, src, "source.extract")); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < spec.Requests; n++ {
		var stage string
		err := s.pool.QueryRow(context.Background(), `SELECT stage FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage LIKE 'source.extract:%' AND state='queued' ORDER BY stage LIMIT 1`, string(scope.OwnerID), string(src.ID)).Scan(&stage)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, src, stage)); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	captured := append([]string{}, requests...)
	mu.Unlock()
	b2Equal(t, len(captured), spec.Requests)
	keyRequests := 0
	for n, prompt := range captured {
		if strings.Contains(prompt, key) {
			keyRequests++
		}
		offset := n * spec.Step
		end := min(offset+spec.Segment, len(filler))
		b1Contains(t, prompt, string(filler[offset:end]))
		if n == 0 {
			b1Contains(t, prompt, spec.First)
			b1Absent(t, prompt, spec.Second)
		} else {
			b1Contains(t, prompt, spec.Second)
			b1Absent(t, prompt, spec.First)
		}
	}
	b2Equal(t, keyRequests, spec.Requests)
	memories := b2Snapshot(t, s, scope).Memories
	b2Equal(t, len(memories), spec.Items)
	keys := 0
	for _, m := range memories {
		if m.Text == key {
			keys++
		}
	}
	b2Equal(t, keys, spec.KeyMemories)
	b2ExtractionRecord(t, s, scope, src, b2Want[string](t, "X14", "state"), spec.Items)
}
func TestPhase2B2_X15_EmptyExtractionStillSuppliesRawText(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b1Model(t, s, nil)
	src := b2Source(t, s, scope, "desk", "user", b2Fixture(t, "empty", "text"), nil)
	b2Equal(t, len(b2Extract(t, s, scope, f, src)), 0)
	b2ExtractionRecord(t, s, scope, src, b2Want[string](t, "X15", "state"), b2Want[int](t, "X15", "items"))
	f.set(`{"reply":"青色灯塔7319","used":["S1"],"actions":[]}`)
	mustTurn(t, s, scope, turnRequest("成都柜子的暗号是什么"))
	b1Contains(t, f.last(t).Prompt, b2Fixture(t, "empty", "text"))
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), 0)
}
func TestPhase2B2_X16_DeleteOrReplaceDuringActualModelCall(t *testing.T) {
	for _, kind := range b2Want[[]string](t, "X16", "cases") {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			reply := b2TripItem(t, "Asia/Shanghai")
			extractionTestModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				secretaryModelReply(w, map[string]any{"items": []any{reply}})
			})
			t.Cleanup(unblock)
			at := b2Anchor(t, "Asia/Shanghai")
			src := b2Source(t, s, scope, "capture", "user", b2Fixture(t, "trip", "text"), &at)
			job := leaseStage(t, s, scope, src, "source.extract")
			done := make(chan error, 1)
			go func() { done <- s.ProcessExtraction(context.Background(), job) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("model request never started")
			}
			if kind == "delete_during_call" {
				b1Delete(t, s, scope, src)
			} else {
				var ext string
				if err := s.pool.QueryRow(context.Background(), `SELECT external_id FROM sources WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(src.ID)).Scan(&ext); err != nil {
					t.Fatal(err)
				}
				mustIngest(t, s, scope, memory.IngestRequest{Connector: "capture", ExternalID: ext, ExternalVersion: "2", Title: "秘书原话", Text: "新版没有旅行安排", MediaType: "text/plain"})
			}
			unblock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("extraction failed to finish")
			}
			b2Equal(t, b2Count(t, s, `SELECT count(*) FROM claims WHERE owner_id=$1`, string(scope.OwnerID)), b2Want[int](t, "X16", "memories"))
			b2Equal(t, b2Count(t, s, `SELECT count(*) FROM entities WHERE owner_id=$1`, string(scope.OwnerID)), b2Want[int](t, "X16", "entities"))
		})
	}
}
func TestPhase2B2_X17_MentionLimitsRetainFirstEightValid(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b1Model(t, s, nil)
	expected := b2Want[[]string](t, "X17", "places")
	names := []string{expected[0], expected[1], strings.Repeat("长", 60), expected[2], expected[3], expected[4], expected[5], expected[6], expected[7], "地点九", "地点十", "地点十一"}
	text := "我去过" + strings.Join(names, "、")
	src := b2Source(t, s, scope, "capture", "user", text, nil)
	i := b2Item("去过多个地点", text, "fact")
	i["places"] = names
	i["people"] = []string{"我", "我们"}
	m := b2One(t, b2Extract(t, s, scope, f, src, i))
	b2Names(t, m, "place", expected)
	b2Names(t, m, "person", []string{})
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND entity_type='place'`, string(scope.OwnerID)), b2Want[int](t, "X17", "max_per_role"))
}
func TestPhase2B2_X18_ArchiveAssistantSkippedButAvailableAsNeighborAndRaw(t *testing.T) {
	s, scope := testStore(t), owner()
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	at := b2Anchor(t, "Asia/Shanghai")
	f := b1Model(t, s, map[string]any{"items": []any{b2Item("用户成都计划", b2Fixture(t, "archive_user", ""), "plan")}})
	_, user, assistant := b2Archive(t, s, scope, at, b2Fixture(t, "archive_user", ""), b2Fixture(t, "archive_assistant", ""))
	before := len(f.all())
	if err = s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, assistant, "source.extract")); err != nil {
		t.Fatal(err)
	}
	b2Equal(t, len(f.all())-before, b2Want[int](t, "X18", "assistant_calls"))
	b2ExtractionRecord(t, s, scope, assistant, b2Want[string](t, "X18", "assistant_state"), 0)
	b2Extract(t, s, scope, f, user, b2Item("用户成都计划", b2Fixture(t, "archive_user", ""), "plan"))
	b1Contains(t, f.last(t).Prompt, b2Fixture(t, "archive_assistant", ""))
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), 1)
	f.set(`{"reply":"紫色灯塔4821","used":["S1"],"actions":[]}`)
	mustTurn(t, s, scope, turnRequest("封面暗号紫色灯塔是什么"))
	b1Contains(t, f.last(t).Prompt, b2Fixture(t, "archive_assistant", ""))
}

func TestPhase2B2_X1_ExpressionFallbackIsLimitedToHandEnteredSources(t *testing.T) {
	for _, connector := range []string{"desk", "capture", "telegram", "desk-incomplete", "archive", "webhook"} {
		t.Run(connector, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b1Model(t, s, nil)
			text := "我偏好清晨整理资料"
			src := b2Source(t, s, scope, connector, "user", text, nil)
			var recorded time.Time
			if err := s.pool.QueryRow(context.Background(), `SELECT recorded_at FROM record_versions WHERE owner_id=$1 AND record_id=$2 AND version=1`, string(scope.OwnerID), string(src.ID)).Scan(&recorded); err != nil {
				t.Fatal(err)
			}
			m := b2One(t, b2Extract(t, s, scope, f, src, b2Item(text, text, "preference")))
			if connector == "archive" || connector == "webhook" {
				b2Time(t, m, "expressedAt", nil)
			} else {
				b2Time(t, m, "expressedAt", &recorded)
			}
		})
	}
}
