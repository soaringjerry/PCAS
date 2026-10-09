# Foundation Deputy Gateway

Executor: Sol. Scope prepared 2026-10-09 from `fa09f48`.

This batch continues the approved [foundation direction](../architecture.md).
The [interactive gateway batch](foundation-interactive-gateway.md) already migrated deputy self-check.
Main deputy generation and document revision still bypass the gateway.

## Goal

Trace each deputy generation from its queued request through billing, saved output, review, and application.
Keep existing product decisions and accepted data.
Use the existing gateway, prompt registry, worker, and database relations.
Do not move business packages in this batch.

## Verified Starting Paths

| Responsibility | Current path | Migration requirement |
|---|---|---|
| Admission and budget | `runs.go`, `runCommandTx` | Reuse the reservation in `agent_runs`. Do not reserve the same generation twice. |
| Generation | `runs.go`, `runAgentOnce` | Submit ordinary and revision generation through the existing gateway. |
| Revision recovery | `document_revise.go`, `cacheReviseResult` and `finishPaidRevise` | Keep saved results. Retry application without another model call. |
| Review | `deputy-selfcheck` through the gateway | Keep its separate invocation, usage, deadline, and visible fallback. |
| Deleted-run billing | `budget.go`, `settleRunCost` | Preserve the owner, run, and creation-origin fence. Do not restore deleted text. |

The daily budget sums `agent_runs.reserved_cost` and `background_usage.reserved_cost`.
The existing deletion paths can transfer a run reservation into `background_usage`.
Their key uses the owner, run ID, and recorded creation origin.
This origin identifies the execution. It is not a record-selection time range.

## Required Behavior

| Sequence | Expected result |
|---|---|
| A queued run starts generation. | Link its existing reservation to one invocation. Do not add another reservation row. |
| A reader or self-check runs. | Keep its separate reservation and usage. Do not use the main-generation reservation. |
| Generation returns. | Save the result before application. Record original usage once and settle the existing reservation. |
| The response is interrupted. | Record an unknown outcome. Retain the existing hold and partial usage. Do not assert zero spending. |
| Usage or output persistence fails. | Continue persistence through the existing recovery path. Do not repeat generation. |
| Document application fails. | Keep the complete saved result and retry the write. Do not repeat generation or self-check. |
| A legacy revision result already exists. | Complete its existing recovery path. Do not invent historical invocation metadata. |
| Memory is corrected during generation. | Keep the result with stale context. Prevent automatic adoption when existing checks require it. |
| Access is withdrawn or an input is deleted. | Prevent application and remove inaccessible output. Preserve billing evidence. |
| A run is deleted during generation. | Settle the surviving reservation under the original origin. Do not recreate the run. |
| The same run ID is reused. | Do not apply old output or settle the new execution's reservation. |
| A required provider capability is absent. | Return a visible failure before provider submission. Do not silently change the requested mode. |

Keep the existing interactive retry policy.
The background queue's five-attempt recovery allowance does not assign a new deputy retry allowance.
Record provider outcome, accounting state, and application outcome separately.

## Implementation Order

1. Add deputy reservation support to the existing interactive accounting adapter.
2. Register the unchanged revision output schema, including its exact field order.
3. Extend the isolated replay command to process one explicitly identified deputy run through the existing worker pass.
4. Migrate main generation and revision recovery together with their saved input and output identities.
5. Link final application and skipped application to the original invocation.
6. Remove the deputy provider-call exceptions after the complete workflows pass acceptance.

The first step must pass budget and recovery contracts before main generation uses it.
The accounting port returns the reservation ID and the amount actually held.
The provider estimate cannot replace the amount already reserved at admission.
The replay command must refuse a copy with other queued or running deputy work.
Its deputy operation uses the existing lease, generation, billing, and application rules.
It does not start the continuous deputy worker.
The existing offline tier override can fix the tier when the diagnostic command admits a run.
Record and replay must use the same tier. Report the selected tier with each result.
Keep the existing instruction bytes, brief bytes, search mode, and applicable model deadlines.
Do not change project selection, task meaning, document revision meaning, or automatic adoption rules.
No new production database migration is assigned.

An expired lease does not erase a known main-generation result from the same execution.
Keep that result only while its original run and input access remain valid.
A new lease must still authorize application. Result retention cannot authorize a business write.
Keep the stricter lease rule for readers and self-checks.

## Checks and Acceptance

- Run architecture checks on each commit.
- Test reservation reuse, duplicate accounting, interrupted outcomes, cancellation, deletion, and origin conflicts with fake providers.
- Run existing deputy dependency, self-check, revision, adoption, undo, and concurrent-operation contracts.
- Capture ordinary and revision requests on an isolated production-data copy before migration.
- Replay the captured model responses before and after migration with strict input checks.
- Compare actions, receipts, and complete owner data. Report each field difference without removing it.
- Check the actual default Codex channel on the isolated copy.
- Inspect the exact prompt, schema, raw response, billing links, and final writes.
- Run formatting, repository checks, PostgreSQL integration checks, and complete CI.

