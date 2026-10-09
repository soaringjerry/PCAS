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
| Main generation is ready for submission. | Save its original private input and started journal in one transaction before the provider call. |
| The private input cannot be stored. | Prevent provider submission. Release the unused admission hold and record the failure without fabricated usage. |
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
Keep the existing instruction bytes, brief construction, search mode, and applicable model deadlines.
Use the approved business clock for deputy date interpretation, retrieval activity decay, and deadline display classification.
The deadline expiry filter must use the same classification time.
Keep source validity, access, leases, quotas, and accounting on actual time.
Production without a diagnostic clock retains database transaction time for deadline classification.
Record new ordinary and revision baselines with the same fixed business time before gateway migration.
Preserve the earlier recordings and their failed replay evidence.
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
| Main generation and application | Ordinary generation and revision now use the gateway in this branch. Known-result recovery and exact application links are implemented. Complete CI passes. Final replay and release remain outstanding. |
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

The original business clock did not fix this retrieval input.
The coordinator approved its extension on 2026-10-09 UTC.
The approved scope fixes deputy date interpretation and retrieval decay on the diagnostic copy.
Production keeps actual time by default. Leases, access, quotas, and accounting continue to use actual time.
The extension is implemented. Filtered repository and integration checks pass with pgvector.
They check lexical and fusion ordering, future applicability, withdrawn access, and actual admission time.
The first new fixture lacked required evidence and failed. The corrected fixture keeps the same assertions.
New ordinary and revision recordings completed through actual Codex.
The revision applied exactly the requested edit. Its registered output schema matches the original bytes.
Both recordings passed strict offline transport replay with unchanged inputs, memory references, and output.
Replay started no live provider. Claim counts remain unchanged in each recording and replay.
The recorded runtime files match `892b2b3`. Private manifests identify the complete captured source tree.
Keep the original failed evidence after this repair.

Complete owner snapshots and every raw field difference remain private.
The comparison retains execution times, generated identities, and unpaired rows. It does not certify complete state equivalence.
Ordinary overflow events also differ in insertion order because their existing writer iterates a map.
Their stage, outcome, reason, and count multisets match. Their original identity and time differences remain in the report.
Do not remove these fields or claim that every remaining difference has been resolved.

