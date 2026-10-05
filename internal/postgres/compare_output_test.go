package postgres

import (
	"reflect"
	"testing"
)

func TestCompareOutputRejectsInvalidNumbersAndFirstWins(t *testing.T) {
	edges, ok := parseCompareOutput(`{"duplicates":[{"keep":1,"members":[1,2,99]},{"keep":4,"members":[2,3]},{"keep":"1","members":[4]}],"superseded":[{"old":2,"new":4},{"old":4,"new":5}]}`, 5)
	want := []compareEdge{{Old: 2, New: 1, Kind: "duplicate"}, {Old: 3, New: 4, Kind: "duplicate"}}
	if !ok || !reflect.DeepEqual(edges, want) {
		t.Fatal(edges, ok)
	}
	for _, text := range []string{
		`{"duplicates":[],"superseded":[{"old":1,"new":"invalid"},{"old":1,"new":2}]}`,
		`{"duplicates":[{"keep":"invalid","members":[1]},{"keep":2,"members":[1]}],"superseded":[]}`,
		`{"duplicates":[{"keep":99,"members":[1]},{"keep":2,"members":[1]}],"superseded":[]}`,
	} {
		if edges, ok := parseCompareOutput(text, 2); !ok || len(edges) != 0 {
			t.Fatal("invalid first suggestion replaced", edges, ok)
		}
	}
	for _, text := range []string{`null`, `no JSON`, `{}`, `{"duplicates":null,"superseded":[]}`, `{"duplicates":[],"superseded":{}}`} {
		if _, ok := parseCompareOutput(text, 5); ok {
			t.Fatal("accepted malformed envelope", text)
		}
	}
	if edges, ok := parseCompareOutput(`{"duplicates":[],"superseded":[]}`, 5); !ok || len(edges) != 0 {
		t.Fatal(edges, ok)
	}
}

func TestCompareGraphRejectsCyclesResolvesChainsAndProtectsRows(t *testing.T) {
	eligible := map[int]bool{1: true, 2: true, 3: true, 4: true}
	for _, protected := range []map[int]bool{{}, {1: true}} {
		edges := []compareEdge{{Old: 1, New: 2, Kind: "superseded"}, {Old: 2, New: 1, Kind: "superseded"}, {Old: 3, New: 1, Kind: "duplicate"}}
		if got := resolveCompareEdges(edges, eligible, protected); len(got) != 0 {
			t.Fatal("cyclic proposal accepted", got)
		}
	}
	edges := []compareEdge{{Old: 1, New: 2, Kind: "duplicate"}, {Old: 2, New: 3, Kind: "superseded"}}
	want := []compareEdge{{Old: 1, New: 3, Kind: "superseded"}, {Old: 2, New: 3, Kind: "superseded"}}
	if got := resolveCompareEdges(edges, eligible, nil); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	want = []compareEdge{{Old: 1, New: 2, Kind: "duplicate"}}
	if got := resolveCompareEdges(edges, eligible, map[int]bool{2: true}); !reflect.DeepEqual(got, want) {
		t.Fatal("protected terminal", got)
	}
	delete(eligible, 2)
	if got := resolveCompareEdges(edges, eligible, nil); len(got) != 0 {
		t.Fatal("changed intermediate accepted", got)
	}
}
