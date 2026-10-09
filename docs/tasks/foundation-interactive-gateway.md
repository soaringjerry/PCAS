# Foundation: Interactive Gateway

Status: assigned by coordinator approval on 2026-10-09. Base: `b39a9eab41a1b8015c4f9fdd016582fce322b09f`.
Executor: Sol. Implement the complete delivery order below.
The approval assigns this gateway extension separately from the recorded-replay scope.
Implementation and acceptance evidence remain incomplete.

## Goal

Bring secretary generation, heavy memory readers, and their self-checks through the existing gateway.
Link each invocation to its budget reservation, measured usage, saved result, and request or run.
Keep business decisions separate from this migration.

## Verified Starting Points

| Part | Current path | Consequence |
|---|---|---|
| Background gateway | `internal/modelcall/gateway.go` and `internal/postgres/model_calls.go` | Provider selection and storage adapters currently assume an extraction job. |
| Secretary generation | `internal/postgres/desk_model_retry.go` | A reservation exists, but the usage row does not use its identity. |
| Readers and self-check | `internal/postgres/memory_use_model.go` | Generation, accounting, and cancellation have a separate wrapper. |
| Reader callers | `internal/postgres/memory_use_heavy.go` | Selection and page reads need separate stage identities. |
| Deputy self-check | `internal/postgres/runs.go` | Its shared `useModelCall` caller must migrate with the wrapper. Main deputy generation remains a later scope. |
| Output modes | `internal/ai/provider.go` | Non-Codex schema and search requests currently become ordinary generation without an explicit outcome. |
| Result storage | Migration `042_library_sources.sql` | `background_model_results.job_id` is a primary key without a job foreign key. Deputy revision already uses a run ID there. |
| Call metadata | Migration `062_model_calls.sql` | Existing fields can describe executions, schemas, modes, accounting, and result references. |
| Secretary admission | `internal/postgres/desk_turn_order.go` and migration `021_desk_turn_order.sql` | Request identity and conversation order already have committed admission records. |

The background adapter's `request.Policy.(worker.Job)` cannot represent an interactive request.
Do not create a fictional job to reuse that assertion.
Do not infer reservation correspondence from equal costs or similar timestamps.

## Delivery Order

1. Extend the existing gateway contract for explicit provider selection, schema identity, and search mode.
2. Register the migrated instruction components and output schemas without changing their bytes or field order.
3. Add an interactive journal, accounting, and result adapter behind the same gateway interfaces.
4. Migrate heavy readers, secretary self-check, and deputy self-check with their shared wrapper.
5. Migrate secretary generation and keep its applicable deadline and retry policy outside the gateway.
6. Replay the recorded cases, add state and failure contracts, remove the migrated direct-call exceptions, and release after passing checks.

No domain package moves occur in this scope.
No project, task, timeline, memory-selection, or semantic decision rule is repaired here.

### Implementation State

The first implementation extends the existing gateway request and provider adapter.
It declares provider selection, registered schema, search requirements, and context-builder version.
Unsupported required capabilities get a failed call record before reservation or invocation.
The background default keeps ordinary generation. Existing interactive callers still use their legacy wrappers.

Four output schemas now use registered, immutable content and hashes.
Their bytes and field order match the preceding source. Instruction migration remains pending.
The interactive journal, caller migration, complete input binding, replay acceptance, and release remain pending.

| Local check | Observed result |
|---|---|
| Provider and gateway contracts | Required-mode rejection, selected-provider retention, mode isolation, and paid-result persistence recovery passed. |
| Schema contracts | Registry mutation protection and secretary action-field order passed. |
| PostgreSQL checks | Required-mode failure without spending and successful Codex mode records passed with synthetic models. Existing call accounting and restart-recovery checks passed. |
| Build and architecture | Formatting, vet, build, resolved provider boundaries, and affected package race checks passed. |

Database checks used a new empty database on the verified owned copy container.
No production data was copied into this test database. The existing replay copy remained unchanged.
Two initial fixture failures were retained in private logs: an unavailable fixture principal and an unset extraction-provider reference.
The corrected fixture uses its declared memory principal and explicit Codex provider. The same checks then passed.

The complete serial local `make check` and `make test-integration` were not repeated.
Previous full serial runs exceeded the local scale-fixture time or storage limits.
Targeted database and package checks do not replace complete exact-revision CI.
CI, real default-channel acceptance, and deployment of this extension are not yet certified.

## Call Contract

Extend `modelcall.Request` instead of adding another application model wrapper.
Keep the current background request valid and preserve its provider payload.
The new request declares the selected provider, registered schema, search mode, context-builder version, and input references.
Select one immutable provider configuration before reservation and invocation.
Required capabilities and actual modes must agree.

Keep model adapters under the existing `internal/ai` boundary.
Add mode support to that provider boundary without permitting application callbacks to invoke models elsewhere.
Unsupported required schema or search capability returns an explicit recorded failure before submission.
Do not implement a new provider protocol or silently select another model in this batch.
Existing optional modes need a declared fallback contract before they can be used.

Registered instructions preserve the final existing bytes, including self-check suffixes and newlines.
Schema registration preserves field order and the secretary skip-action schema.
Inspect actual Codex requests and raw replies on the isolated copy before accepting the provider-mode change.

## Accounting and Recovery

