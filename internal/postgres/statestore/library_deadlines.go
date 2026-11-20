package statestore

import (
	"crypto/sha256"
	"fmt"

	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/schedulestore"
)

func organizedDeadlineID(owner memory.ID, m kernel.CardMemory, d organize.Deadline) string {
	sum := sha256.Sum256([]byte(statusDeadlineID(owner, m, d) + "/" + d.Title + "/" + d.TimeNote))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func optionalScheduleRule(rule *schedulestore.ScheduleRule) any {
	if rule == nil {
		return nil
	}
	return kernel.JSON(rule)
}

// statusDeadlineID is the identity of a deadline of a memory: the same
// deadline of the same memory version keeps its identity.
func statusDeadlineID(owner memory.ID, m kernel.CardMemory, d organize.Deadline) string {
	at := ""
	if d.At != nil {
		at = *d.At
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%s/%s/%s", owner, m.Ref.ID, m.Ref.Version, d.Kind, at, d.Recurrence)))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
