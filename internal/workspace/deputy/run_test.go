package deputy

import (
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func ids(ms []workspace.Memory) string {
	out := ""
	for _, m := range ms {
		out += m.ID
	}
	return out
}

func TestARunGetsItsMemoriesInOneOrderAndKeepsLongTermConstraints(t *testing.T) {
	all := []workspace.Memory{
		{ID: "a", Version: 1, Kind: "fact"}, {ID: "b", Version: 2, Kind: "fact"}, {ID: "c", Version: 1, Kind: "preference"},
		{ID: "d", Version: 1, Kind: "decision"}, {ID: "e", Version: 1, Kind: "fact"}, {ID: "f", Version: 1, Kind: "fact", Trust: "inferred"},
	}
	ref := func(id string, v int) memory.Ref { return memory.Ref{ID: memory.ID(id), Version: v} }
	// b was recalled at an old version as a structured match; it comes with the other recalled memories.
	got := OrderMemories(all, []memory.Ref{ref("e", 1), ref("b", 1)}, []memory.Ref{ref("a", 1)}, []memory.Ref{ref("b", 1), ref("e", 1), ref("x", 1)})
	if ids(got) != "eacdb" {
		t.Fatal(ids(got))
	}
	if ids(OrderMemories(all, nil, nil, nil)) != "cd" {
		t.Fatal("each preference and decision is given without a recall")
	}
	agent := workspace.Agent{MemoryKinds: []string{"fact"}}
	ranked := RankMemories(all, agent, []memory.Ref{ref("e", 1)}, []memory.Ref{ref("a", 1)}, []memory.Ref{ref("b", 1), ref("b", 2), ref("c", 1), ref("f", 1), ref("e", 1)})
	if ids(ranked) != "eab" {
		t.Fatal("only current versions that the agent may get are ranked", ids(ranked))
	}
}

func TestARunTakesAResultByHandOnlyWhileItWaits(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	run := workspace.Run{Status: "waiting", Kind: "draft", Error: "旧错误"}
	if err := Paste(&run, "虚构的结果", now); err != nil || run.Status != "done" || run.Output != "虚构的结果" || run.Error != "" || run.FinishedAt != "2026-03-01T00:00:00Z" {
		t.Fatalf("%+v %v", run, err)
	}
	if Paste(&run, "再贴一次", now) != memory.ErrConflict {
		t.Fatal("a finished run takes no result")
	}
	empty := workspace.Run{Status: "waiting", Kind: "draft"}
	if Paste(&empty, "  ", now) != memory.ErrConflict || empty.Status != "waiting" {
		t.Fatal("a result needs text")
	}
	stale := workspace.Run{Status: "waiting", StaleContext: true}
	if CanPaste(stale) != memory.ErrConflict {
		t.Fatal("a run whose context changed takes no result")
	}
	running := workspace.Run{Status: "running"}
	if Discard(&running, now) != memory.ErrConflict {
		t.Fatal("a run that is being generated cannot be discarded")
	}
	queued := workspace.Run{Status: "queued"}
	if err := Discard(&queued, now); err != nil || queued.Status != "failed" || queued.Error != "已弃用" {
		t.Fatalf("%+v %v", queued, err)
	}
	if CheckRequest("plan", "虚构的要求") != nil || CheckRequest("email", "x") != memory.ErrInvalid || CheckRequest("plan", " ") != memory.ErrInvalid {
		t.Fatal("request")
	}
}
