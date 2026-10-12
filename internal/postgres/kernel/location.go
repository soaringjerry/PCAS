package kernel

import (
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Location is the time zone of the owner's settings. A zone that cannot be
// loaded gives UTC.
func Location(settings workspace.Settings) *time.Location {
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}
