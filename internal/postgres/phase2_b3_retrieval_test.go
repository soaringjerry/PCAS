package postgres

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2B3_S1_StructuredHitsFirstWithLocalMetadata(t *testing.T) {
	for _, zone := range []string{"Australia/Sydney", "Asia/Shanghai"} {
		t.Run(zone, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, b3Used())
			d := b3FixtureData(t, s, scope, zone)
			mustTurn(t, s, scope, turnRequest(b3Text(t, "query")))
			prompt := f.last(t).Prompt
			b3Order(t, prompt, b3Text(t, "self_old_a"), b3Text(t, "self_old_b"), b3Text(t, "wang"))
			first := strings.Index(prompt, b3Text(t, "self_old_b"))
			for _, key := range []string{"self_current", "dali"} {
				if idx := strings.Index(prompt, b3Text(t, key)); idx >= 0 && idx < first {
					t.Errorf("%s appeared before the two target plans", key)
				}
			}
			for _, key := range []string{"self_old_a", "self_old_b", "wang"} {
				line := b3MemoryLine(t, prompt, b3Text(t, key))
				b1Contains(t, line, "说于", fmt.Sprint(d.Now.Year()-1), "事件", "成都", "老王")
			}
			// A UTC/local day boundary proves that formatting actually uses settings.
			at := time.Date(d.Now.Year()-1, 2, 3, 23, 30, 0, 0, time.UTC)
			b3Exec(t, s, `UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, d.Refs["self_old_a"].ID, at)
			mustTurn(t, s, scope, turnRequest(b3Text(t, "query")))
			b1Contains(t, b3MemoryLine(t, f.last(t).Prompt, b3Text(t, "self_old_a")), "说于 "+at.In(d.Now.Location()).Format("2006-01-02"))
		})
	}
}

func TestPhase2B3_S2_VisibilityAndExcerptTimePriority(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	d := b3FixtureData(t, s, scope, "Asia/Shanghai")
	req := turnRequest(b3Text(t, "query"))
	mustTurn(t, s, scope, req)
	prompt := f.last(t).Prompt
	b1Absent(t, prompt, b3Text(t, "hidden"))
	b1Contains(t, prompt, "相关原话")
	b3Order(t, prompt, b3Text(t, "raw_old"), b3Text(t, "raw_current"))
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), d.Refs["hidden"], false)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), d.Refs["raw_old"], true)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), d.Refs["raw_current"], true)
	// R2a also closes the original of a structurally matched restricted claim.
	original := b3Source(t, s, scope, "成都受限原话：未公开的行程", b3Year(d.Now, -1, time.May, 1))
	hidden := b3Claim(t, s, scope, b3ClaimSpec{Text: "关闭的附属记忆", Subject: d.Self, Said: &d.Now, Source: original, Mentions: []b3Mention{{d.Chengdu, "place"}}, Agents: []string{"manual"}})
	req = turnRequest(b3Text(t, "query"))
	mustTurn(t, s, scope, req)
	b1Absent(t, f.last(t).Prompt, "成都受限原话：未公开的行程", "关闭的附属记忆")
	for _, ref := range []memory.Ref{hidden, original} {
		b1HasRef(t, b1Refs(t, s, scope, req.RequestID), ref, false)
	}
}

func TestPhase2B3_S3_RelaxTimeOnlyWhenEntityMatches(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Australia/Sydney")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	old := b3Year(now, -2, time.March, 1)
	b3Claim(t, s, scope, b3ClaimSpec{Text: b3Text(t, "fallback"), Subject: self, Said: &old, Mentions: []b3Mention{{place, "place"}}})
	mustTurn(t, s, scope, turnRequest("去年说去成都要做什么来着"))
	b1Contains(t, f.last(t).Prompt, b3Text(t, "fallback"), b3Text(t, "relaxed"), "说于 "+old.Format("2006-01-02"))
	b1Contains(t, f.last(t).System, "实际", "日期")
	valid := b3Year(now, -1, time.April, 1)
	b3Claim(t, s, scope, b3ClaimSpec{Text: "正常年份计划", Subject: self, Said: &valid, Mentions: []b3Mention{{place, "place"}}})
	mustTurn(t, s, scope, turnRequest("去年说去成都要做什么来着"))
	b1Contains(t, f.last(t).Prompt, "正常年份计划")
	b1Absent(t, f.last(t).Prompt, b3Text(t, "relaxed"))
	// Time-only misses never get the R8 relaxation notice.
	// Use an explicit empty year, independent of weekday and the test's run date.
	mustTurn(t, s, scope, turnRequest(fmt.Sprintf("%d 年说过什么", now.Year()-5)))
	b1Absent(t, f.last(t).Prompt, b3Text(t, "relaxed"))
}

func TestPhase2B3_S4_TimeOnlyNaturePriorityAndSaidAxis(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	// A real self entity proves 我 changes ranking without adding an entity filter.
	subject := b3Entity(t, s, scope, "self", "本人")
	weekday := (int(now.Weekday()) + 6) % 7
	start := testsupport.DateFromToday(t, "Asia/Shanghai", -weekday-7, 0, 0)
	said := start.AddDate(0, 0, 2).Add(12 * time.Hour)
	for _, spec := range []b3ClaimSpec{{Text: "普通记录标记", Nature: "fact"}, {Text: "计划记录标记", Nature: "plan"}, {Text: "意向记录标记", Nature: "intention"}} {
		spec.Subject = subject
		at := said
		if spec.Nature == "fact" {
			at = at.Add(time.Hour)
		}
		spec.Said = &at
		b3Claim(t, s, scope, spec)
	}
	outside := start.AddDate(0, 0, -20)
	b3Claim(t, s, scope, b3ClaimSpec{Text: "只在事件时间重叠的记录", Subject: subject, Said: &outside, EventFrom: &start, EventTo: &said, Precision: "range"})
	mustTurn(t, s, scope, turnRequest("我上周说了什么计划"))
	prompt := f.last(t).Prompt
	for _, text := range []string{"计划记录标记", "意向记录标记"} {
		b3Order(t, prompt, text, "普通记录标记")
	}
	b1Absent(t, prompt, "只在事件时间重叠的记录")
	// either must admit overlapping events, including an old expression date.
	mustTurn(t, s, scope, turnRequest("上周有什么计划"))
	b1Contains(t, f.last(t).Prompt, "只在事件时间重叠的记录")
	// Half-open intervals: event ending at start and starting at end do not overlap.
	end := start.AddDate(0, 0, 7)
	before := start.AddDate(0, 0, -1)
	after := end.AddDate(0, 0, 1)
	b3Claim(t, s, scope, b3ClaimSpec{Text: "止于左边界暗号", Subject: subject, Said: &outside, EventFrom: &before, EventTo: &start, Precision: "range"})
	b3Claim(t, s, scope, b3ClaimSpec{Text: "始于右边界暗号", Subject: subject, Said: &outside, EventFrom: &end, EventTo: &after, Precision: "range"})
	mustTurn(t, s, scope, turnRequest("上周有什么计划"))
	b1Absent(t, f.last(t).Prompt, "止于左边界暗号", "始于右边界暗号")
}

func TestPhase2B3_S5_EntityOnlyAcrossYears(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	wang := b3Entity(t, s, scope, "person", "老王", "老王")
	for i := 1; i <= 3; i++ {
		at := b3Year(now, -i, time.March, 1)
		b3Claim(t, s, scope, b3ClaimSpec{Text: fmt.Sprintf("被提到的人职业档案%d", i), Subject: self, Said: &at, Mentions: []b3Mention{{wang, "person"}}})
	}
	b3Claim(t, s, scope, b3ClaimSpec{Text: "老王无关词法记录", Subject: self, Said: &now})
	mustTurn(t, s, scope, turnRequest("老王是做什么的"))
	for i := 1; i <= 3; i++ {
		b3Order(t, f.last(t).Prompt, fmt.Sprintf("被提到的人职业档案%d", i), "老王无关词法记录")
	}
}

func TestPhase2B3_S7_AllExistingFiltersApplyToStructuredHits(t *testing.T) {
	for _, filter := range []string{"category", "inferred", "excluded", "project", "visibility"} {
		t.Run(filter, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, "过滤验收回答")
			now := b3Zone(t, s, scope, "Asia/Shanghai")
			self := b3Entity(t, s, scope, "self", "本人")
			place := b3Entity(t, s, scope, "place", "成都", "成都")
			project := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "当前项目"}).Projects[0].ID
			other := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "其他项目"})
			otherID := ""
			for _, p := range other.Projects {
				if p.ID != project {
					otherID = p.ID
				}
			}
			if otherID == "" {
				t.Fatal("missing other project")
			}
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "过滤事项", ProjectID: project})
			task := st.Tasks[0].ID
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "model", Patch: asJSON(map[string]any{"memoryKinds": []string{"plan"}, "includeInferred": false})})
			at := b3Year(now, -1, time.December, 1)
			allow := b3Claim(t, s, scope, b3ClaimSpec{Text: "允许命中标记", Subject: self, Said: &at, Project: project, Mentions: []b3Mention{{place, "place"}}})
			source := b3Source(t, s, scope, "成都受限证据暗号", at)
			spec := b3ClaimSpec{Text: "受限命中标记", Subject: self, Said: &at, Project: project, Source: source, Mentions: []b3Mention{{place, "place"}}}
			switch filter {
			case "category":
				spec.Nature = "preference"
			case "inferred":
				spec.Acquisition = "inferred"
			case "project":
				spec.Project = otherID
			case "visibility":
				spec.Agents = []string{"manual"}
			}
			hidden := b3Claim(t, s, scope, spec)
			if filter == "excluded" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: task, MemoryID: string(hidden.ID)})
			}
			run := b1R2aRun(t, s, scope, f, task, "model", "去年说去成都要做什么", "过滤验收回答")
			b1Contains(t, f.last(t).Prompt, "允许命中标记")
			b1Absent(t, f.last(t).Prompt, "受限命中标记")
			b1HasRef(t, run.ContextVersions, allow, true)
			b1HasRef(t, run.ContextVersions, hidden, false)
			// Section 8: only user visibility and item exclusions restrict originals.
			// Category, inference and project filters restrict claims alone.
			if filter == "excluded" || filter == "visibility" {
				b1Absent(t, f.last(t).Prompt, "成都受限证据暗号")
				b1HasRef(t, run.ContextVersions, source, false)
			}
		})
	}
}

func TestPhase2B3_S8_SixtyHitsKeepTopTwentyAndExcerptBudget(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	base := b3Year(now, -1, time.December, 1)
	for i := 0; i < 60; i++ {
		at := base.Add(-time.Duration(i) * time.Hour)
		b3Claim(t, s, scope, b3ClaimSpec{Text: fmt.Sprintf("预算命中%02d结束", i), Subject: self, Said: &at, Mentions: []b3Mention{{place, "place"}}})
	}
	for i := 0; i < 10; i++ {
		b3Source(t, s, scope, fmt.Sprintf("成都预算原话%02d：", i)+strings.Repeat("长", 600), base.Add(time.Duration(i)*time.Hour))
	}
	req := turnRequest("去年说去成都要做什么来着")
	out := mustTurn(t, s, scope, req)
	if out.Turn.Reply == "" {
		t.Fatal("budget truncation prevented normal completion")
	}
	prompt := f.last(t).Prompt
	parts := []string{}
	for i := 0; i < 60; i++ {
		text := fmt.Sprintf("预算命中%02d结束", i)
		if i < 20 {
			parts = append(parts, text)
		} else {
			b1Absent(t, prompt, text)
		}
	}
	b3Order(t, prompt, parts...)
	refs := b1Refs(t, s, scope, req.RequestID)
	memories, sources := 0, 0
	for _, ref := range refs {
		if ref.Kind == memory.ClaimKind {
			memories++
		}
		if ref.Kind == memory.SourceKind {
			sources++
		}
	}
	if memories != 20 || sources > 6 {
		t.Errorf("delivered dependency counts memories=%d sources=%d", memories, sources)
	}
	// Count the actual excerpt bodies (including their identifying text),
	// excluding only each source's prompt metadata and the section headings.
	count, excerpts := 0, 0
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "[S") {
			end := strings.Index(line, "] ")
			if end < 0 {
				t.Fatalf("malformed actual excerpt line: %q", line)
			}
			count += utf8.RuneCountInString(line[end+2:])
			excerpts++
		}
	}
	if count == 0 || count > 2400 || excerpts > 6 || excerpts != sources {
		t.Errorf("actual excerpts=%d characters=%d dependencies=%d", excerpts, count, sources)
	}
}

func TestPhase2B3_S9_SecretaryDeputyManualShareStructuredPrefix(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	d := b3FixtureData(t, s, scope, "Asia/Shanghai")
	mustTurn(t, s, scope, turnRequest(b3Text(t, "query")))
	b3Order(t, f.last(t).Prompt, b3Text(t, "self_old_a"), b3Text(t, "self_old_b"), b3Text(t, "wang"))
	task := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "核对安排"}).Tasks[0].ID
	// Match secretary visibility for this comparison; S2/S7 cover restrictions.
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(d.Refs["hidden"].ID), AgentIDs: []string{}})
	for _, agent := range []string{"model", "manual"} {
		workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: agent, Patch: asJSON(map[string]any{"memoryKinds": []string{"fact", "preference", "intention", "plan", "decision"}, "includeInferred": false})})
		run := b1R2aRun(t, s, scope, f, task, agent, b3Text(t, "query"), "入口验收回答")
		b3Order(t, run.Brief, b3Text(t, "self_old_a"), b3Text(t, "self_old_b"), b3Text(t, "wang"))
		for _, key := range []string{"self_old_a", "self_old_b", "wang"} {
			b1HasRef(t, run.ContextVersions, d.Refs[key], true)
		}
		if agent == "model" {
			b3Order(t, f.last(t).Prompt, b3Text(t, "self_old_a"), b3Text(t, "self_old_b"), b3Text(t, "wang"))
		}
	}
}

func TestPhase2B3_S11_StructuredDependencyCorrectionMarksTurnOutdated(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	d := b3FixtureData(t, s, scope, "Asia/Shanghai")
	f.set(b3Used(d.Refs["self_old_a"]))
	req := turnRequest(b3Text(t, "query"))
	out := mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, b3Text(t, "self_old_a"))
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), d.Refs["self_old_a"], true)
	b1Correct(t, s, scope, d.Refs["self_old_a"], "纠正后的计划")
	b1Preserved(t, out.Turn, b1History(t, s, scope, out.ConversationID))
}

func TestPhase2B3_S12_AliasLengthSubstringCaseAndDeletedEntity(t *testing.T) {
	for _, scenario := range []string{"one_character", "substring", "case_insensitive", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, b3Used())
			now := b3Zone(t, s, scope, "Asia/Shanghai")
			subject := b3Entity(t, s, scope, "person", "匿名甲")
			alias, query := "成都", "成都市的安排"
			wanted := true
			switch scenario {
			case "one_character":
				alias = "蓉"
				query = "蓉的安排"
				wanted = false
			case "case_insensitive":
				alias = "Chengdu"
				query = "cHeNgDu的安排"
			case "deleted":
				wanted = false
			}
			place := b3Entity(t, s, scope, "place", "未知地点", alias)
			b3Claim(t, s, scope, b3ClaimSpec{Text: "独立结构匹配暗号", Subject: subject, Said: &now, Mentions: []b3Mention{{place, "place"}}})
			if scenario == "deleted" {
				b3Exec(t, s, `UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$2`, scope.OwnerID, place)
				b3Exec(t, s, `UPDATE record_versions SET state='withdrawn' WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, place)
			}
			mustTurn(t, s, scope, turnRequest(query))
			if wanted {
				b1Contains(t, f.last(t).Prompt, "独立结构匹配暗号")
			} else {
				b1Absent(t, f.last(t).Prompt, "独立结构匹配暗号")
			}
		})
	}
}

