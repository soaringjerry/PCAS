package postgres

import (
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var _ workspace.StudioAPI = (*Store)(nil)
