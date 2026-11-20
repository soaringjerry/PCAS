package aliasscan

import (
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestTheStrongestNameOfEachTypeIsJudgedFirst(t *testing.T) {
	name := func(id, kind, text string, count int) Name {
		return Name{Type: kind, Name: text, MemoryCount: count, Ref: memory.Ref{ID: memory.ID(id)}}
	}
	order := MergeOrder([]Name{
		name("p-few", "person", "Wang", 2),
		name("t", "topic", "考试", 9),
		name("p-latin", "person", "Li", 5),
		name("p-chinese", "person", "小李", 5),
		name("p-a", "person", "Ann", 5),
	})
	want := []string{"p-chinese", "p-a", "p-latin", "p-few", "t"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
