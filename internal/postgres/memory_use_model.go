package postgres

import (
	"encoding/json"
	"strings"

	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

type usePlan = secretary.UsePlan

func safeUsePlan(raw json.RawMessage) any {
	var p usePlan
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	keys := []string{}
	for _, key := range p.Groups {
		if oneOf(key, "self:identity", "self:taste", "self:rule", "self:goal") || strings.HasPrefix(key, "entity:") && memory.ID(strings.TrimPrefix(key, "entity:")).Valid() {
			if !oneOf(key, keys...) {
				keys = append(keys, key)
			}
		}
	}
	if !oneOf(p.Depth, "light", "medium", "heavy") {
		p.Depth = ""
	}
	return json.RawMessage(asJSON(usePlan{Groups: keys, Depth: p.Depth}))
}

// The deputy run has the limits of a secretary turn with heavy reading.
const heavyUseTimeout = secretary.TurnTimeout
const heavyReaderBudget = reader.Budget

// Registered bytes retain the skip-action slot and original field order.
var secretaryCheckSchema = prompts.MustSchema("secretary-check").Bytes()
