package postgres_test

import "testing"

func TestPhase25B1_ConcurrentOrganizeSchedulerWorkerAndUserRequests(t *testing.T) {
	phase25B12ConcurrentScheduler(t, "organize")
}
