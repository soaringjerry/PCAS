# Foundation Shared Model Skeleton

Recorded 2026-10-08. Executor: Sol. Base revision: `ee41212`.
The [assigned scope](../tasks/foundation-shared-skeleton.md) defines this batch.
The coordinator approved the call metadata table and migration before implementation.

## Behavior

Existing `generatePaid` background callers now use `internal/modelcall` for invocation, paid-result recovery, and accounting coordination.
Explicit interfaces separate providers, accounting, saved results, and lifecycle records.
The gateway does not import concrete PostgreSQL storage or apply business changes.
The temporary job adapter retains existing selection, application, retry budgets, and transaction fences.

Registered background instructions are embedded in `internal/prompts`.
Shared extraction fragments have one copy. Final composed instructions have stable names and hashes.
The gateway pins the selected provider and its accounting rates.
Configuration changes during reservation cannot change the actual model behind the recorded identity.

Migration `062_model_calls.sql` adds invocation metadata.
It does not overwrite existing data or duplicate the token and cost ledger.
Reservation links commit atomically with reservations.
Returned or partial output commits atomically with its lifecycle status.
Accounting recovery uses the original paid result, invocation, and reservation.
Unknown outcomes remain visible and cannot silently cause another paid call.

## Real Model Baseline

Use the existing `TestOrganizeLiveSynthetic` scenario in an isolated database.
The scenario seeds sixty fictional memories and uses the configured default Codex provider, `gpt-6.1-sol`.
Requests, outputs, configuration, and comparison artifacts remain private outside Git.
No production memory or queue record is used as scenario input.

| Observation | Before | After |
|---|---|---|
| Completed processing | 60 of 60 memories | 60 of 60 memories |
| Model calls and completed turns | 2 | 2 |
| Generated project label | `流萤` | `流萤项目` |
| Whole scenario duration | 50.25 seconds | 56.40 seconds |

The project labels differ. Do not claim identical generated content.
These durations include scenario preparation. They do not establish a model-latency trend or a gateway performance effect.
One scenario does not establish project quality, recall quality, or production cost improvement.

The migration artifact compares eleven registered instruction values with their compiled original constants.
All values match exactly, including whitespace and shared fragments.
Captured thread requests also have identical base instructions, developer instructions, model, tool, and sandbox settings.
Synthetic identities and generated vocabulary differ between runs. The complete context payloads are not byte-identical.
The workflows' context builders and output formats are unchanged.

Provider token notices remain in the private artifacts.
Configured subscription accounting prices are estimates, not invoices.
No cost or token reduction is claimed.

## Checks Run

The targeted PostgreSQL checks passed with the race detector:

- A call record survives business application and keeps its unique usage link.
- Accounting failure followed by restart reuses the paid result without a provider.
- A started invocation without a response does not start another paid call.
- Failed-call accounting recovery does not apply partial output.
- Disconnect and cancellation keep usage and an unknown outcome without another generation.
- Legacy paid receipts recover without invented historical call metadata.
- Existing organizer rollback recovery and comparison failure accounting remain valid.

The resolved provider-boundary check passed.
It rejects new application provider entry points and stale migration exceptions.
The provider selection check verifies the transmitted model and charged rates after configuration changes.
Full command results and delivery status are recorded below when those checks finish.

## Checks Not Complete

The first complete-check attempt used one database for incompatible test requirements.
Evaluation tests required an empty database; storage tests required a migrated database.
That test container also exited during the run.
The interrupted attempt is not a pass. Its private log is retained.
Replacement checks separate the normal check environment from the migrated integration database.

CI and deployment remain pending at this record's preparation.
The batch has not yet supplied a live secretary check or a release backup record.

## Uncertain and Remaining

Secretary, deputy, embedding, media, Telegram transcription, routing, and diagnostic calls remain migration exceptions.
Business workflows remain in PostgreSQL. Domain-owner write boundaries are not yet enforced across the repository.
The local job root does not supply the complete upstream event chain.

Source manifests, selection coverage, and pre-model exclusions still need their complete workflow migration.
Missing expected project creation cannot yet be explained from call records alone.
Central event responses, chain limits, and the unified daily activity query remain outstanding.

Legacy saved results recover without fabricated prompt or causal metadata.
An interrupted call without a saved result can require investigation.
No new recovery interface or automatic accounting repair service is included.
This batch does not repair the reported project, timeline, or task defects.
It does not close Phase 3.9 or Phase 4 acceptance.
