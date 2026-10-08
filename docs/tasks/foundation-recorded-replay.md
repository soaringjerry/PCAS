# Foundation: Recorded Production-Copy Replay

Executor: Sol. Base revision: `3e1336b`.
The coordinator assigned completion of Phase 3.9 and continued execution in its agreed order.
The released [shared skeleton](foundation-shared-skeleton.md) supplies the first gateway and paid-result recovery path.
This scope builds the second test layer in delivery order before business-domain migration.
The [five test layers](../architecture.md#five-test-layers) remain separate requirements.

## Goal

Record real requests, selected background tasks, and their actual model replies on an isolated production-data copy.
Use those recordings before and after refactoring to compare decisions, receipts, and resulting business data.
A replay cannot call a live provider when a recording is absent or does not match.

## Permitted Changes

| Part | Scope |
|---|---|
| Diagnostics | Add a separate replay mode under `cmd/pcas-eval` or a dedicated script. Keep existing synthetic evaluation contracts unchanged. |
| Copy preparation | Restore an authorized backup into an explicitly owned disposable database. Copy necessary private configuration and files outside Git. |
| Recording | Capture application requests, selected jobs, actual provider input and output, outcomes, usage, and resulting state. |
| Replay | Exercise existing application paths with recorded provider replies. Reuse paid-result storage for application-recovery scenarios. |
| Comparison | Compare owner data, receipts, effects, and invocation selection. Report differences and unavailable coverage explicitly. |
| Documents | Index the scope and acceptance report. Update the milestone state only after evidence exists. |

Use `/root/PCAS-worktrees/foundation-recorded-replay` on branch `foundation/recorded-replay`.
Do not change the shared `/root/PCAS` worktree.
Do not move business-domain packages, change business rules, or add production database migrations in this batch.
Do not change production instructions or output schemas.

## Isolation and Evidence

Use a new database container with an explicit ownership identity and loopback port.
Validate its identity before restore, reset, or cleanup.
Never infer a disposable target from a database name alone.
The production services cannot connect to the copy.
Do not start Telegram polling, connector polling, notification delivery, or an unrestricted background worker on the copy.
Invoke only the operations named in the private replay manifest.
The ordinary `serve` command starts deputy, reminder, notification, Telegram, and connector loops.
Use a diagnostic entry instead of ordinary `serve` or `worker`.

Keep copies, credentials, actual requests, prompts, replies, and state snapshots outside every Git repository.
Use private directory and file modes. Public checks use synthetic cases and summary evidence.
Use the real default Codex channel when capturing model replies.
Replay has no credential requirement or live-provider fallback.

Record the backup fingerprint, revision, owner, timezone, configuration, operation order, input identities, and versions.
Include committed and failed operations. Keep existing defects as recorded cases.
Do not manufacture a missing expected action from an absent event.

## Matching and Comparison

Restore the same initial copy before each side of a comparison.
Keep original request identities and recorded replies in the private manifest.
Verify invocation purpose, registered instructions, schema, model mode, and supplied input before returning a recorded reply.
Missing, duplicate, or unused replies produce explicit failed or incomplete results.

Raw snapshots retain all fields. Comparison rules identify each excluded runtime field and its reason.
Time changes and generated identities must not hide different business decisions, version checks, relationships, deadlines, or writes.
When deterministic matching is unavailable, report that case as incomplete.
Do not replace a strict match with arbitrary text stripping or response lookup by request order alone.

Saved-result replay checks application and recovery. It does not prove unchanged context assembly by itself.
Provider-boundary replay must also exercise and compare the generated inputs.
Keep recorded model resources separate from replay execution resources.
Replay latency does not establish faster model generation or lower production spending.

## Acceptance

- Capture representative real secretary requests and background operations on the owned production copy.
- Preserve actual model replies and inputs with stable private identities.
- Replay both sides from the same starting data without live model calls.
- Compare actions, executed/skipped/failed receipts, dependencies, and affected business data.
- Reject absent recordings, input mismatches, duplicate consumption, and unexpected additional invocations.
- Preserve lease, version, request-idempotency, and paid-result recovery checks.
- Report incomplete scenarios and known business defects without marking them as passed.
- Keep synthetic checks in ordinary CI. Private evidence and model credentials cannot enter fork-triggered CI.

Run the required formatting, code, architecture, and affected storage checks before delivery.
Passing this batch does not close Phase 3.9.
The owner migration, activity query, event limits, and remaining test layers still need evidence.

## Preparation State

The owned database and file copy was restored from the verified shared-skeleton release backup.
Migration `062_model_calls.sql` was applied only to that copy during preparation.
No ordinary API or worker loop was started on the copy.
Actual model capture and before-and-after replay remain incomplete.

Formatting, vet, build, resolved architecture, and documentation checks passed for this scope record.
The runtime source is unchanged from released revision `935ad5b`, which passed complete CI.
Full local commands were not repeated for this documentation-only record.
Earlier interrupted full local attempts remain incomplete, as recorded in the shared-skeleton acceptance.

## Sol Window Prompt

```text
Sol, complete Foundation Recorded Production-Copy Replay in PCAS.
Use /root/PCAS-worktrees/foundation-recorded-replay on foundation/recorded-replay.
Do not switch branches or stage files in /root/PCAS.

Read AGENTS.md, then docs/tasks/foundation-recorded-replay.md and its current links.
The coordinator assigned completion of Phase 3.9 in the agreed order.
The first shared background skeleton is released; Phase 3.9 is still incomplete.

Implement only the permitted replay and diagnostic scope.
Use an explicitly owned disposable production-data copy and private artifacts outside Git.
Keep existing synthetic evaluation contracts unchanged.
Do not move domain packages or change production prompts, schemas, or business rules.
Do not add a production migration in this batch.

Record real requests, selected background operations, and actual default Codex replies.
Replay through existing application paths with no live-provider fallback.
Validate input and response identities before returning recorded output.
Restore the same initial data for each comparison.
Compare actions, receipts, dependencies, and resulting business data.
Keep raw snapshots; document runtime-field comparison rules and incomplete coverage.
Do not count saved-result application replay as proof of unchanged input assembly.

Run meaningful synthetic, architecture, and affected storage checks.
Keep private datasets and real-model credentials outside fork-triggered CI.
Report actual evidence, failures, and remaining requirements.
Write code, commits, and specifications in English. Communicate in Chinese.
Index every document. Stage only assigned files and follow direct integration after clean CI.
```
