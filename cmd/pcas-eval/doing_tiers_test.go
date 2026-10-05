package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/doing"
)

func TestDoingTiersDispatchLeavesLegacyModeAlone(t *testing.T) {
	for _, args := range [][]string{{"-methods=none,current,ideal"}, {"-methods", "frozen-current"}, {"-context-adapter=heavy=/tmp/executable"}} {
		if doingTierArgs(args) {
			t.Fatal("intercepted legacy method", args)
		}
	}
	for _, args := range [][]string{{"--methods=light,medium,heavy"}, {"-methods", "medium"}} {
		if !doingTierArgs(args) {
			t.Fatal("did not dispatch tiers", args)
		}
	}
}
func TestTierContextContainsHeavyAdditionsAndNoHistoryOrRequest(t *testing.T) {
	prompt := "本对话历史：私有问句\n交接说明：\n虚构交接\n必须遵守的要求：\n虚构要求\nTHIS：\n对象描述\n这句话：虚构请求\n只输出 JSON 对象。\n重档读者补充 [M8 / trust=stated] 虚构补充事实\n"
	text, err := tierContextSections(prompt)
	if err != nil || !strings.Contains(text, "虚构交接") || !strings.Contains(text, "虚构补充事实") {
		t.Fatal("lost product context", err)
	}
	for _, excluded := range []string{"私有问句", "对象描述", "虚构请求"} {
		if strings.Contains(text, excluded) {
			t.Fatal("included non-evidence section")
		}
	}
	if _, err = tierContextSections("unknown"); err == nil {
		t.Fatal("accepted changed product format")
	}
}

type tierPeakModel struct{ active, peak atomic.Int32 }

