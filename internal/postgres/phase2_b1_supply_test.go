package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2B1_P1_DeskTurnRawSourceAndDeletionControl(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"收到。","used":[],"actions":[]}`)
	// The original is spoken through the real secretary entry, with extraction absent.
	originalReq := turnRequest(b1Text(t, "trip", "text"))
	original := mustTurn(t, s, scope, originalReq)
	source := b1TurnSource(t, s, scope, originalReq.RequestID)
	// The frozen trip fixture has a known expression time; DeskTurn accepts no
	// timestamp field, so attach this synthetic metadata before the recall step.
	if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET expressed_at=$1::timestamptz WHERE owner_id=$2 AND record_id=$3 AND version=$4", b1Text(t, "trip", "expressed_at"), string(scope.OwnerID), string(source.ID), source.Version); err != nil {
		t.Fatal(err)
	}
	b1ZeroClaims(t, s, scope)
	f.set(`{"reply":"春熙路火锅，见老王。","used":["S1","S1","S999"],"actions":[]}`)
	req := turnRequest("我去成都想吃什么")
	out := mustTurn(t, s, scope, req)
	if out.ConversationID == original.ConversationID {
		t.Error("fixture did not switch conversation")
	}
	actual := f.last(t)
	b1Contains(t, actual.Prompt, "相关原话", b1Text(t, "trip", "text"), "[S1 / 秘书原话 /", "说于", "用户")
	b1Contains(t, actual.System, "M*", "S*", "used")
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, true)
	found := 0
	for _, card := range out.Turn.Cards {
		if card.Kind == "timeline" {
			b1Absent(t, string(asJSON(card)), string(source.ID))
		}
		if card.Kind != "sources" {
			continue
		}
		var items []map[string]any
		if err := json.Unmarshal(asJSON(card.Items), &items); err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item["kind"] == "source" {
				found++
				if item["memoryId"] != string(source.ID) || item["sourceId"] != string(source.ID) || item["version"] != float64(source.Version) || item["sourceVersion"] != float64(source.Version) || item["text"] != b1Text(t, "trip", "text") || item["at"] == nil {
					t.Errorf("wrong source card: %v", item)
				}
				w := b1HTTP(t, s, scope, "GET", "/v1/memory/sources/"+string(source.ID), nil)
				if w.Code != 200 {
					t.Error("source card cannot open", w.Code)
				}
				b1Contains(t, w.Body.String(), "春熙路", "火锅", "老王")
			}
		}
	}
	if found != 1 {
		t.Errorf("want one deduplicated source card, got %d", found)
	}
	b1Delete(t, s, scope, source)
	f.set(`{"reply":"删除后对照","used":[],"actions":[]}`)
	mustTurn(t, s, scope, turnRequest("我去成都想吃什么"))
	b1Absent(t, f.last(t).Prompt, b1Text(t, "trip", "text"))
}

func TestPhase2B1_P2_ActualDeputyRequestAndSourceDependencies(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, "副手固定结果")
	source := b1Trip(t, s, scope)
	b1ZeroClaims(t, s, scope)
	run := b1Run(t, s, scope, "model", "我去成都想吃什么")
	b1Contains(t, f.last(t).Prompt, b1Text(t, "trip", "text"), "相关原话：", b1SourceLine("source", source), "说于 2026-09-12")
	b1HasRef(t, run.ContextVersions, source, true)
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM run_dependencies WHERE owner_id=$1 AND run_id=$2 AND memory_id=$3 AND memory_version=$4", string(scope.OwnerID), run.ID, string(source.ID), source.Version).Scan(&n); err != nil || n != 1 {
		t.Error("run_dependencies missing source", n, err)
	}
	b1Delete(t, s, scope, source)
	b1Run(t, s, scope, "model", "我去成都想吃什么")
	b1Absent(t, f.last(t).Prompt, b1Text(t, "trip", "text"))
}
func TestPhase2B1_P3_ManualBriefAndDeletionControl(t *testing.T) {
	s, scope := b1Store(t), owner()
	b1Model(t, s, "不会调用")
	source := b1Trip(t, s, scope)
	b1ZeroClaims(t, s, scope)
	run := b1Run(t, s, scope, "manual", "我去成都想吃什么")
	b1Contains(t, run.Brief, b1Text(t, "trip", "text"), "相关原话：", b1SourceLine("source", source), "说于 2026-09-12")
	b1HasRef(t, run.ContextVersions, source, true)
	b1Delete(t, s, scope, source)
	b1Absent(t, b1Run(t, s, scope, "manual", "我去成都想吃什么").Brief, b1Text(t, "trip", "text"))
}
func TestPhase2B1_P4_ClaimOnlyEmptySourceSection(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"陈述回答","used":["M1"],"actions":[]}`)
	src := b1Source(t, s, scope, "快速记录", "成都陈述测试", "memory-input")
	claim := b1Claim(t, s, scope, "成都独立陈述火锅细节", "fact", "adopted", src)
	req := turnRequest("成都陈述有什么")
	out := mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, "成都独立陈述火锅细节", "相关原话", "（没有）")
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), claim, true)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), src, false)
	for _, c := range out.Turn.Cards {
		if c.Kind == "sources" {
			b1Contains(t, string(asJSON(c.Items)), `"kind":"claim"`)
			b1Absent(t, string(asJSON(c.Items)), `"kind":"source"`)
		}
	}
}
func TestPhase2B1_P5_SystemGeneratedSourcesExcluded(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"正常","actions":[]}`)
	b1Trip(t, s, scope)
	for _, kind := range []string{"actions", "corrections", "memory-input"} {
		b1Source(t, s, scope, "秘书原话", b1Text(t, "system_sources", kind), kind)
	}
	mustTurn(t, s, scope, turnRequest("成都旅行系统动作纠正记忆"))
	prompt := f.last(t).Prompt
	b1Contains(t, prompt, b1Text(t, "trip", "text"))
	for _, kind := range []string{"actions", "corrections", "memory-input"} {
		b1Absent(t, prompt, b1Text(t, "system_sources", kind))
	}
	run := b1Run(t, s, scope, "model", "成都旅行系统动作纠正记忆")
	b1Contains(t, run.Brief, b1Text(t, "trip", "text"))
	for _, kind := range []string{"actions", "corrections", "memory-input"} {
		b1Absent(t, f.last(t).Prompt, b1Text(t, "system_sources", kind))
	}
}

var b1SourceRows = regexp.MustCompile(`(?m)^\[(S\d+ /[^\]]+|source:[^\]]+)\] ([^\n]*)`)

func b1Budget(t *testing.T, text string, maxSegments, maxChars int) []string {
	t.Helper()
	lines := b1SourceRows.FindAllStringSubmatch(text, -1)
	if len(lines) == 0 || len(lines) > maxSegments {
		t.Errorf("source excerpts count %d, want 1..%d", len(lines), maxSegments)
	}
	total := 0
	aliases := map[string]bool{}
	excerpts := []string{}
	for _, line := range lines {
		n := utf8.RuneCountInString(line[2])
		total += n
		if n > 600 {
			t.Errorf("excerpt %s has %d characters", line[1], n)
		}
		if aliases[line[1]] {
			t.Error("duplicate excerpt alias", line[1])
		}
		aliases[line[1]] = true
		excerpts = append(excerpts, line[2])
	}
	if total > maxChars {
		t.Errorf("source budget %d > %d", total, maxChars)
	}
	return excerpts
}
func TestPhase2B1_P6_BudgetsTruncateAndComplete(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"正常完成","actions":[]}`)
	for i := 0; i < 20; i++ {
		prefix := fmt.Sprintf("成都旅行长资料%02d", i)
		text := prefix + strings.Repeat("旅", 2000-utf8.RuneCountInString(prefix)-utf8.RuneCountInString("不应收到的全文尾部")) + "不应收到的全文尾部"
		if utf8.RuneCountInString(text) != 2000 {
			t.Fatal("bad fixture length")
		}
		b1Source(t, s, scope, "快速记录", text, "manual")
	}
	b1ZeroClaims(t, s, scope)
	// Actual ranked Recall is an observation of retrieval order; only limits and
	// selected-excerpt policy come from gold, never from context assembly.
	ranked, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "成都旅行长资料", Mode: memory.Remember, Budget: memory.Budget{Candidates: 100, Tokens: 10000}})
	if err != nil {
		t.Fatal(err)
	}
	req := turnRequest("成都旅行长资料")
	out := mustTurn(t, s, scope, req)
	if out.Turn.Reply != "正常完成" {
		t.Error("budget interrupted secretary")
	}
	for _, r := range out.Turn.Receipts {
		if r.Status == "skipped" || r.Reason != "" {
			t.Error("budget error receipt", r)
		}
	}
	excerpts := b1Budget(t, f.last(t).Prompt, 6, 2400)
	for _, e := range excerpts {
		if !strings.HasSuffix(e, "…") {
			t.Error("truncated excerpt lacks ellipsis")
		}
	}
	b1Absent(t, f.last(t).Prompt, "不应收到的全文尾部")
	secretaryRefs := b1Refs(t, s, scope, req.RequestID)
	selected := []memory.Ref{}
	for _, r := range secretaryRefs {
		if r.Kind == memory.SourceKind {
			selected = append(selected, r)
		}
	}
	if len(selected) != len(excerpts) {
		t.Errorf("dependencies %d != supplied excerpts %d", len(selected), len(excerpts))
	}
	expected := []memory.Ref{}
	for _, ref := range ranked.Memories {
		if ref.Kind == memory.SourceKind {
			expected = append(expected, ref)
			if len(expected) == len(selected) {
				break
			}
		}
	}
	for _, ref := range expected {
		b1HasRef(t, selected, ref, true)
	}

	f.set("副手正常完成")
	b1Run(t, s, scope, "model", "成都旅行长资料")
	b1Budget(t, f.last(t).Prompt, 8, 4000)
	b1Absent(t, f.last(t).Prompt, "不应收到的全文尾部")
	manual := b1Run(t, s, scope, "manual", "成都旅行长资料")
	b1Budget(t, manual.Brief, 8, 4000)
	b1Absent(t, manual.Brief, "不应收到的全文尾部")
}
func TestPhase2B1_P7_HistoricalQuestionsNotRepeatedAsSources(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"收到","actions":[]}`)
	firstText := b1Text(t, "trip", "text")
	firstReq := turnRequest(firstText)
	first := mustTurn(t, s, scope, firstReq)
	firstSource := b1TurnSource(t, s, scope, firstReq.RequestID)
	second := turnRequest("顺便查天气")
	second.ConversationID = &first.ConversationID
	mustTurn(t, s, scope, second)
	third := turnRequest("我去成都想吃什么")
	third.ConversationID = &first.ConversationID
	mustTurn(t, s, scope, third)
	if n := strings.Count(f.last(t).Prompt, firstText); n != 1 {
		t.Errorf("historical question occurs %d times, want once", n)
	}
	for _, row := range b1SourceRows.FindAllStringSubmatch(f.last(t).Prompt, -1) {
		b1Absent(t, row[2], firstText)
	}
	// Positive control: the exclusion is local to this conversation.
	fresh := turnRequest("我去成都想吃什么")
	mustTurn(t, s, scope, fresh)
	found := false
	for _, row := range b1SourceRows.FindAllStringSubmatch(f.last(t).Prompt, -1) {
		if strings.Contains(row[2], firstText) {
			found = true
		}
	}
	if !found {
		t.Error("new conversation did not receive the historical source")
	}
	b1HasRef(t, b1Refs(t, s, scope, fresh.RequestID), firstSource, true)

}
func TestPhase2B1_P8_DuplicateTitlesDoNotMergeIdentities(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"同名资料","used":["S1","S2","S3","S4"],"actions":[]}`)
	refs := map[string]memory.Ref{}
	for i := 0; i < 18; i++ {
		text := fmt.Sprintf("成都旅行备忘第%02d份：在地点%02d吃特色菜%02d，联系朋友%02d b1needle%02d", i, i, i, i, i)
		title := "秘书原话"
		if i%2 == 1 {
			title = "快速记录"
		}
		refs[text] = b1Source(t, s, scope, title, text, "manual")
	}
	b1ZeroClaims(t, s, scope)
	// Each query isolates one of the many identically titled records.
	for i := 0; i < 18; i++ {
		text := fmt.Sprintf("成都旅行备忘第%02d份：在地点%02d吃特色菜%02d，联系朋友%02d b1needle%02d", i, i, i, i, i)
		req := turnRequest(fmt.Sprintf("b1needle%02d", i))
		mustTurn(t, s, scope, req)
		b1Contains(t, f.last(t).Prompt, text)
		b1HasRef(t, b1Refs(t, s, scope, req.RequestID), refs[text], true)
	}
	run := b1Run(t, s, scope, "manual", "成都旅行备忘")
	lines := b1SourceRows.FindAllStringSubmatch(run.Brief, -1)
	if len(lines) < 2 {
		t.Error("duplicate-title records were collapsed")
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if seen[line[1]] {
			t.Error("sources confused by title")
		}
		seen[line[1]] = true
	}
}
func TestPhase2B1_P9_PublicReadsCannotForgeInternalAccess(t *testing.T) {
	s, scope := b1Store(t), owner()
	b1Model(t, s, `{"reply":"ok","actions":[]}`)
	source := b1Trip(t, s, scope)
	outsider := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}
	for _, who := range []memory.Scope{scope, outsider} {
		recall := b1HTTP(t, s, who, "POST", "/v1/memory/recall", memory.RecallRequest{Query: "成都火锅老王", Mode: memory.Remember})
		if recall.Code != 200 {
			t.Error(recall.Code, recall.Body.String())
		}
		expanded := b1HTTP(t, s, who, "POST", "/v1/memory/expand", memory.ExpandRequest{Refs: []memory.Ref{source}, Evidence: true})
		original := b1HTTP(t, s, who, "GET", "/v1/memory/sources/"+string(source.ID), nil)
		if who.IsOwner {
			b1Contains(t, recall.Body.String(), "火锅")
			if original.Code != 200 || expanded.Code != 200 {
				t.Error("owner denied")
			}
		} else {
			b1Absent(t, recall.Body.String(), "火锅", string(source.ID))
			b1Absent(t, expanded.Body.String(), "火锅")
			if original.Code == 200 {
				t.Error("non-owner read ungranted source")
			}
		}
	}
	// Exercise the server-only marker without guessing its name.
	typ := reflect.TypeOf(scope)
	markers := 0
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Bool || field.Name == "IsOwner" {
			continue
		}
		markers++
		if field.Tag.Get("json") != "-" {
			t.Errorf("internal scope marker %s is JSON-decodable", field.Name)
		}
		forged := outsider
		if err := json.Unmarshal(asJSON(map[string]any{field.Name: true}), &forged); err != nil {
			t.Fatal(err)
		}
		if reflect.ValueOf(forged).Field(i).Bool() {
			t.Error("forged internal access via JSON")
		}
	}
	if markers != 1 {
		t.Errorf("want one internal bool marker, found %d", markers)
	}
}
func TestPhase2B1_P10_DeputyClaimFiltersAndSharedRawSources(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, "副手结果")
	source := b1Trip(t, s, scope)
	system := b1Source(t, s, scope, "快速记录", "成都类别过滤夹具", "memory-input")
	hidden := b1Claim(t, s, scope, "成都隐藏偏好不吃香菜", "preference", "adopted", system)
	allowed := b1Claim(t, s, scope, "成都可见事实旅程", "fact", "adopted", system)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "model", Patch: asJSON(map[string]any{"memoryKinds": []string{"fact"}, "includeInferred": false})})
	run := b1Run(t, s, scope, "model", "成都旅程偏好")
	b1Contains(t, f.last(t).Prompt, b1Text(t, "trip", "text"), "成都可见事实旅程")
	b1Absent(t, f.last(t).Prompt, "成都隐藏偏好不吃香菜")
	b1HasRef(t, run.ContextVersions, source, true)
	b1HasRef(t, run.ContextVersions, allowed, true)
	b1HasRef(t, run.ContextVersions, hidden, false)
}
func TestPhase2B1_P11_CurrentVersionOnly(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"版本回答","used":["S1"],"actions":[]}`)
	in := memory.IngestRequest{Connector: "manual", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: b1Text(t, "versioned", "old"), MediaType: "text/plain"}
	old := mustIngest(t, s, scope, in)
	in.ExternalVersion = "2"
	in.Text = b1Text(t, "versioned", "current")
	current := mustIngest(t, s, scope, in)
	req := turnRequest("成都版本吃什么")
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, in.Text)
	b1Absent(t, f.last(t).Prompt, b1Text(t, "versioned", "old"))
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), current.Ref, true)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), old.Ref, false)
	f.set("副手结果")
	run := b1Run(t, s, scope, "model", "成都版本吃什么")
	b1Contains(t, f.last(t).Prompt, in.Text)
	b1Absent(t, f.last(t).Prompt, b1Text(t, "versioned", "old"))
	b1HasRef(t, run.ContextVersions, current.Ref, true)
}
func TestPhase2B1_P12_MediaRequiresReadableTranscriptOrOCR(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, `{"reply":"媒体回答","actions":[]}`)
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	for _, kind := range []string{"audio/ogg", "image/png"} {
		original, err := s.IngestAttachment(context.Background(), scope, memory.IngestRequest{Connector: "upload", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "成都无文本媒体", MediaType: kind}, strings.NewReader("synthetic-media-bytes"))
		if err != nil {
			t.Fatal(err)
		}
		text, repr := b1Text(t, "media", "transcript"), "transcript"
		if kind == "image/png" {
			text = b1Text(t, "media", "ocr")
			repr = "ocr"
		}
		parsed := b1Source(t, s, scope, "快速记录", text, "attachment-text")
		if _, err := s.pool.Exec(context.Background(), "UPDATE source_versions SET representation=$4,derived_from_id=$3,derived_from_version=1 WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(parsed.ID), string(original.ID), repr); err != nil {
			t.Fatal(err)
		}
		req := turnRequest("成都青羊宫武侯祠媒体内容")
		mustTurn(t, s, scope, req)
		b1Contains(t, f.last(t).Prompt, text)
		b1Absent(t, f.last(t).Prompt, "synthetic-media-bytes", "成都无文本媒体")
		b1HasRef(t, b1Refs(t, s, scope, req.RequestID), parsed, true)
		b1HasRef(t, b1Refs(t, s, scope, req.RequestID), original.Ref, false)
	}
}

func TestPhase2B1_P13_AdoptedSourceContentSurvivesForUserButExpiresForModels(t *testing.T) {
	s, scope := b1Store(t), owner()
	marker := "采纳的成都方案独特内容"
	f := b1Model(t, s, "- [ ] "+marker)
	second := s.models.Config.Providers[0]
	second.ID = "model2"
	second.Name = "另一个副手"
	s.models.Config.Providers = append(s.models.Config.Providers, second)
	source := b1Trip(t, s, scope)
	run := b1Run(t, s, scope, "model", "成都吃什么")
	b1HasRef(t, run.ContextVersions, source, true)
	adopted, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if run.Adopted == nil {
		t.Fatal("deputy result was not adopted into item")
	}
	originalChecklist := ""
	for _, task := range adopted.Tasks {
		if task.ID == run.ThingID {
			originalChecklist = string(asJSON(task.Checklist))
		}
	}
	b1Contains(t, originalChecklist, marker)
	secretary := func(want bool) {
		f.set(`{"reply":"秘书分析","actions":[]}`)
		req := turnRequest("处理这件事")
		req.ThingID = &run.ThingID
		mustTurn(t, s, scope, req)
		if want {
			b1Contains(t, f.last(t).Prompt, marker)
		} else {
			b1Absent(t, f.last(t).Prompt, marker)
		}
	}
	deputy := func(want bool) {
		f.set("另一个副手当前结果")
		workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: run.ThingID, AgentID: "model2", Kind: "ask", Prompt: "处理这件事"})
		if err := s.runAgentOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if want {
			b1Contains(t, f.last(t).Prompt, marker)
		} else {
			b1Absent(t, f.last(t).Prompt, marker)
		}
	}
	secretary(true)
	deputy(true)
	var external string
	if err := s.pool.QueryRow(context.Background(), "SELECT external_id FROM sources WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(source.ID)).Scan(&external); err != nil {
		t.Fatal(err)
	}
	mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: external, ExternalVersion: "2", Title: "秘书原话", Text: "成都新的原话资料宽窄巷子兔头", MediaType: "text/plain"})
	secretary(false)
	deputy(false)
	after, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range after.Tasks {
		if task.ID == run.ThingID {
			found = true
			if string(asJSON(task.Checklist)) != originalChecklist {
				t.Error("stale source erased user-visible adopted checklist")
			}
		}
	}
	if !found {
		t.Error("adopted item deleted")
	}
}

func TestPhase2B1_P6_MatchedTailExcerptBeforeAndAfterChunking(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprintf("chunked_%v", chunked), func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, `{"reply":"尾段回答","actions":[]}`)
			marker := "成都交付暗号青色灯塔7319"
			source := b1Source(t, s, scope, "秘书原话", strings.Repeat("无关背景材料。", 5000)+marker, "manual")
			if chunked {
				if err := s.ProcessChunks(context.Background(), leaseStage(t, s, scope, source, "source.chunk")); err != nil {
					t.Fatal(err)
				}
			}
			req := turnRequest("青色灯塔7319")
			mustTurn(t, s, scope, req)
			b1Contains(t, f.last(t).Prompt, marker)
			b1Absent(t, f.last(t).Prompt, strings.Repeat("无关背景材料。", 1000))
			b1Budget(t, f.last(t).Prompt, 6, 2400)
			b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, true)
			f.set("副手尾段回答")
			run := b1Run(t, s, scope, "model", "青色灯塔7319")
			b1Contains(t, f.last(t).Prompt, marker)
			b1Absent(t, f.last(t).Prompt, strings.Repeat("无关背景材料。", 1000))
			b1HasRef(t, run.ContextVersions, source, true)
			manual := b1Run(t, s, scope, "manual", "青色灯塔7319")
			b1Contains(t, manual.Brief, marker)
			b1Budget(t, manual.Brief, 8, 4000)
		})
	}
}
func TestPhase2B1_P10_ExclusionsInferenceAndProjectFiltersStayOnClaimsOnly(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, "副手过滤结果")
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "当前项目"})
	project := st.Projects[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "其他项目"})
	other := ""
	for _, p := range st.Projects {
		if p.ID != project {
			other = p.ID
		}
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "成都事项", ProjectID: project})
	task := st.Tasks[0].ID
	raw := b1Trip(t, s, scope)
	system := b1Source(t, s, scope, "快速记录", "成都过滤证据", "memory-input")
	makeClaim := func(text, projectID, acquisition string) memory.Ref {
		entity := memory.Entity{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.EntityKind}}, Name: "用户", Type: "person"}
		claim := memory.Claim{Revision: memory.Revision{Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}}, SubjectID: entity.ID, Predicate: "成都过滤", Value: asJSON(text), Nature: "fact", Acquisition: acquisition, Confirmation: "adopted", Scope: map[string]json.RawMessage{"project_id": asJSON(projectID)}}
		in := memory.CommitRequest{RequestID: memory.NewID(), Entities: []memory.Entity{entity}, Claims: []memory.Claim{claim}, Evidence: []memory.Evidence{{Source: system, Target: claim.Ref, Acquisition: acquisition, Stance: "supports"}}}
		if _, err := s.Commit(context.Background(), scope, in); err != nil {
			t.Fatal(err)
		}
		workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(claim.ID), AgentIDs: []string{"model", "manual"}})
		return claim.Ref
	}
	allowed := makeClaim("成都当前项目事实", project, "direct")
	excluded := makeClaim("成都被排除陈述", project, "direct")
	foreign := makeClaim("成都其他项目陈述", other, "direct")
	inferred := makeClaim("成都推测陈述", project, "inferred")
	workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: task, MemoryID: string(excluded.ID)})
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "model", Patch: asJSON(map[string]any{"memoryKinds": []string{"fact"}, "includeInferred": false})})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task, AgentID: "model", Kind: "ask", Prompt: "成都当前项目事实"})
	run := st.Runs[0]
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	b1Contains(t, f.last(t).Prompt, "成都当前项目事实", b1Text(t, "trip", "text"))
	b1Absent(t, f.last(t).Prompt, "成都被排除陈述", "成都其他项目陈述", "成都推测陈述")
	b1HasRef(t, run.ContextVersions, allowed, true)
	b1HasRef(t, run.ContextVersions, raw, true)
	for _, ref := range []memory.Ref{excluded, foreign, inferred} {
		b1HasRef(t, run.ContextVersions, ref, false)
	}
}

func b1R2aFixture(t *testing.T, id string) map[string]string {
	t.Helper()
	var supplement struct {
		Fixtures map[string]map[string]string `json:"fixtures"`
	}
	if err := json.Unmarshal(b1Gold(t)["R2a_supplement_2026_10_02"], &supplement); err != nil {
		t.Fatal(err)
	}
	fixture := supplement.Fixtures[id]
	if fixture["raw"] == "" || fixture["claim"] == "" || fixture["query"] == "" {
		t.Fatal("missing frozen R2a fixture", id)
	}
	return fixture
}

func b1R2aVisibility(t *testing.T, s *Store, scope memory.Scope, claim memory.Ref, agents ...string) {
	t.Helper()
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(claim.ID), AgentIDs: agents})
}

func b1R2aRun(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, task, agent, query string, reply any) workspace.Run {
	t.Helper()
	f.set(reply)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task, AgentID: agent, Kind: "ask", Prompt: query})
	run := st.Runs[0]
	if agent == "manual" {
		if run.Status != "waiting" {
			t.Error("manual handoff did not complete", run.Status)
		}
		return run
	}
	before := len(f.all())
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// R4-5/R4-10 now permit selfcheck even before status cards exist.
	// This older retrieval test still requires exactly one answer request.
	answers := 0
	for _, call := range f.all()[before:] {
		if !strings.Contains(call.System, "这是自查") {
			answers++
		}
	}
	if answers != 1 {
		t.Fatal("deputy answer request was not actually sent exactly once")
	}
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, completed := range st.Runs {
		if completed.ID == run.ID {
			if completed.Status != "done" {
				t.Error("deputy failed", completed.Status, completed.Output)
			}
			return completed
		}
	}
	t.Fatal("completed deputy run missing")
	return run
}

func b1R2aSupply(t *testing.T, prompt string, refs []memory.Ref, fixture map[string]string, source, claim memory.Ref, want bool) {
	t.Helper()
	if want {
		b1Contains(t, prompt, fixture["raw"], fixture["claim"])
	} else {
		b1Absent(t, prompt, fixture["raw"], fixture["claim"])
	}
	b1HasRef(t, refs, source, want)
	b1HasRef(t, refs, claim, want)
}

// Review candidates from one original, then adopt through the same command as
// the UI. Conservative agents keep their existing inference settings.
func b1R2aExtractAndAdopt(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, source memory.Ref, items ...map[string]any) []workspace.Memory {
	t.Helper()
	for _, item := range items {
		item["kind"] = "unknown"
	}
	b1Extract(t, s, scope, f, source, items...)
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		candidateID := ""
		for _, candidate := range st.Candidates {
			if candidate.State == "pending" && candidate.Text == item["text"] && candidate.Source.SourceID == string(source.ID) && candidate.Source.Version == source.Version {
				candidateID = candidate.ID
				break
			}
		}
		if candidateID == "" {
			t.Fatal("review candidate missing for adoption", item["text"])
		}
		st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: candidateID, Kind: "memory", MemoryKind: item["nature"].(string), Text: item["text"].(string)})
	}
	return st.Memories
}

func TestPhase2B1_P14_CapturedMemoryVisibilityClosesAndReopensOriginal(t *testing.T) {
	s, scope := b1Store(t), owner()
	fixture := b1R2aFixture(t, "P14")
	f := b1Model(t, s, `{"reply":"R2a原回答固定标记","used":["S1"],"actions":[]}`)
	second := s.models.Config.Providers[0]
	second.ID, second.Name = "model2", "仍然可见的副手"
	s.models.Config.Providers = append(s.models.Config.Providers, second)
	// Capture and adoption use the same public workspace commands as the UI.
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: fixture["raw"]})
	candidate := st.Candidates[0]
	source := memory.Ref{ID: memory.ID(candidate.Source.SourceID), Version: candidate.Source.Version, Kind: memory.SourceKind}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: candidate.ID, Kind: "memory", MemoryKind: "fact", Text: fixture["claim"]})
	claim := b1MemoryRef(t, st.Memories, fixture["claim"])
	b1R2aVisibility(t, s, scope, claim, "model", "model2", "manual")
	originalRequest := turnRequest(fixture["query"])
	original := mustTurn(t, s, scope, originalRequest)
	b1R2aSupply(t, f.last(t).Prompt, b1Refs(t, s, scope, originalRequest.RequestID), fixture, source, claim, true)

	b1R2aVisibility(t, s, scope, claim, "model2", "manual")
	b1Preserved(t, original.Turn, b1History(t, s, scope, original.ConversationID))
	f.set(`{"reply":"当前资料作答","actions":[]}`)
	follow := turnRequest(fixture["query"])
	follow.ConversationID = &original.ConversationID
	mustTurn(t, s, scope, follow)
	b1R2aSupply(t, f.last(t).Prompt, b1Refs(t, s, scope, follow.RequestID), fixture, source, claim, false)
	b1Contains(t, f.last(t).Prompt, original.Turn.Text, "（先前回答的依据已更新，请按现在的资料回答）")
	b1Absent(t, f.last(t).Prompt, original.Turn.Reply)
	// A fresh conversation must also obey the visibility closure.
	fresh := turnRequest(fixture["query"])
	mustTurn(t, s, scope, fresh)
	b1R2aSupply(t, f.last(t).Prompt, b1Refs(t, s, scope, fresh.RequestID), fixture, source, claim, false)
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R2a 可见副手事项"})
	run := b1R2aRun(t, s, scope, f, st.Tasks[0].ID, "model2", fixture["query"], "仍可见的副手固定回答")
	b1R2aSupply(t, f.last(t).Prompt, run.ContextVersions, fixture, source, claim, true)

	b1R2aVisibility(t, s, scope, claim, "model", "model2", "manual")
	f.set(`{"reply":"重新打开后作答","actions":[]}`)
	reopened := turnRequest(fixture["query"])
	mustTurn(t, s, scope, reopened)
	b1R2aSupply(t, f.last(t).Prompt, b1Refs(t, s, scope, reopened.RequestID), fixture, source, claim, true)
}

func TestPhase2B1_P15_ItemExclusionAffectsOnlyThatItemAcrossAllEntrances(t *testing.T) {
	s, scope := b1Store(t), owner()
	fixture := b1R2aFixture(t, "P15")
	f := b1Model(t, s, `{"reply":"准备资料","actions":[]}`)
	source := b1Source(t, s, scope, fixture["title"], fixture["raw"], "manual")
	item := b1ExtractItem(fixture["claim"], "fact")
	item["quote"], item["predicate"] = fixture["raw"], "交接位置"
	otherItem := b1ExtractItem(fixture["other_claim"], "fact")
	otherItem["quote"], otherItem["predicate"] = fixture["raw"], "携带物品"
	memories := b1R2aExtractAndAdopt(t, s, scope, f, source, item, otherItem)
	claim := b1MemoryRef(t, memories, fixture["claim"])
	otherClaim := b1MemoryRef(t, memories, fixture["other_claim"])
	b1R2aVisibility(t, s, scope, claim, "model", "manual")
	b1R2aVisibility(t, s, scope, otherClaim, "model", "manual")
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R2a 事项甲"})
	a := st.Tasks[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R2a 事项乙"})
	b := ""
	for _, task := range st.Tasks {
		if task.ID != a {
			b = task.ID
		}
	}
	if b == "" {
		t.Fatal("second item missing")
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: a, MemoryID: string(claim.ID)})
	for _, target := range []struct {
		id   string
		want bool
	}{{a, false}, {b, true}} {
		for _, agent := range []string{"model", "manual"} {
			run := b1R2aRun(t, s, scope, f, target.id, agent, fixture["query"], "R2a 副手固定回答")
			b1R2aSupply(t, run.Brief, run.ContextVersions, fixture, source, claim, target.want)
			b1Contains(t, run.Brief, fixture["other_claim"])
			b1HasRef(t, run.ContextVersions, otherClaim, true)
			if agent != "manual" {
				b1R2aSupply(t, f.last(t).Prompt, run.ContextVersions, fixture, source, claim, target.want)
				b1Contains(t, f.last(t).Prompt, fixture["other_claim"])
			}
		}
		f.set(`{"reply":"事项页固定回答","actions":[]}`)
		req := turnRequest(fixture["query"])
		req.ThingID = &target.id
		mustTurn(t, s, scope, req)
		b1R2aSupply(t, f.last(t).Prompt, b1Refs(t, s, scope, req.RequestID), fixture, source, claim, target.want)
		b1Contains(t, f.last(t).Prompt, fixture["other_claim"])
		b1HasRef(t, b1Refs(t, s, scope, req.RequestID), otherClaim, true)
	}
	// No concrete item: another item's exclusion must not become global.
	f.set(`{"reply":"大厅固定回答","actions":[]}`)
	hall := turnRequest(fixture["query"])
	mustTurn(t, s, scope, hall)
	b1R2aSupply(t, f.last(t).Prompt, b1Refs(t, s, scope, hall.RequestID), fixture, source, claim, true)
	workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: a, MemoryID: string(claim.ID)})
	reopened := b1R2aRun(t, s, scope, f, a, "model", fixture["query"], "排除恢复后的回答")
	b1R2aSupply(t, f.last(t).Prompt, reopened.ContextVersions, fixture, source, claim, true)
}

func TestPhase2B1_P14_OneRestrictedActiveClaimClosesWholeSource(t *testing.T) {
	s, scope := b1Store(t), owner()
	fixture := b1R2aFixture(t, "P15")
	f := b1Model(t, s, `{"reply":"多记忆固定回答","actions":[]}`)
	source := b1Source(t, s, scope, fixture["title"], fixture["raw"], "manual")
	first := b1ExtractItem(fixture["claim"], "fact")
	second := b1ExtractItem(fixture["other_claim"], "fact")
	first["quote"], second["quote"] = fixture["raw"], fixture["raw"]
	first["predicate"], second["predicate"] = "交接位置", "携带物品"
	memories := b1R2aExtractAndAdopt(t, s, scope, f, source, first, second)
	allowed := b1MemoryRef(t, memories, fixture["claim"])
	restricted := b1MemoryRef(t, memories, fixture["other_claim"])
	b1R2aVisibility(t, s, scope, allowed, "model", "manual")
	b1R2aVisibility(t, s, scope, restricted, "manual")
	f.set(`{"reply":"多记忆固定回答","actions":[]}`)
	req := turnRequest(fixture["query"])
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, fixture["claim"])
	b1Absent(t, f.last(t).Prompt, fixture["raw"], fixture["other_claim"])
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, false)
	b1R2aVisibility(t, s, scope, restricted, "model", "manual")
	req = turnRequest(fixture["query"])
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, fixture["raw"], fixture["other_claim"])
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, true)
	b1R2aVisibility(t, s, scope, restricted, "manual")
	b1Delete(t, s, scope, restricted)
	// Withdrawn claims must cease restricting their still-live original.
	req = turnRequest(fixture["query"])
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, fixture["raw"], fixture["claim"])
	b1Absent(t, f.last(t).Prompt, fixture["other_claim"])
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, true)
	b1Active(t, s, scope, source, true)
}

func TestPhase2B1_P15_SourceDependentAdoptionObeysVisibilityAndItemExclusion(t *testing.T) {
	for _, closure := range []string{"hidden", "excluded"} {
		t.Run(closure, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			fixture := b1R2aFixture(t, "P15")
			f := b1Model(t, s, "- [ ] "+fixture["adopted"])
			source := b1Source(t, s, scope, fixture["title"], fixture["raw"], "manual")
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R2a 采纳事项"})
			task := st.Tasks[0].ID
			run := b1R2aRun(t, s, scope, f, task, "model", fixture["query"], "- [ ] "+fixture["adopted"])
			b1HasRef(t, run.ContextVersions, source, true)
			if run.Adopted == nil {
				t.Fatal("source-dependent result not adopted")
			}
			// Add the claim after adoption: the adopted run's input dependency is
			// strictly source, so claim validation cannot accidentally mask R2a.
			item := b1ExtractItem(fixture["claim"], "fact")
			item["quote"] = fixture["raw"]
			claim := b1MemoryRef(t, b1Extract(t, s, scope, f, source, item), fixture["claim"])
			b1R2aVisibility(t, s, scope, claim, "model", "manual")
			secretary := func(want bool) workspace.DeskTurnResponse {
				f.set(`{"reply":"采纳事项固定回答","used":["S1"],"actions":[]}`)
				req := turnRequest(fixture["query"])
				req.ThingID = &task
				out := mustTurn(t, s, scope, req)
				if want {
					b1Contains(t, f.last(t).Prompt, fixture["adopted"], fixture["raw"])
				} else {
					b1Absent(t, f.last(t).Prompt, fixture["adopted"], fixture["raw"], fixture["claim"])
				}
				b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, want)
				return out
			}
			before := secretary(true)
			originalChecklist := ""
			for _, item := range before.State.Tasks {
				if item.ID == task {
					originalChecklist = string(asJSON(item.Checklist))
				}
			}
			b1Contains(t, originalChecklist, fixture["adopted"])
			if closure == "hidden" {
				b1R2aVisibility(t, s, scope, claim, "manual")
			} else {
				workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: task, MemoryID: string(claim.ID)})
			}
			secretary(false)
			current := b1R2aRun(t, s, scope, f, task, "model", fixture["query"], "当前可用资料回答")
			b1Absent(t, f.last(t).Prompt, fixture["adopted"], fixture["raw"], fixture["claim"])
			b1HasRef(t, current.ContextVersions, source, false)
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range st.Tasks {
				if item.ID == task && string(asJSON(item.Checklist)) != originalChecklist {
					t.Error("R2a erased user-visible adopted content")
				}
			}
			if closure == "hidden" {
				b1R2aVisibility(t, s, scope, claim, "model", "manual")
			} else {
				workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: task, MemoryID: string(claim.ID)})
			}
			secretary(true)
		})
	}
}

func TestPhase2B1_P3_BriefDateUsesWorkspaceTimezoneAcrossUTCDayBoundary(t *testing.T) {
	for _, expressed := range []bool{true, false} {
		t.Run(fmt.Sprintf("expressed_%v", expressed), func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, "日期边界固定回答")
			at, err := time.Parse(time.RFC3339, b1Text(t, "brief_date_boundary", "at"))
			if err != nil {
				t.Fatal(err)
			}
			in := memory.IngestRequest{Connector: "manual", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: b1Text(t, "brief_date_boundary", "title"), Text: b1Text(t, "brief_date_boundary", "text"), MediaType: "text/plain"}
			label := "记录于"
			if expressed {
				in.ExpressedAt = &at
				label = "说于"
			}
			source := mustIngest(t, s, scope, in).Ref
			if !expressed {
				// Recorded time is synthetic past metadata, never a future reminder.
				if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET recorded_at=$1 WHERE owner_id=$2 AND record_id=$3 AND version=$4", at, string(scope.OwnerID), string(source.ID), source.Version); err != nil {
					t.Fatal(err)
				}
			}
			for _, zone := range []string{"Asia/Shanghai", "UTC"} {
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": zone})})
				want := b1Text(t, "brief_date_boundary", zone)
				other := b1Text(t, "brief_date_boundary", "UTC")
				if zone == "UTC" {
					other = b1Text(t, "brief_date_boundary", "Asia/Shanghai")
				}
				line := func(date string) string {
					return fmt.Sprintf("[source:%s@%d / %s / %s %s]", source.ID, source.Version, in.Title, label, date)
				}
				for _, agent := range []string{"model", "manual"} {
					run := b1Run(t, s, scope, agent, "b1dateneedle")
					b1Contains(t, run.Brief, line(want), in.Text)
					b1Absent(t, run.Brief, line(other))
					b1HasRef(t, run.ContextVersions, source, true)
					if agent != "manual" {
						b1Contains(t, f.last(t).Prompt, line(want), in.Text)
						b1Absent(t, f.last(t).Prompt, line(other))
					}
				}
			}
		})
	}
}
