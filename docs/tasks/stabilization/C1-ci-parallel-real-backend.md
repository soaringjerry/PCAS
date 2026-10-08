# C1: parallel real-backend CI

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

Status: implemented and locally verified; independent draft PR matrix verification pending, 2026-10-01. See `docs/evaluations/2026-10-01-ci-parallel-real-backend.md`; final remote evidence will be recorded in the draft PR body without a report-only CI rerun.

## Ownership and base

- Coordinator: root (task definition, review, integration decisions only).
- Executor: Sol 6.1, high; agent `ci_parallel_audit` continues as C1.
- Exclusive worktree: `/root/PCAS-wt/C1`, branch `ci/parallel-real-backend`.
- Base: `6da99a04bc8c8215b308618f50ee1257895545b2`, aggregate PR #32.
- Allowed changes: this task document, `.github/workflows/browser-regression.yml`, `web/tests/support/real-backend.sh`, narrowly relevant runner documentation, and `docs/evaluations/2026-10-01-ci-parallel-real-backend.md`.
- Do not edit application code, test scenarios/assertions, other worktrees, or PR #32. Do not merge, deploy, cancel existing CI, or use real model credentials.

## Outcome

Reduce browser CI elapsed time by running the three existing golden repetitions on independent GitHub-hosted runners. Preserve every scenario and each golden scenario's three executions. Do not parallelize tests that share one database, owner, settings, or fixture control server.

## Implementation contract

1. Three independent real-backend matrix jobs, `fail-fast: false`, each retaining one Playwright worker.
2. Round 1: timezone once, golden once, four legacy cases once (17 executions). Rounds 2 and 3: golden once each (12 each). Total 41; no skipped cases, retries, shortened reminder/timeout behavior, or reduced assertions.
3. A documented `PCAS_REAL_BACKEND_ROUND=1|2|3` selects the default runner's round. Unset retains the existing shared-database full local suite (timezone + golden repeat-three + legacy). Invalid nonempty values fail before resource allocation. Preserve the existing custom-command path; explicitly document its interaction with the selector.
4. Keep failure accumulation so auxiliary tests still execute after an earlier suite failure. Every round uploads uniquely named artifacts with `if: always()`. Preserve owned-resource cleanup.
5. Keep the stable `browser-real-backend` check as an aggregate gate. It succeeds only if every matrix round succeeds; failure, cancellation, or skip must not become success. Existing main branch protection API reports unprotected; keeping the check also avoids future naming surprises.
6. State the coverage boundary: matrix repetitions each use a fresh database; the local default retains shared-database cross-round accumulation. Inspect scenarios to ensure none intentionally requires earlier repetitions. Do not describe these two execution modes as identical state coverage.

## Verification and delivery

- Inspect syntax and available shell/workflow linting; validate illegal selector behavior without creating resources. Review gate handling for success/failure/cancellation/skip.
- Push only this new branch and open a separate draft PR targeting `stabilization/acceptance-candidate` (PR #32's branch). Use a structured body or `--body-file`; do not edit aggregate history to accelerate its current run.
- Let the new PR's actual matrix CI prove all three rounds and artifacts. Check 17/12/12 counts, each golden scenario present once per round, and no skipped/flaky/retried execution. Retain any failures and fix causes rather than weakening checks.
- Measure elapsed test/job/workflow time against the existing ~625-second golden / ~13.6-second auxiliary local baseline and, when available, the old workflow's completed remote timing. Clearly distinguish estimates from measurements and report increased aggregate runner usage.
- Put design, exact commands, results, limitations, cleanup evidence, and PR/run links in the evaluation report. Avoid a report-only push that pointlessly reruns all CI; use PR-body updates for final remote results if the report was committed before completion.
- Do not start three copies on the same host: fixed callback/API ports and output directories make that unsafe.
- Return exact commit/PR, checks, any remaining work, and clean worktree/resource status.
