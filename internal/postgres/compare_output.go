package postgres

import (
	"encoding/json"
)

type compareEdge struct {
	Old, New int
	Kind     string
}

// Numbers are local to the supplied batch, never database identifiers. Invalid
// suggestions are discarded independently; the first suggestion for an old
// memory wins. A malformed envelope is a failed comparison of the whole batch.
func parseCompareOutput(text string, count int) ([]compareEdge, bool) {
	var envelope struct {
		Duplicates json.RawMessage `json:"duplicates"`
		Superseded json.RawMessage `json:"superseded"`
	}
	if strictJSON([]byte(text), &envelope) != nil {
		return nil, false
	}
	var duplicates, superseded []json.RawMessage
	if len(envelope.Duplicates) == 0 || len(envelope.Superseded) == 0 || string(envelope.Duplicates) == "null" || string(envelope.Superseded) == "null" ||
		json.Unmarshal(envelope.Duplicates, &duplicates) != nil || json.Unmarshal(envelope.Superseded, &superseded) != nil {
		return nil, false
	}
	valid := func(n int) bool { return n > 0 && n <= count }
	seen := map[int]bool{}
	edges := []compareEdge{}
	add := func(old, new int, kind string) {
		if !valid(old) || old == new || seen[old] {
			return
		}
		seen[old] = true
		if !valid(new) {
			return
		}
		edges = append(edges, compareEdge{Old: old, New: new, Kind: kind})
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
		good := strictJSON(raw, &group) == nil && json.Unmarshal(group.Keep, &keep) == nil && valid(keep)
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
		if strictJSON(raw, &pair) != nil || json.Unmarshal(pair.New, &new) != nil {
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

// Resolve the full proposed graph before dropping protected/changed rows. A
// cycle invalidates every suggestion leading into it; dropping one edge must
// never turn a cyclic proposal into an accepted one-way retirement.
func resolveCompareEdges(edges []compareEdge, eligible, protected map[int]bool) []compareEdge {
	graph := map[int]compareEdge{}
	for _, edge := range edges {
		graph[edge.Old] = edge
	}
	resolved := []compareEdge{}
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
			resolved = append(resolved, compareEdge{Old: edge.Old, New: at, Kind: kind})
		}
	}
	return resolved
}