One private snapshot archive was interrupted with exit 137. Its complete original snapshot survived.
Serial archival later verified every original byte before removing the extra compressed copy.
Archives, failed replay evidence, prompts, and raw outputs remain outside Git.

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
[Complete CI for `c02617e`](https://github.com/soaringjerry/PCAS/actions/runs/37916251593) passed every job.
That revision certifies result retention. It does not certify the later input-receipt changes.

### Input Receipt Preparation

The main-generation port saves private input under the invocation ID before provider submission.
It uses the existing private result relation. The journal remains free of private prompt text.
The input receipt and started journal commit in one transaction after the current lease and input checks.
An input-only receipt has purpose `deputy_input`. It is not a returned result or a usage record.
Its required numeric fields are placeholders. They do not assert actual spending or complete usage.
The shared result reader excludes this receipt type.

A returned response completes that same receipt after its invocation and original input identities match.
Deletion or lost access removes the private input instead of restoring inaccessible content.
Interrupted processing keeps its unknown outcome and admission hold. Input alone cannot authorize application.
The main worker still uses its original generation path. Complete migration and final replay remain outstanding.

Final filtered `make check` and `make test-integration` pass after the shared-reader exclusion.
They cover input durability, failed input writes, interruption, and related gateway contracts.
[Complete CI for `d0269cc`](https://github.com/soaringjerry/PCAS/actions/runs/37919071697) passed every job.
That revision certifies input receipts. It does not certify the later business-clock extension.
[Complete CI for `892b2b3`](https://github.com/soaringjerry/PCAS/actions/runs/37920723056) passed every job.
That revision certifies the business-clock extension. Main deputy gateway migration and final batch acceptance remain outstanding.

### Main Generation Migration

Ordinary generation and revision use the existing gateway with their original instructions, schema, brief, search mode, and deadlines.
The request contains the original run and coverage snapshot. The provider receives only the original brief.
Recovery uses that snapshot instead of constructing another prompt from current memory.

The gateway reuses the admitted reservation and records usage once.
Unknown outcomes retain the hold. Required capabilities cannot silently fall back.
An unsupported capability fails before submission and has a visible run error.
An empty, unmeasured ordinary response keeps the original usage-record guard.

Before business writes, ordinary generation saves the result-selection identities and output hash.
This record distinguishes the original draft from the accepted self-check result.
A new process restores a saved decision without another model call.
A review without a saved decision remains incomplete. Recovery keeps the draft and records the visible fallback.
A failed selection write retains the decision in the current process for persistence recovery.

Application records use the original invocation, owner, run, and creation origin.
Interrupted input without a returned response receives an inapplicable application record after its owner closes the execution.
A saved unknown billing receipt keeps its original provider outcome as the application reason.
The first interruption-reason assertion expected the same reason for both cases and failed.
Separate contracts now check abandoned input and saved unknown billing. Filtered repository and integration checks pass for both.
They identify applied, skipped, or inapplicable results and the existing adoption receipt.
Legacy revision caches keep their recovery path without fabricated invocation metadata.
The obsolete cache writer is removed. New revisions use invocation-keyed results.

The recovery contracts inject failures into selection and business writes.
They check ordinary output, accepted and rejected review, process restart, revision, interrupted calls, and unsupported search.
Initial fixture defects remain in the private check records.
An empty provider response tested provider failure instead of structural rejection. The fixture now returns a valid checklist with mismatched item count.
A cancellation fixture waited on an unread HTTP connection. Captured stacks identified that fixture wait; its handler now returns after cancellation.
Final filtered repository and pgvector integration checks pass for main migration.
Additional checks found an old deputy cancellation assertion that treated partial usage as complete spending.
The deputy now uses the existing extraction contract for unknown outcomes, incomplete usage, and retained reservation.
The asynchronous correction fixture uses the synthetic gateway instead of a provider without required search capability.
These fixture changes keep correction, usage, and budget assertions. Their filtered repository and pgvector integration checks pass.
They include architecture, deputy, revision, adoption, undo, and interactive accounting contracts.
The ordinary recording rejects the migrated replay because one displayed deadline state changed from upcoming to expired.
The input difference is confined to that state label. Original prompts and failed replay evidence remain private.
The original `libraryDeadlinesTx` calculated this label with actual database time. The diagnostic business clock did not fix that calculation.
The coordinator approved the additional deadline classification clock on 2026-10-09 UTC.
This extension applies only when the diagnostic clock is configured.
It must not change source validity or permit expired execution rights.
The extension is implemented. Filtered repository and pgvector integration checks pass.
They check expiry labels, expiry filtering, recurring dates, unclear dates, and restoration of the default clock.
Future and withdrawn source evidence remains unavailable under the diagnostic clock.
The first fixture lacked its required synthetic role and failed. The corrected fixture keeps the same assertions.
Final transport acceptance remains outstanding.
The failed replay started no live provider. It cannot certify successful generation or complete state equivalence.
[Complete CI for `8088abc`](https://github.com/soaringjerry/PCAS/actions/runs/37934650717) passed every job.
[Complete CI for `acfb01a`](https://github.com/soaringjerry/PCAS/actions/runs/37936200710) also passed every job, including each storage partition.
The earlier `c78f3d4` run was superseded and cancelled. It does not certify the complete suite.
The unmodified pre-migration program now rejects the same recording.
Its rejected provider input equals the migrated program's rejected input exactly.
This control establishes a temporal replay difference, not a prompt change introduced by the gateway.
Both failed replay reports, original inputs, and complete raw owner snapshots remain private.
Each failed case started no live provider. Claim counts remain unchanged. Their owned disposable clones were removed after snapshot verification.
Main migration has not been released. Final transport replay, real-channel acceptance, and complete state comparison remain outstanding.
Final checks and replay evidence must identify the tested source revision.
Do not claim complete CI, release, or full state equivalence from filtered checks.

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
Keep the original instructions, schema bytes, brief construction, and applicable deadlines.
Use the approved business clock for deputy dates, retrieval decay, and deadline display classification.
Use that classification time for the deadline expiry filter. Keep execution clocks actual.
Record new fixed-clock ordinary and revision baselines before gateway migration.
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
