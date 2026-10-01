package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestRunKeepsRelevantOldMemoryAheadOfRecentNoise(t *testing.T) {
	s := testStore(t)
	phase2ManualDestination(t, s)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "旧项目月光列车的交付暗号是蓝色灯塔"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "旧项目月光列车的交付暗号是蓝色灯塔"})
	relevant := st.Memories[0].ID
	for i := 0; i < 8; i++ {
		text := fmt.Sprintf("无关新闻%d：%s", i, strings.Repeat("与项目毫无关系的材料", 550))
		st = workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: text})
		st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: text})
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "查询月光列车"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "ask", Prompt: "月光列车的交付暗号是什么"})
	pkg := phase2RTGetPackage(t, s, scope, st.Runs[0])
	if !strings.Contains(pkg.Text, "蓝色灯塔") {
		t.Fatal("relevant older memory was crowded out")
	}
	if !oneOf(relevant, st.Runs[0].ContextMemoryIDs...) {
		t.Fatal("retrieved memory missing dependency")
	}
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
}
