package postgres

import (
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func contextEvidenceDelimiters(marker string) (string, string) {
	return "⟦pcas-evidence:" + marker + "⟧\n", "\n⟦/pcas-evidence:" + marker + "⟧"
}

// Assign the marker only at final assembly. It cannot occur in earlier user
// text, system/schema text or imported source content. Consumers pass this
// same entry to the attempt writer, with the exact literal window unchanged.
func appendContextEvidence(prompt *strings.Builder, alias string, entry *memory.EvidenceEntry) {
	entry.AssemblyMarker = strings.ReplaceAll(string(memory.NewID()), "-", "")
	start, end := contextEvidenceDelimiters(entry.AssemblyMarker)
	role := entry.Role
	if !oneOf(role, "user", "assistant", "system", "tool") {
		role = "unknown"
	}
	expressedAt := "unknown"
	if entry.ExpressedAt != nil {
		expressedAt = entry.ExpressedAt.UTC().Format(time.RFC3339Nano)
	}
	fmt.Fprintf(prompt, "\n[%s / kind=%s / role=%s / expressed_at=%s / historical=%t / changed=%t", alias, entry.Ref.Kind, role, expressedAt, entry.Historical, entry.Changed)
	if entry.Ref.Kind == memory.ClaimKind {
		epistemic, confirmation, acquisition := entry.Epistemic, entry.Confirmation, entry.Acquisition
		if !oneOf(epistemic, "inferred", "confirmed", "sourced") {
			epistemic = "unknown"
		}
		if !oneOf(confirmation, "unknown", "candidate", "adopted", "confirmed", "disputed") {
			confirmation = "unknown"
		}
		if !oneOf(acquisition, "direct", "reported", "inferred", "execution") {
			acquisition = "unknown"
		}
		fmt.Fprintf(prompt, " / epistemic=%s / confirmation=%s / acquisition=%s", epistemic, confirmation, acquisition)
	}
	fmt.Fprintf(prompt, "]\n%s%s%s\n", start, entry.Text, end)
}
