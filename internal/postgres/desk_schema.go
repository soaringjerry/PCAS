package postgres

import "github.com/soaringjerry/PCAS/internal/prompts"

// Preserve registered schema bytes and field order for legacy callers.
var secretaryOutputSchema = prompts.MustSchema("secretary-output").Bytes()