func TestPhase2B3_S13_OnlyCurrentUtterancePlansEntities(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	now := b3Zone(t, s, scope, "Asia/Shanghai")
	subject := b3Entity(t, s, scope, "person", "匿名甲")
	cd := b3Entity(t, s, scope, "place", "成都", "成都")
	dl := b3Entity(t, s, scope, "place", "大理", "大理")
	old := b3Year(now, -1, time.December, 1)
	b3Claim(t, s, scope, b3ClaimSpec{Text: "当前地点结构命中", Subject: subject, Said: &old, Mentions: []b3Mention{{cd, "place"}}})
	newer := old.Add(time.Hour)
	b3Claim(t, s, scope, b3ClaimSpec{Text: "前轮地点结构命中", Subject: subject, Said: &newer, Mentions: []b3Mention{{dl, "place"}}})
	first := mustTurn(t, s, scope, turnRequest("大理的安排"))
	req := turnRequest("去年说去成都要干什么来着")
	req.ConversationID = &first.ConversationID
	mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, "当前地点结构命中")
	b1Absent(t, f.last(t).Prompt, "前轮地点结构命中")
}

func TestPhase2B3_S1_EventPrecisionAndInclusiveRangeDisplay(t *testing.T) {
	var oracle struct{ ModelTemplates map[string]string }
	if err := json.Unmarshal(b3Gold(t)["rangeDisplayRuling"], &oracle); err != nil {
		t.Fatal(err)
	}
	t.Run("people_then_places_each_sorted_by_name", func(t *testing.T) {
		var ruling struct {
			MentionOrder struct {
				Input          []struct{ Name, Role string }
				ExpectedNames  []string
				ExpectedSuffix string
			}
		}
		if err := json.Unmarshal(b3Gold(t)["secondAcceptanceRuling"], &ruling); err != nil {
			t.Fatal(err)
		}
		if len(ruling.MentionOrder.Input) != 4 || len(ruling.MentionOrder.ExpectedNames) != 4 || ruling.MentionOrder.ExpectedSuffix == "" {
			t.Fatal("missing frozen section 8 mention order")
		}
		s, scope := b1Store(t), owner()
		f := b1Model(t, s, b3Used())
		now := b3Zone(t, s, scope, "Asia/Shanghai")
		self := b3Entity(t, s, scope, "self", "本人")
		mentions := []b3Mention{}
		for _, m := range ruling.MentionOrder.Input {
			id := b3Entity(t, s, scope, m.Role, m.Name, m.Name)
			mentions = append(mentions, b3Mention{id, m.Role})
		}
		said := b3Year(now, -1, time.January, 1)
		text := "多人多地点排序验收"
		b3Claim(t, s, scope, b3ClaimSpec{Text: text, Subject: self, Said: &said, Mentions: mentions})
		mustTurn(t, s, scope, turnRequest("成都的安排来着"))
		line := b3MemoryLine(t, f.last(t).Prompt, text)
		if !strings.HasSuffix(line, ruling.MentionOrder.ExpectedSuffix) {
			t.Errorf("model memory suffix must list people then places, sorted within each: %q", line)
		}
	})

	for _, precision := range []string{"day", "month", "year", "range"} {
		t.Run(precision, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, b3Used())
			now := b3Zone(t, s, scope, "Asia/Shanghai")
			self := b3Entity(t, s, scope, "self", "本人")
			place := b3Entity(t, s, scope, "place", "成都", "成都")
			said := b3Year(now, -1, time.January, 1)
			from := b3Year(now, -1, time.June, 12)
			from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
			to := from.AddDate(0, 0, 1)
			switch precision {
			case "month":
				from = time.Date(now.Year()-1, time.June, 1, 0, 0, 0, 0, now.Location())
				to = from.AddDate(0, 1, 0)
			case "year":
				from = time.Date(now.Year()-1, time.January, 1, 0, 0, 0, 0, now.Location())
				to = from.AddDate(1, 0, 0)
			case "range":
				to = from.AddDate(0, 0, 3)
			}
			text := "事件格式验收" + precision
			b3Claim(t, s, scope, b3ClaimSpec{Text: text, Subject: self, Said: &said, EventFrom: &from, EventTo: &to, Precision: precision, Mentions: []b3Mention{{place, "place"}}})
			mustTurn(t, s, scope, turnRequest("成都的安排来着"))
			line := b3MemoryLine(t, f.last(t).Prompt, text)
			expand := func(key string) string {
				template := oracle.ModelTemplates[key]
				if template == "" {
					t.Fatal("missing frozen event display template", key)
				}
				return strings.ReplaceAll(template, "{year}", fmt.Sprint(now.Year()-1))
			}
			if precision == "range" {
				b1Contains(t, line, "事件 "+expand("rangeFrom"), strings.TrimPrefix(expand("rangeTo"), fmt.Sprint(now.Year()-1)+"-"))
				b1Absent(t, line, strings.TrimPrefix(expand("rangeExcludedEnd"), fmt.Sprint(now.Year()-1)+"-"))
			} else {
				pattern := regexp.MustCompile("事件 " + regexp.QuoteMeta(expand(precision)) + `(?:$|\s*/)`)
				if !pattern.MatchString(line) {
					t.Errorf("event precision %s did not match frozen display: %q", precision, line)
				}
			}
		})
	}
}
