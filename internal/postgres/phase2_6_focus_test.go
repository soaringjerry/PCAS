package postgres

// T2-T4 focus oracle frozen before reading the merged implementations at
// integration head 5c2479a. Supplements the original pre-implementation matrix.
//
// FOCUS-1: For classification/comparison/alias confirmation/name scan/handover,
// overlap scheduler, processing, foreground snapshot/library reads and commands.
// Record baseline and concurrent request latency, SQLSTATE, deferred reasons,
// queue completion, actual provider calls, and lock waits. No 40P01 or repeated
// lock deferrals caused by independent background objects are acceptable.
// The initial three-second diagnostic guard was expanded to 30s baseline/20s
// concurrent to diagnose cold planner statistics. Measurements are reported,
// not treated as a contract SLO. The fixture ANALYZE precondition matches the
// statistics available in an established scale database, without changing indexes.
//
// FOCUS-2: Merge an entity with at least 1,200 current/historical links using the
// real confirmation processor, while foreground reads and writes run. Assert all
// links move, unchanged rows/card snapshots survive, paid output isn't repeated,
// merge latency stays below the documented one-minute transaction envelope and
// user latency/SQLSTATE is measured. Also check negative decisions with card locks.
//
// FOCUS-3: Exercise a fictitious subscription adapter which omits usage. Enforce
// a positive estimated charge, token/cost estimation flags and exact daily ledger
// settlement. Fill the daily allowance with estimated charges, then every stage
// must defer without another provider call; stage health must expose budget reason.
// Hourly budgets are independent 40/40/30/6/2, with no borrowing or queue-history
// cleanup that erases calls. Provider-attempt count is independent of DB counters.
//
// FOCUS-4/T3: Backfill all 5,000 memories from the T1 content oracle through real
// scheduling and classification. Expect ceil(5000/40)=125 successful calls; #246's
// 131 is ceil(5217/40), not an expected number for the 5,000-row fixture. Under
// 40/h both require four quota windows (3.125 and 3.275 quota-hours respectively).
// A rolling-hour release must not re-call paid outputs, drop historical usage or
// truncate members. Report real wall/model time separately from simulated budget
// windows; do not claim to have waited 3.3 real hours. Compare all 48 planted
// deadline contents/IDs, all requirement scopes, and handover input identities.
//
// Remaining matrix: execute the original G1-G7/A1-A12/B1-B9/C1-C8 expectations.
// Page checks started after #249 merged at e1e6271. A finding/skip remains an
// outstanding acceptance blocker, never a pass. Only fictitious data and owned
// disposable containers are permitted; implementations remain untouched.