Keep private data, prompts, raw outputs, credentials, and data-copy manifests outside Git.
Do not claim full replay equivalence when timing or business clocks differ.
Record missing or failed checks explicitly.

After clean review and passing CI, integrate directly as the coordinator requested.
Back up the database, files, and private configuration before release.
Check the live default Codex secretary, errors, receipts, and claim counts after release.
Clean only this batch's test records by request or source ID.

## Status

| Step | Current evidence |
|---|---|
| Admission reservation adapter | Implemented. Reservation, cancellation, duplicate accounting, deletion, and origin-conflict contracts pass with synthetic providers. Production deputy generation does not use it yet. |
| Revision schema registration | Original bytes match. The registered asset preserves field order and contains no added newline. |
| Bounded deputy replay operation | Implemented through the existing worker pass. Manifest checks and existing deputy contracts pass. Ordinary and revision baselines use actual Codex on an isolated production-data copy. Final migration replay remains outstanding. |
| Main generation and application | Not migrated. The existing direct-call exceptions remain. |
| Complete batch acceptance and release | Not complete. |

The initial repository check failed when evaluation and storage tests shared a database.
The evaluation tool correctly refused nonempty data. Non-storage checks passed with a separate empty database.
Do not report the initial repository check as passed.
The storage package exceeded the aggregate 30-minute test deadline. Complete storage checks use the existing CI partition.
Complete storage checks and final checks on the completed batch remain required.
This batch does not complete Phase 3.9.

CI for `d539922` passed twelve jobs and failed the unbuilt-deputy medium-tier case.
The synthetic main answer delayed its response by 100 milliseconds.
The original self-check budget follows the elapsed main-answer phase.
Under CI load, self-check preparation exceeded that allowance before provider submission.
The fixture now matches the existing 500-millisecond allowance in the same file.
Its assertions and the production deadline remain unchanged.
[CI for `1a260dd`](https://github.com/soaringjerry/PCAS/actions/runs/37909270227) passed every job, including each storage partition.
That result certifies the preparation commit. It does not certify later changes or the complete batch.

The affected tier contract passes with pgvector after this fixture change.
The later local `make check` was interrupted when the host filesystem became full.
That command had no PostgreSQL URL. It cannot certify the database-dependent contracts.
Do not report that interruption or the incomplete local integration run as passed.

### Replay Finding

The ordinary baseline completed strict offline replay without a live provider call.
The revision baseline failed strict offline replay before main generation changed.
The prompt contains the same complete lines, but their order differs.
The failed input, recorded response, and complete owner snapshots remain private.
Do not report this replay as passed. Do not remove differences from its input comparison.

Retrieval uses actual database time to calculate activity decay.
A read-only calculation used the original query and recorded vector response.
It reproduced the changed order at the recording and replay times.
The calculation uses the qualified provider model identity from the existing embedding adapter.
Earlier calculations with an unqualified model identity did not establish the cause.

The existing business clock does not fix this retrieval input.
Extending that clock requires a separate scope decision before implementation.
The proposed scope fixes deputy date interpretation and retrieval decay on the diagnostic copy.
Production keeps actual time by default. Leases, access, quotas, and accounting continue to use actual time.
Approval and new copy checks remain pending. Keep the original failed evidence after any repair.

### Result Retention Preparation

The main-generation storage port retains known output after lease expiry or replacement.
It requires the same original execution and current input access.
Correction preserves the original input identities and output. Application still requires a current lease and validation.
Deletion or execution replacement prevents private output retention. Original billing remains recoverable.
These adapter changes do not switch production deputy generation to the gateway.
Filtered `make check` and `make test-integration` pass with pgvector.
The filter selects deputy, interactive, gateway, provider-boundary, and registered-schema contracts.
Formatting, static analysis, and the service build also pass.
These filtered checks do not certify the full suite. Complete CI on this change remains required.
The host filesystem has insufficient space for the large local fixtures.

## Sol Window Prompt

```text
Window: Sol
Continue Phase 3.9 under docs/tasks/foundation-deputy-gateway.md.
Read AGENTS.md first, then this scope and its current specification links.
Use your isolated worktree. Do not switch or stage /root/PCAS for another window.
Keep user replies in Chinese. Use English for code, commits, and current documents.
Reuse the existing gateway, prompt registry, worker pass, and storage adapters.
Complete reservation support before switching main deputy generation.
Record ordinary and revision runs on a verified isolated production-data copy first.
Keep the original instructions, schema bytes, brief bytes, and applicable deadlines.
Preserve admission budget ownership and all saved-result recovery paths.
Keep review separate from generation. Record visible skipped and failed outcomes.
Preserve stale output after correction. Prevent application after deletion or lost access.
Test duplicate requests, cancellation, deletion, reused IDs, write failure, and restart.
Replay complete owner state without removing fields or inventing historical links.
Do not move business packages or create a production migration in this scope.
Complete required checks and clean review. Integrate directly; do not create a PR.
Release only after integration and passing CI, with backups and actual default Codex checks.
Clean only your test request or source IDs. Check claim counts after live tests.
Report measured results and every missing check. This batch does not close Phase 3.9.
Ask the coordinator only when an actual stop condition in AGENTS.md applies.
```
