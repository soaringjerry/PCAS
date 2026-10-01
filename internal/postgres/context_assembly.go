package postgres

import (
	"fmt"
	"strings"

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
	fmt.Fprintf(prompt, "\n[%s]\n%s%s%s\n", alias, start, entry.Text, end)
}
