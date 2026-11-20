// Package comparison owns the memory interpretation rules for the model's
// comparison of statements: which statement repeats another and which one
// replaces another. The rules here need no storage.
package comparison

import (
	"encoding/json"

	"github.com/soaringjerry/PCAS/internal/modelcall"
)

// Edge proposes that statement Old is retired in favor of statement New.
// Kind is "duplicate" or "superseded". The numbers are local to one batch.
type Edge struct {
	Old, New int
	Kind     string
}

// ParseOutput reads the model's answer for a batch of count statements.
// Numbers are local to the supplied batch, never database identifiers. Invalid
// suggestions are discarded independently; the first suggestion for an old
// memory wins. A malformed envelope is a failed comparison of the whole batch.
func ParseOutput(text string, count int) ([]Edge, bool) {
	var envelope struct {
		Duplicates json.RawMessage `json:"duplicates"`
		Superseded json.RawMessage `json:"superseded"`
	}
	if modelcall.DecodeStrict([]byte(text), &envelope) != nil {
		return nil, false
	}
	var duplicates, superseded []json.RawMessage
	if len(envelope.Duplicates) == 0 || len(envelope.Superseded) == 0 || string(envelope.Duplicates) == "null" || string(envelope.Superseded) == "null" ||
		json.Unmarshal(envelope.Duplicates, &duplicates) != nil || json.Unmarshal(envelope.Superseded, &superseded) != nil {
		return nil, false
	}
	valid := func(n int) bool { return n > 0 && n <= count }
	seen := map[int]bool{}
	edges := []Edge{}
	add := func(old, new int, kind string) {
		if !valid(old) || old == new || seen[old] {
			return
		}
		seen[old] = true
		if !valid(new) {
			return
		}
		edges = append(edges, Edge{Old: old, New: new, Kind: kind})
	}
	for _, raw := range duplicates {
		var group struct {
			Keep    json.RawMessage   `json:"keep"`
			Members []json.RawMessage `json:"members"`
		}
		// Even a malformed suggestion consumes its known old numbers. Do not
		// let a later suggestion retire them by replacing an invalid first one.
		if json.Unmarshal(raw, &group) != nil {
			continue
		}
		var keep int
		good := modelcall.DecodeStrict(raw, &group) == nil && json.Unmarshal(group.Keep, &keep) == nil && valid(keep)
		members := []int{}
		for _, member := range group.Members {
			var old int
			if json.Unmarshal(member, &old) != nil {
				good = false
				continue
			}
			members = append(members, old)
		}
		for _, old := range members {
			if old == keep {
				continue
			}
			if good {
				add(old, keep, "duplicate")
			} else {
				add(old, 0, "duplicate")
			}
		}
	}
	for _, raw := range superseded {
		var pair struct {
			Old json.RawMessage `json:"old"`
			New json.RawMessage `json:"new"`
		}
		if json.Unmarshal(raw, &pair) != nil {
			continue
		}
		var old, new int
		if json.Unmarshal(pair.Old, &old) != nil {
			continue
		}
		if modelcall.DecodeStrict(raw, &pair) != nil || json.Unmarshal(pair.New, &new) != nil {
			new = 0
		}
		if old == new && valid(old) {
			seen[old] = true
			continue
		}
		add(old, new, "superseded")
	}

	return edges, true
}

// ResolveEdges follows chains of proposals to their final keeper.
// Resolve the full proposed graph before dropping protected/changed rows. A
// cycle invalidates every suggestion leading into it; dropping one edge must
// never turn a cyclic proposal into an accepted one-way retirement.
func ResolveEdges(edges []Edge, eligible, protected map[int]bool) []Edge {
	graph := map[int]Edge{}
	for _, edge := range edges {
		graph[edge.Old] = edge
	}
	resolved := []Edge{}
	for _, edge := range edges {
		if !eligible[edge.Old] || protected[edge.Old] {
			continue
		}
		seen := map[int]bool{edge.Old: true}
		at, kind, valid := edge.New, edge.Kind, true
		for {
			if seen[at] || !eligible[at] {
				valid = false
				break
			}
			seen[at] = true
			next, exists := graph[at]
			if !exists {
				break
			}
			// A protected row is a terminal keeper, unless the original graph
			// is cyclic. Check the original graph for cycles below as well.
			if protected[at] {
				break
			}
			if next.Kind == "superseded" {
				kind = "superseded"
			}
			at = next.New
		}
		cycleSeen := map[int]bool{}
		for node := edge.Old; ; {
			if cycleSeen[node] {
				valid = false
				break
			}
			cycleSeen[node] = true
			next, exists := graph[node]
			if !exists {
				break
			}
			node = next.New
		}
		if valid {
			resolved = append(resolved, Edge{Old: edge.Old, New: at, Kind: kind})
		}
	}
	return resolved
}

// The lines of the owner's log for the comparison's own actions.
const (
	RestoreSummary = "恢复退出的记忆"
	MergeSummary   = "合并同一实体的叫法"
)
