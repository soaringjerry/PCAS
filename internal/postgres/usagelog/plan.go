package usagelog

import (
	"encoding/json"
	"strings"

	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// UsePlan is the plan of memory use that a usage record keeps.
type UsePlan = secretary.UsePlan

// SafeUsePlan keeps the validated group keys and depth of a plan. Arbitrary
// plan text never reaches storage.
func SafeUsePlan(raw json.RawMessage) any {
	var p UsePlan
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	keys := []string{}
	for _, key := range p.Groups {
		if kernel.OneOf(key, "self:identity", "self:taste", "self:rule", "self:goal") || strings.HasPrefix(key, "entity:") && memory.ID(strings.TrimPrefix(key, "entity:")).Valid() {
			if !kernel.OneOf(key, keys...) {
				keys = append(keys, key)
			}
		}
	}
	if !kernel.OneOf(p.Depth, "light", "medium", "heavy") {
		p.Depth = ""
	}
	return json.RawMessage(kernel.JSON(UsePlan{Groups: keys, Depth: p.Depth}))
}
