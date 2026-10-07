package postgres

import "testing"

func TestPhase26A12ExactFB28RegressionNeverSkips(t *testing.T) {
	// Additional legacy regression, separate from the 5000-memory independent
	// boundary cases. Supply ONLY this test's disposable DB to the original
	// 110-real-call pipeline; completed history is never deleted by its fixture.
	s, _ := phase26DisposableStore(t)
	t.Setenv("PCAS_TEST_DATABASE_URL", s.pool.Config().ConnString())
	TestOrganizeCompareHourlyPipelineRetainsCompletedHistory(t)
	if t.Skipped() {
		t.Fatal("F-B2-8 must not skip")
	}
	t.Log("original F-B2-8 completed 40+40+30 actual calls, real Worker quota deferrals, and next-hour recovery in an owned disposable database")
}
