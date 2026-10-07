package postgres_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// System-role routing is intentionally centralized until the first captured
// B4 request is available. The wire responses are the frozen §7 formats.
func phase25B4Kind(r phase25B234ModelRequest) string {
	var sys strings.Builder
	for _, m := range r.Messages {
		if m.Role == "system" {
			var s string
			_ = json.Unmarshal(m.Content, &s)
			sys.WriteString(s)
		}
	}
	s := sys.String()
	if strings.Contains(phase25B4Prompt(r), `只输出 JSON：{"groups"`) {
		return "selection"
	}
	if strings.Contains(s, "自查") || strings.Contains(s, "检查并修订") {
		return "selfcheck"
	}
	if strings.Contains(s, "读者") || strings.Contains(s, "只读这一组") || (strings.Contains(s, `"used"`) && !strings.Contains(s, `"answer"`) && !strings.Contains(s, `"reply"`)) {
		return "reader"
	}
	return "answer"
}
func phase25B4Selection(keys []string) phase25B234ModelReply {
	s, e := phase25B4DefaultAdapter().encodeSelection(keys)
	if e != nil {
		return phase25B234ModelReply{status: 500}
	}
	return phase25B234ModelReply{content: s}
}
func phase25B4Reader(ids []memory.ID) phase25B234ModelReply {
	s, e := phase25B4DefaultAdapter().encodeReader(ids)
	if e != nil {
		return phase25B234ModelReply{status: 500}
	}
	return phase25B234ModelReply{content: s}
}

type phase25B4HeavyGroup struct {
	key   string
	name  string
	refs  []memory.Ref
	texts []string
}

