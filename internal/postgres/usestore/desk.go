package usestore

import (
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

func (s *Use) DeskNow(loc *time.Location) string {
	return secretary.Now(s.BusinessNow(), loc)
}
