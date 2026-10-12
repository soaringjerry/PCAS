package deskstore

import "github.com/soaringjerry/PCAS/internal/prompts"

// Preserve registered schema bytes and field order for legacy callers.
var SecretaryOutputSchema = prompts.MustSchema("secretary-output").Bytes()
