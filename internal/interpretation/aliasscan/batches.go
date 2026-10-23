package aliasscan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// Name is one current entity name that the scan reads, with its count of
// current memories and its stable position in each scope.
type Name struct {
	Positions   map[string]int64 `json:"-"`
	N           int              `json:"n"`
	Type        string           `json:"type"`
	Name        string           `json:"name"`
	MemoryCount int              `json:"memoryCount"`
	Ref         memory.Ref       `json:"-"`
	Protected   bool             `json:"-"` // withdrawn merge: don't remerge under the same rule
}

// Batch is one list of names that the model reads together.
type Batch struct {
	Scope    string `json:"scope"`
	Entities []Name `json:"entities"`
	Marker   string `json:"-"`
}

// Up to 200 names are sent together. Larger lists use every pair of 100-name
// blocks, so EVERY pair can meet, even across languages or distant positions.
// No ordering heuristic can permanently separate translations.
func Batches(names []Name, version int) []Batch {
	return BatchesFor(names, version, false)
}

// BatchesFor gives the batches of the names. Legacy batches are
// reconstructed only to transfer still-matching paid receipts.
func BatchesFor(names []Name, version int, legacy bool) []Batch {
	out := []Batch{}
	for _, scope := range []string{"person", "place", "organization", "topic", "project", "place_topic", "organization_topic"} {
		list := []Name{}
		for _, e := range names {
			if e.Type == scope || scope == "place_topic" && (e.Type == "place" || e.Type == "topic") || scope == "organization_topic" && (e.Type == "organization" || e.Type == "topic") {
				list = append(list, e)
			}
		}
		add := func(batch []Name) {
			// Cross-type passes require both types; unused entities alone cannot
			// be confirmed, but still accompany the full current-name list.
			eligible := false
			for i, a := range batch {
				for _, b := range batch[i+1:] {
					if !a.Protected && !b.Protected && (a.Type == b.Type && scope == a.Type || a.Type != b.Type && PairTypesAllowed(a.Type, b.Type)) {
						eligible = true
					}
				}
			}
			if !eligible {
				return
			}
			entities := append([]Name{}, batch...)
			refs := make([]memory.Ref, len(entities))
			for i := range entities {
				entities[i].N = i + 1
				refs[i] = entities[i].Ref
			}
			b := Batch{Scope: scope, Entities: entities}
			identities := []any{}
			for _, e := range entities {
				identities = append(identities, []any{e.Ref.ID, e.Type, e.Name})
			}
			hash := sha256.Sum256(asJSON([]any{scope, identities}))
			if legacy {
				hash = sha256.Sum256(asJSON([]any{b, refs}))
			}
			b.Marker = fmt.Sprintf("%s:%d:seen:%x", Stage, version, hash)
			out = append(out, b)
		}
		if legacy && len(list) <= 200 {
			add(list)
			continue
		}
		stable := false
		for _, e := range list {
			if _, ok := e.Positions[scope]; ok {
				stable = true
			}
		}
		blocks := map[int64][]Name{}
		keys := []int64{}
		for i, e := range list {
			position := int64(i)
			if stable {
				position = e.Positions[scope]
			}
			key := position / 100
			if blocks[key] == nil {
				keys = append(keys, key)
			}
			blocks[key] = append(blocks[key], e)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		for _, key := range keys {
			sort.SliceStable(blocks[key], func(i, j int) bool { return blocks[key][i].Positions[scope] < blocks[key][j].Positions[scope] })
		}
		for i, a := range keys {
			for _, b := range keys[i:] {
				if legacy && a == b {
					continue
				}
				batch := append([]Name{}, blocks[a]...)
				if a != b {
					batch = append(batch, blocks[b]...)
				}
				add(batch)
			}
		}

	}
	return out
}

func asJSON(v any) []byte { b, _ := json.Marshal(v); return b }