Use `model_calls` for invocation metadata, `model_usage` for usage, and `background_usage` for budget reservations.
Use an explicit relation from the call to its reservation and usage. Preserve reader selection IDs used by existing consumers.
Do not count reservations as additional spending.
Retain unknown or partial cost as unknown or estimated rather than reporting a free call.

Use existing saved-result storage before introducing another table.
For new interactive results, use the invocation ID as the stored result key and record that key in `model_calls.result_receipt`.
The legacy `job_id` column is only a storage-key name for those results. Do not create or claim a queue job.
Legacy background and deputy revision rows keep their existing keys and readers.
Remove the legacy column name only in a separately approved storage migration.

Validate the full original input binding before reuse: execution, stage, instructions, schema, provider, mode, source and memory versions, and payload hash.
A request can contain several stages and several reader pages. They cannot share one mutable result slot.
A changed input cannot receive a cached response.
Save results before retrying accounting or application. Persistence failures cannot launch another paid call.
Caller cancellation cannot cancel the accounting record.
Restart and late-response handling preserve the invocation identity and applicable execution fence.

Secretary execution identity follows its admitted request rather than a regenerated presentation-turn ID.
Preserve request idempotency, conversation order, source access checks, and current dependency verification.
Any link that cannot survive restart remains an acceptance failure, not fabricated history.

Keep the existing secretary transient retry policy: two attempts, one 30-second deadline, and a 500-millisecond wait.
Do not introduce a five-attempt interactive loop merely because the background recovery ceiling is five.
An unknown interactive outcome retains its reservation and visible state.
Its replacement policy needs a concrete scope decision if the existing transient policy cannot safely express it.

## Visible Outcomes

Record and return unsupported capabilities, failed stages, and authorized fallback outcomes explicitly.
Use the current response notice and receipt mechanisms where applicable. Do not invent a new public response schema.
A failed self-check cannot masquerade as a completed check.
A successful fallback reply and its failed original stage remain separate facts.

## Checks and Acceptance

| Check | Required result |
|---|---|
| Architecture | The migrated provider entry points disappear from the exception list. Existing declared boundaries remain checked. |
| Exact input binding | Existing default Codex instructions, context order, search mode, and output schema remain unchanged. |
| Saved results | A result-write or accounting failure retries persistence without a new invocation. Restart can recover the result. |
| Accounting | Reservation, call, usage, and settlement links remain exact under duplicate requests and accounting retries. |
| Concurrency | Reader pages do not share result slots. Late responses cannot replace another execution's result. |
| State | Conversation order, version conflicts, source revocation, request replay, cancellation, and undo show no migration regression. |
| Capabilities | An unsupported required mode makes no provider call and returns a visible failure. |
| Replay | The fixed-clock secretary case and extraction case retain strict input matching and zero live calls. All state differences remain reported. |
| Real model | Default Codex acceptance uses private copy evidence and representative requests. Synthetic checks cannot replace it. |
| Delivery | Required local checks, exact-revision CI, backup, release, real default-channel smoke, and own-request cleanup have recorded evidence. |

Keep known project, timeline, task, and accounting defects separate from migration regressions.
This scope does not close Phase 3.9.
Full deputy generation, multimodal calls, query embeddings, Jev, connection tests, owners, activity queries, events, and nightly evaluation remain later work.

## Assignment Boundaries

Approval assigns this explicit extension of the existing gateway and registered prompts.
It does not authorize a database migration, package extraction, changed instruction meaning, or a new public output schema.
If existing storage cannot support the stated recovery guarantees, prepare its concrete migration and return to the coordinator.
Before an unknown-outcome policy changes, present its actual deadline, counters, overflow path, and accounting effect.

## Complete Sol Window Prompt

```text
Sol, implement the approved Interactive Gateway scope in PCAS.
Use /root/PCAS-worktrees/foundation-recorded-replay on foundation/recorded-replay.
Do not switch or stage files in /root/PCAS.

Read AGENTS.md, the approved scope, docs/architecture.md, docs/tasks/process.md,
docs/status.md, and the registered replay evidence before implementation.
Refresh actual definitions, callers, migrations, and existing tests before edits.

Extend the existing internal/modelcall gateway. Do not create a second gateway.
Register migrated instruction components and schemas with exact byte preservation.
Keep output schema structure and field order.
Migrate heavy readers, secretary self-check, deputy self-check, and secretary generation.
Keep complete request, stage, provider, input, result, reservation, and usage links.
Reuse existing storage with explicitly typed result keys; do not fabricate queue jobs.
Preserve saved-result recovery, cancellation accounting, conversation order,
access checks, version checks, and request idempotency.
Make unsupported capabilities and authorized fallbacks visible.

Do not move business packages or repair semantic project, task, or timeline rules.
Do not add a production migration or change instruction meaning without a new scope approval.
Keep known defects in private recorded cases and public summary evidence.
Run strict before-and-after replay and the affected architecture and storage contracts.
Use actual default Codex on the isolated copy for provider-mode acceptance.
Keep credentials, prompts, replies, and production copies outside Git.

Update and index current documentation in English under the writing guide.
Write code and commits in English. Communicate in Chinese.
Integrate directly after clean review and CI. Back up before release.
Test the live default Codex secretary, inspect errors, and clean only your own IDs.
Do not mark Phase 3.9 complete after this batch.
```