func (f *phase25B234Fixture) heavyGroups(t *testing.T, n int) []phase25B4HeavyGroup {
	t.Helper()
	var groups []phase25B4HeavyGroup
	for i := 0; i < n; i++ {
		g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", fmt.Sprintf("虚构接力项目%02d", i))), Type: "project", Name: fmt.Sprintf("虚构接力项目%02d", i)}
		group := phase25B4HeavyGroup{key: "entity:" + g.EntityID, name: g.Name}
		for j := 0; j < 3; j++ {
			text := fmt.Sprintf("AcceptanceHeavy 虚构接力项目%02d原文%02d：提交蓝色图表。", i, j)
			r := f.claim(t, text)
			f.labels(t, r, "progress", true, 1, g)
			group.refs = append(group.refs, r)
			group.texts = append(group.texts, text)
		}
		f.card(t, g, group.refs, false)
		groups = append(groups, group)
	}
	if len(groups) > 0 {
		f.handover(t, groups[0].key, false)
	}
	return groups
}
func phase25B4ReaderGroup(t *testing.T, r phase25B234ModelRequest, groups []phase25B4HeavyGroup) int {
	t.Helper()
	p := phase25B4Prompt(r)
	found := -1
	for i, g := range groups {
		has := false
		for _, s := range g.texts {
			if strings.Contains(p, s) {
				has = true
			}
		}
		if has {
			if found >= 0 {
				t.Error("independent reader received two groups")
			}
			found = i
		}
	}
	if found < 0 {
		t.Error("reader received no original current group text")
	}
	return found
}
func TestPhase25B4_X4_9_DefaultDeputyFiveReadersOneFailure(t *testing.T) {
	f := phase25B234NewFixtureTimeout(t, 3*time.Minute)
	groups := f.heavyGroups(t, 5)
	var keys []string
	for _, g := range groups {
		keys = append(keys, g.key)
	}
	var mu sync.Mutex
	calls := map[string]int{}
	entered := make(chan struct{}, 5)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	model := f.model(t, func(r *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		kind := phase25B4Kind(request)
		mu.Lock()
		calls[kind]++
		mu.Unlock()
		switch kind {
		case "selection":
			phase25B4MustContain(t, phase25B4Prompt(request), keys...)
			return phase25B4Selection(keys)
		case "reader":
			i := phase25B4ReaderGroup(t, request, groups)
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return phase25B234ModelReply{status: 503}
			}
			if i < 0 || i == 4 {
				return phase25B234ModelReply{status: 503}
			}
			return phase25B4Reader([]memory.ID{groups[i].refs[0].ID})
		default:
			p := phase25B4Prompt(request)
			for _, g := range groups[:4] {
				phase25B4MustContain(t, p, g.texts[0])
			}
			if kind == "answer" {
				time.Sleep(50 * time.Millisecond)
			}
			return phase25B4DeputyReply("虚构四组接力完成。")
		}
	})
	// Release only after all five independent readers enter. Serial readers
	// cannot satisfy this barrier and produce a clear concurrency failure.
	barrierDone := make(chan struct{})
	t.Cleanup(func() { once.Do(func() { close(release) }); <-barrierDone })
	go func() {
		defer close(barrierDone)
		for i := 0; i < 5; i++ {
			select {
			case <-entered:
			case <-release:
				return
			case <-time.After(5 * time.Second):
				t.Error("readers were not parallel")
				once.Do(func() { close(release) })
				return
			}
		}
		once.Do(func() { close(release) })
	}()
	run := f.deputyRun(t, "认真写一份虚构接力方案")
	if !strings.Contains(run.Output, "虚构四组接力完成") {
		t.Errorf("deputy output=%q", run.Output)
	}
	mu.Lock()
	if calls["selection"] != 1 || calls["reader"] != 5 || calls["answer"] != 1 || calls["selfcheck"] != 1 {
		t.Errorf("heavy calls=%v", calls)
	}
	mu.Unlock()
	if len(model.calls()) != 8 {
		t.Errorf("heavy total calls=%d", len(model.calls()))
	}
	f.tierUsage(t, "heavy", map[string]int{"deputy": 1, "selfcheck": 1})
	for _, g := range groups {
		f.assertRevisions(t, g.refs...)
	}
	var allPlan bool
	if err := f.db.QueryRow(f.ctx, `SELECT bool_and(plan->'groups' @> $2::jsonb) FROM model_usage WHERE owner_id=$1 AND purpose IN ('deputy','selfcheck')`, f.scope.OwnerID, phase25B4KeysJSON(keys)).Scan(&allPlan); err != nil {
		t.Fatal(err)
	}
	if !allPlan {
		t.Error("heavy selected groups missing from answer/selfcheck usage plan")
	}
	var readersValid bool
	if err := f.db.QueryRow(f.ctx, `SELECT bool_and(jsonb_array_length(plan->'groups') > 0 AND $2::jsonb @> (plan->'groups')) FROM model_usage WHERE owner_id=$1 AND purpose='reader'`, f.scope.OwnerID, phase25B4KeysJSON(keys)).Scan(&readersValid); err != nil {
		t.Fatal(err)
	}
	if !readersValid {
		t.Error("reader plan must name a nonempty subset of selected groups")
	}
}
func phase25B4KeysJSON(keys []string) string { data, _ := json.Marshal(keys); return string(data) }
func TestPhase25B4_X4_10_SharedNinetySecondReaderDeadlineLeavesAnswerTime(t *testing.T) {
	f := phase25B234NewFixtureTimeout(t, 175*time.Second)
	groups := f.heavyGroups(t, 2)
	keys := []string{groups[0].key, groups[1].key}
	var mu sync.Mutex
	readerStarted := time.Time{}
	slowCancelled := false
	f.model(t, func(r *http.Request, _ int, request phase25B234ModelRequest) phase25B234ModelReply {
		kind := phase25B4Kind(request)
		switch kind {
		case "selection":
			return phase25B4Selection(keys)
		case "reader":
			i := phase25B4ReaderGroup(t, request, groups)
			mu.Lock()
			if readerStarted.IsZero() {
				readerStarted = time.Now()
			}
			mu.Unlock()
			if i == 1 {
				select {
				case <-r.Context().Done():
					mu.Lock()
					slowCancelled = true
					mu.Unlock()
					return phase25B234ModelReply{status: 503}
				case <-time.After(150 * time.Second):
					t.Error("slow reader exceeded shared 90-second deadline")
					return phase25B4Reader([]memory.ID{groups[1].refs[0].ID})
				}
			}
			return phase25B4Reader([]memory.ID{groups[0].refs[0].ID})
		default:
			phase25B4MustContain(t, phase25B4Prompt(request), groups[0].texts[0])
			if kind == "answer" {
				time.Sleep(50 * time.Millisecond)
			}
			return phase25B4DeputyReply("虚构采用已经返回的结果。")
		}
	})
	start := time.Now()
	run := f.deputyRun(t, "起草虚构限时方案")
	elapsed := time.Since(start)
	if elapsed > 170*time.Second {
		t.Errorf("round deadline=%s", elapsed)
	}
	if run.Output != "虚构采用已经返回的结果。\n这几组没来得及看："+groups[1].name {
		t.Errorf("partial answer=%q", run.Output)
	}
	mu.Lock()
	cancelled, started := slowCancelled, readerStarted
	mu.Unlock()
	if !cancelled {
		t.Error("slow reader not cancelled")
	}
	if !started.IsZero() && time.Since(started) > 100*time.Second {
		t.Errorf("reader budget exceeded: %s", time.Since(started))
	}
}
func TestPhase25B4_SelectionValidatesKeysTwelveCapAndReaderIDs(t *testing.T) {
	f := phase25B234NewFixtureTimeout(t, 3*time.Minute)
	groups := f.heavyGroups(t, 13)
	keys := []string{"entity:00000000-0000-0000-0000-000000000000"}
	for _, g := range groups {
		keys = append(keys, g.key)
	}
	keys = append(keys, groups[0].key)
	foreign := f.claim(t, "虚构读者不允许使用的外组哨兵。")
	var mu sync.Mutex
	readers := 0
	f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
		kind := phase25B4Kind(r)
		switch kind {
		case "selection":
			return phase25B4Selection(keys)
		case "reader":
			i := phase25B4ReaderGroup(t, r, groups)
			mu.Lock()
			readers++
			mu.Unlock()
			if i < 0 {
				return phase25B234ModelReply{status: 400}
			}
			return phase25B4Reader([]memory.ID{groups[i].refs[0].ID, foreign.ID, groups[i].refs[0].ID})
		default:
			if strings.Contains(phase25B4Prompt(r), "外组哨兵") {
				t.Error("invalid reader ID reached answer")
			}
			if kind == "answer" {
				time.Sleep(50 * time.Millisecond)
			}
			return phase25B4DeputyReply("虚构已校验分组与编号。")
		}
	})
	f.deputyRun(t, "起草 AcceptanceHeavy 虚构方案")
	mu.Lock()
	defer mu.Unlock()
	if readers != 12 {
		t.Errorf("valid distinct readers=%d want 12", readers)
	}
}
