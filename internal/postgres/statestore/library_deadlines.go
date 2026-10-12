package statestore

import (
	"crypto/sha256"
	"fmt"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/schedulestore"
)

func organizedDeadlineID(owner memory.ID, m kernel.CardMemory, d kernel.CardDeadline) string {
	sum := sha256.Sum256([]byte(statusDeadlineID(owner, m, d) + "/" + d.Title + "/" + d.TimeNote))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func optionalScheduleRule(rule *schedulestore.ScheduleRule) any {
	if rule == nil {
		return nil
	}
	return kernel.JSON(rule)
}
