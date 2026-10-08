# Foundation: Shared Model Skeleton

Executor: Sol. Base revision: `ee41212`.

The user assigned foundation execution in the agreed order.
The [architecture](../architecture.md) and [existing-path inventory](../evaluations/2026-10-08-foundation-inventory.md) supply its direction and code evidence.
This scope covers the shared skeleton. Business-domain package extraction follows the completed pilot.
The coordinator approved this batch's call metadata table and database migration after reviewing the scope and SQL draft.
This approval supplies the confirmation required by [Executor Rules section 9](../../AGENTS.md#9-stop-and-ask).

## Goal

Move the common background provider call, accounting, and paid-result mechanics behind a gateway.
Register its existing instructions without changing the final request bytes.
Give calls durable identities, input references, prompt hashes, outcomes, and links to existing usage and result receipts.
Keep business decisions, dependency validation, queue policy, and application transactions in their responsible paths.

## Permitted Changes

| Part | Scope |
|---|---|
| Prompt registry | Add `internal/prompts` with embedded instructions, stable names, and content hashes. Keep shared fragments authoritative in one place. |
| Gateway | Add one application entry and explicit provider, accounting, and saved-result interfaces. The gateway cannot import concrete PostgreSQL storage. |
| Background integration | Replace the provider and accounting mechanics in `generatePaid`. Keep an adapter for its named domain callers until their later migrations. |
| Persistence | Add call-lifecycle metadata after migration confirmation. Use existing usage, reservations, and paid-result storage for their current purposes. |
| Wiring | Construct the gateway through application setup or the existing `SetModels` integration boundary during migration. |
| Checks | Verify original requests, cancellation, accounting failure, paid-result reuse, restart, and dependency changes. Record remaining provider entry points explicitly. |
| Documents | Update service paths, status, terminology, and the indexed acceptance record. |

Do not move domain packages in this batch.
Do not change project eligibility, timeline interpretation, task semantics, prompts, output schemas, or existing daily and stage budgets.

The coordinator approved bounded automatic recovery for unknown outcomes after reviewing the alternatives.
Apply the [recovery contract](../architecture.md#5-model-gateway). This decision replaces the initial unconditional recovery block.

Do not restore an intelligent event bus, policy router, or another queue.
Do not retire callable legacy interfaces without their supported-client check.

## Call Record Proposal

Existing `model_usage` stores returned usage. Its token and cost fields are mandatory.
It does not store started calls, prompt identity, actual capability mode, or causal execution identity.
`background_model_results` stores recoverable output per job. Workflows delete that result after application.
Neither table alone can supply durable call-lifecycle evidence without changing its current meaning.

The proposed `model_calls` table stores lifecycle metadata, not another token or cost ledger.
It uses a stable invocation ID and links to `model_usage` and existing reservation/result identities.
The draft has no source text, prompt text, credentials, or duplicated charge fields.
It does not overwrite old rows or fabricate metadata for historical calls.

| Record group | Proposed fields and purpose |
|---|---|
| Identity | Owner, invocation, execution, root execution, immediate cause, function, and stage. |
| Instructions | Registered prompt name and instruction hash. Optional output schema name and hash. |
| Input | Versioned manifest and context-builder version. Explicit absent information for legacy recovery. |
| Provider | Provider and model, required capabilities, and actual mode. |
| Lifecycle | Prepared, started, returned, failed, or unknown; stable error code and lifecycle times. |
| Accounting and recovery | Reservation ID, usage ID, saved-result reference, and accounting state. |

Actual tokens and costs stay authoritative in `model_usage`.
A lifecycle record without usage has unknown resources. Do not report an asserted zero.
Reservation totals must not be added to usage costs.
Billing and invocation identities survive source deletion without retaining source text.

The additive SQL draft is prepared outside production migrations for coordinator review.
The recorded approval covers this table and its schema migration, including isolated checks before an authorized release.
The event delivery schema and centralized response implementation are outside this batch.
The gateway still carries causal identities so those later paths can preserve one root chain.

## Guarantees and Replacement

Keep model calls outside transactions.
Save paid output before retryable business application. Retry recording or application without repeating generation.
Keep original input and invocation identities on recovery.

An interrupted provider call can have an unknown outcome. Do not classify it as success.
Use bounded automatic recovery through the existing queue, with separate invocation records and explicit recovery links.
Pause after the recovery attempt limit or an exhausted recovery budget.
Keep accounting independent of caller cancellation and business transaction failure.
Record failed paid attempts and unresolved accounting states.

The temporary `generatePaid` adapter serves its existing background domain callers.
Its removal condition is migration of those callers to the gateway's declared request and receipt interfaces.
Sol owns this adapter during the foundation.
Unmigrated secretary, deputy, embedding, media, channel, routing, and diagnostic calls remain explicit coverage gaps.
The skeleton does not close gateway acceptance until those paths have migrated.

## Baseline and Checks

Before integration, capture the unchanged path's request, output, usage, duration, and applied state on isolated data.
Use the real default Codex channel for the representative model baseline.
Keep the configuration, input versions, model, timezone, and operation sequence for the after comparison.
Private requests and outputs stay outside Git.
Synthetic scenarios must not write to production memory or the production queue.

Verify that prompt extraction preserves exact instruction bytes, whitespace, context ordering, and output schema ordering.
Use a migration comparison artifact. Do not add permanent prompt-wording tests or duplicate the earlier implementation.
Keep meaningful paid-result, cancellation, accounting, version, lease, and replay tests.
Architecture checks use resolved dependencies and explicit migration exceptions.

The [five test layers](../architecture.md#five-test-layers) must all be completed within Phase 3.9.
This skeleton does not supply complete owner contracts, production-copy replay, or nightly evaluation.
Build recorded replay before the next business-domain migration.

Run the applicable code, PostgreSQL, and real-model checks before integration.
Record each skipped or incomplete check and its reason.
Passing mocked output alone does not establish model quality.

## Delivery

Report the final behavior, checks, incomplete checks, uncertainties, and remaining migration paths.
Use English code and commit messages. Communicate in Chinese.
Use the executor's isolated worktree and stage only its assigned files.
Follow the user's direct integration preference after clean review and passing checks.

Before an authorized runtime release, complete backup and live default-channel checks under the workflow.
This scope does not claim that reported project, timeline, or task defects are repaired.

## Sol Window Prompt

Copy this complete assignment into Sol's execution window:

```text
Sol, implement the Foundation Shared Model Skeleton in PCAS.
Use your isolated worktree at /root/PCAS-worktrees/docs-english-ste100.
Do not switch branches or stage files in the shared /root/PCAS directory.

Read AGENTS.md first. Then read docs/tasks/foundation-shared-skeleton.md.
Read its current specification and inventory links before implementation.
The coordinator approved the model_calls table and its additive migration.
Do not request that approval again.

Follow the scope's permitted changes, guarantees, baseline checks, and delivery conditions.
Capture the unchanged real Codex baseline on isolated data before integration.
Keep actual instructions and output formats unchanged.
Implement the prompt registry, gateway interfaces, and bounded background integration.
Use existing budget, usage, saved-result, and queue mechanisms.
Apply the coordinator-approved bounded recovery contract for unknown outcomes.
Record every attempt and pause at the attempt or recovery budget limit.
Do not move business-domain packages in this batch.

Keep data-owner, visible-outcome, and event constraints from docs/architecture.md.
List incomplete input coverage, upstream causes, and remaining provider paths explicitly.
Do not claim that model-call records explain all missing project creation.

Do not restore an intelligent event bus or create another queue.

Run the required code, storage, and real-model checks.
Complete all five test layers within Phase 3.9 under docs/architecture.md.
Build architecture checks first and production-copy replay second.
Keep real-model credentials outside fork-triggered CI.
Report incomplete checks and observed output differences honestly.
Keep credentials, actual prompts, and raw model output outside Git.
Communicate in Chinese. Write code, commits, and current specifications in English.

Review the final diff and stage only assigned files.
Use the coordinator's direct integration preference after clean checks.
Verify CI for the exact committed revision before runtime release.
Back up with pg_dump, verify the backup, and follow the documented release checks.
Test the live secretary on the default Codex channel and show errors.
Clean only your test records by request or source identity and examine claim counts.
```
