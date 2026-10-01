# Parallel real-backend CI evaluation

The browser workflow now runs the three golden repetitions on three independent
GitHub-hosted runners. Round 1 executes timezone first, twelve golden scenarios,
then the four legacy cases (17 executions); rounds 2 and 3 each execute the twelve
golden scenarios. The total stays 41 and each golden scenario executes three
times. Playwright retains one worker, the original reminder/timeout waits and
all existing assertions. Application code and scenario files are unchanged.

This report records design and local evidence before the first independent CI
run. Remote results, timings, artifact checks and the exact PR/run links will be
added to the draft PR body after completion, avoiding a report-only push that
restarts CI. Branch: `ci/parallel-real-backend`; base:
`6da99a04bc8c8215b308618f50ee1257895545b2`; PR target:
`stabilization/acceptance-candidate`. The
[branch comparison](https://github.com/soaringjerry/PCAS/compare/stabilization/acceptance-candidate...ci/parallel-real-backend)
shows the implementation. PR #32 and its running CI were not changed.

## Isolation and coverage boundary

Each matrix job creates its own owner/token, tmpfs PostgreSQL container with a
dynamic host port, temporary settings/blob/ChatGPT directories, and fixture
HTTP/TLS/proxy listeners. The fixed API and ChatGPT callback ports are safe
because the jobs use separate hosted machines. Locally the script still uses
fixed ports and result directories, so copies must run sequentially.

The fixture control API replaces shared rules and clears events; workspace
commands depend on a shared revision, and scenarios change global settings.
Increasing Playwright workers would introduce races and was not used.

Inspection found no intentional dependency on a preceding golden repetition:
`beforeEach` logs in and sets timezone/follow-ups; each scenario creates its own
records and installs its fixture rules. `arrange` creates project A when absent,
so rounds 2 and 3 can start on a fresh database without the timezone suite;
the task-page scenarios create their own tasks. Undo-chain dependencies stay
inside individual scenarios. The timezone scenario still runs before other
tests in round 1 and preserves its first-login check.

Matrix repetitions use fresh databases and therefore no longer exercise
cross-repetition state accumulation. The unset/empty-selector local default
still runs timezone, golden `--repeat-each=3`, then legacy on one database,
retaining that coverage. The two modes are not identical state coverage.

## Failure behavior

`PCAS_REAL_BACKEND_ROUND=1|2|3` selects a default-suite round. Other nonempty
values exit 2 before creating directories, processes or containers. A valid
selector does not replace or filter an explicit custom command; invalid values
are rejected even with a custom command. An empty value behaves like unset.

Suite exit failures accumulate so round 1 still attempts golden and legacy
after an earlier suite failure. The matrix uses `fail-fast: false`, without
`continue-on-error`, so another failed round does not cancel sibling rounds.
Each job uploads `real-backend-acceptance-round-<round>` with `if: always()` and
fails the upload if there are no result files. The stable `browser-real-backend`
gate runs with `always()` and exits nonzero unless the matrix result is exactly
`success`, including `failure`, `cancelled`, `skipped` or empty results.

The matrix and gate follow GitHub's documented
[matrix failure handling](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/run-job-variations#handling-failures)
and [needs result values](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#needs-context).

## Local verification

Commands executed in `/root/PCAS-wt/C1`:

```sh
bash -n web/tests/support/real-backend.sh
shellcheck web/tests/support/real-backend.sh
git show HEAD:web/tests/support/real-backend.sh | shellcheck -
shellcheck -e SC2155,SC1091,SC2034 web/tests/support/real-backend.sh
/tmp/pcas-C1-tools/bin/actionlint -shellcheck= .github/workflows/browser-regression.yml
git diff --check
python3 /tmp/pcas-C1-verify/verify.py
```

Bash syntax, actionlint v1.7.12 and whitespace checks passed. Full ShellCheck
reports the same three baseline diagnostics: SC2155 on owner assignment,
SC1091 on generated fixture.env and SC2034 on the readiness loop counter.
Filtering only those baseline diagnostics passes; no new diagnostics appeared.

The temporary orchestration harness copied the actual runner to a separate
temporary repository and stubbed Docker, Go builds, readiness HTTP and npx.
Fourteen checks passed: unset/empty/default selectors; all three round command
sequences; five invalid values; timezone/golden failures that still attempt
legacy; custom command exit 37; and gate success/failure/cancelled/skipped/empty
behavior. Invalid selectors invoked no resource command. Valid runs invoked
owned-container cleanup, removed their golden temporary directories, and left
none of the three recorded child processes alive. Evidence:
`/tmp/pcas-C1-verify/orchestration-results.json`. These are orchestration checks,
not real browser/backend execution. The actual worktree script was also invoked
with selectors `0`, `4` and `bad` plus a custom command: all exited 2 before
executing it.

After `npm ci`, the following discovery commands in `web` confirmed twelve
golden scenarios and five auxiliary cases, with no filtering:

```sh
PCAS_TEST_FIXTURE_URL=http://127.0.0.1:1 ./node_modules/.bin/playwright test \
  tests/golden.spec.ts --repeat-each=1 --list
PCAS_TEST_FIXTURE_URL=http://127.0.0.1:1 ./node_modules/.bin/playwright test \
  tests/timezone-backend.spec.ts tests/backend.spec.ts tests/continuity.spec.ts \
  tests/chatgpt-direct.spec.ts tests/model-api.spec.ts --list
```

The local host has Node 20.19.5, which npm flagged against the package's Node
22.12+ requirement; discovery completed, but no local browser run was claimed.
CI retains Node 22. No real backend, model credentials, Docker container or
fixed-port listener was started locally for C1.

## Timing baseline and pending remote evidence

T4's earlier complete native JSON reports recorded golden duration 625.3
seconds, timezone 5.1 seconds and legacy 8.5 seconds. Ordered test durations
sum to 203.4, 208.8 and 208.4 seconds for the three golden repetitions.
G6 reminder waits total 249.9 seconds and G9 timeout waits total 274.7 seconds,
about 84% of the golden report duration. That complete run had three failures
in the old conflict oracle, which was later corrected; these timings are a
baseline, not a successful run of the new matrix or the current head.

Each new runner still has one approximately 3.5-minute golden round. Running
the rounds concurrently should remove serial repetition from the critical
path, but no speedup is guaranteed before measurement. Three runners duplicate
dependency installation, build and database startup; the aggregate gate adds
one small job. Total runner usage is expected to increase even if elapsed time
decreases. Hosted scheduling and cache differences affect the comparison.

The new PR's first run must demonstrate all three successful rounds and gate,
unique artifacts, native report counts 17/12/12, every golden scenario exactly
once per round, and zero skipped/flaky/retried results. Service log copies in
each artifact show that the exit cleanup path executed; the local harness
proves recorded-process termination and directory removal, but remote
container removal is not independently observable from copied logs alone.
Compare remote job/test/workflow elapsed time and summed job seconds with the
old completed browser workflow when available, clearly separating it from
the earlier local baseline. Final evidence belongs in the draft PR body.