func (m *tierPeakModel) Generate(ctx context.Context, _, _ string) (string, error) {
	n := m.active.Add(1)
	defer m.active.Add(-1)
	for {
		p := m.peak.Load()
		if n <= p || m.peak.CompareAndSwap(p, n) {
			break
		}
	}
	timer := time.NewTimer(5 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return "虚构输出", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func TestTierGlobalLimitIncludesReadersAndJudges(t *testing.T) {
	backend := &tierPeakModel{}
	model := &tierLimitedModel{model: backend, slots: make(chan struct{}, 4)}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := model.Generate(context.Background(), "虚构读者/评分", "虚构输入"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if backend.peak.Load() != 4 {
		t.Fatal("wrong physical model concurrency", backend.peak.Load())
	}
	model.slots <- struct{}{}
	model.slots <- struct{}{}
	model.slots <- struct{}{}
	model.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := model.Generate(ctx, "", ""); err != context.Canceled {
		t.Fatal("canceled queued call was started")
	}
}

type tierDelayedFailure struct{ entered, release chan struct{} }

func (m tierDelayedFailure) Generate(context.Context, string, string) (string, error) {
	close(m.entered)
	<-m.release
	return "", context.Canceled
}
func TestLateReaderFailureCannotPolluteNextTurnLedger(t *testing.T) {
	model := tierDelayedFailure{entered: make(chan struct{}), release: make(chan struct{})}
	b := newTierBridge(model, false)
	defer b.server.Close()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"system","content":"你是只读的记忆读者"},{"role":"user","content":"虚构读者请求"}]}`))
	done := make(chan struct{})
	go func() { defer close(done); b.serve(httptest.NewRecorder(), req) }()
	<-model.entered
	b.reset("", doing.Suite{}, doing.Task{}, "light")
	close(model.release)
	<-done
	usage, _, _ := b.metrics()
	if len(usage.Calls) != 0 || len(usage.Failed) != 0 {
		t.Fatal("late call contaminated next answer", usage)
	}
}

// Real product preparation, PostgreSQL TEMPLATE isolation, private approval
// gate, three requested tiers and the unchanged double-judge runner.
func TestDoingTiersPrivateProductPipeline(t *testing.T) {
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an empty disposable test database")
	}
	dir, err := os.MkdirTemp("/var/tmp", "pcas-v2c-private-test.")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s := doing.Suite{Version: 1, Synthetic: false, Persona: "虚构纸艺检查", Timezone: "UTC", AsOf: "2026-10-04T00:00:00Z", Memories: []doing.Memory{
		{ID: "f1", Text: "虚构的窄边纸只用于折纸", Group: "虚构纸艺", ExpressedAt: "2026-10-03T10:00:00Z"},
		{ID: "f2", Text: "虚构的栀岑偏好方形折纸", Group: "虚构纸艺", ExpressedAt: "2026-10-03T10:01:00Z"},
		{ID: "f3", Text: "虚构的雾棘偏好折纸信封", Group: "虚构纸艺", ExpressedAt: "2026-10-03T10:02:00Z"},
	}, Tasks: []doing.Task{
		{ID: "fictional-cross", Reviewed: true, Category: "cross_group", Request: "虚构纸艺检查", Must: []doing.Check{{ID: "M1", Text: "提到折纸", Evidence: []string{"f1"}}}, Handling: "给虚构答复", CanComplete: true},
		{ID: "fictional-recall", Reviewed: true, Category: "direct_recall", Request: "回忆虚构的窄边纸用途", Must: []doing.Check{{ID: "M1", Text: "用于折纸", Evidence: []string{"f1"}}}, Handling: "给虚构答复", CanComplete: true},
	}}
	for i := range s.Tasks {
		s.Tasks[i].Must = []doing.Check{
			{ID: "M1", Text: "窄边纸用于折纸", Evidence: []string{"f1"}},
			{ID: "M2", Text: "栀岑偏好方形折纸", Evidence: []string{"f2"}},
			{ID: "M3", Text: "雾棘偏好折纸信封", Evidence: []string{"f3"}},
		}
		s.Tasks[i].Forbidden = []doing.Check{{ID: "F1", Text: "把窄边纸说成用于烘焙", Evidence: []string{"f1"}}}
	}
	path := filepath.Join(dir, "approved.json")
	if err = doing.WriteJSON(path, s); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "report")
	args := []string{"-fake", "-private", "-suite", path, "-database-url", dsn, "-output", output, "-methods=light,medium,heavy", "-heavy-categories=cross_group,outgoing", "-workers=4", "-repeats=3", "-prepare-timeout=2m"}
	if err = runDoingTiers(args); err != nil {
		t.Fatal(err)
	}
	var report doing.Report
	raw, err := os.ReadFile(output + ".json")
	if err != nil || json.Unmarshal(raw, &report) != nil {
		t.Fatal("missing final report", err)
	}
	if !report.Fake || report.Preparation == nil || !report.Preparation.Complete || !report.Preparation.Handover || len(report.Preparation.Stages) != 3 || len(report.Rows) != 15 {
		t.Fatalf("incomplete pipeline: %+v", report.Preparation)
	}
	for _, row := range report.Rows {
		if row.TierUsage == nil || row.TierUsage.Effective != row.Method || row.ModelCalls < 3 {
			t.Fatal("missing native tier/call accounting", row)
		}
		if row.Method == "heavy" && row.Category != "cross_group" {
			t.Fatal("heavy ran unrequested category")
		}
	}
	for _, sensitive := range []string{s.Memories[0].Text, s.Tasks[0].Request, dsn} {
		if strings.Contains(string(raw), sensitive) {
			t.Fatal("report leaked private fixture")
		}
	}
	for _, suffix := range []string{".json", ".md", ".prepare.json"} {
		info, err := os.Stat(output + suffix)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("wrong private output permissions", suffix, err)
		}
	}
	if _, err = os.Stat(output + ".partial.json"); !os.IsNotExist(err) {
		t.Fatal("left complete run marked partial")
	}
	s.Tasks[0].Reviewed = false
	if err = doing.WriteJSON(path, s); err != nil {
		t.Fatal(err)
	}
	if err = runDoingTiers([]string{"-private", "-suite", path, "-methods=light", "-validate"}); err == nil {
		t.Fatal("accepted unreviewed private task")
	}
}
