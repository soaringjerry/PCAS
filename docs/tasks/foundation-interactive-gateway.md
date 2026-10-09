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
The background default keeps ordinary generation. Deputy self-check now uses the gateway.
Secretary and reader calls still use their legacy wrappers.

Four output schemas now use registered, immutable content and hashes.
Their bytes and field order match the preceding source.
The assigned fixed instructions and shared components also use registered assets with unchanged bytes.
The interactive storage adapter and input binding now have an initial implementation.
Caller migration, complete recovery acceptance, replay, and release remain pending.

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
The [complete foundation CI](https://github.com/soaringjerry/PCAS/actions/runs/37877218889) passed all 13 jobs at `eae0260`.
That revision covers the mode contract and schema registration. It does not cover the subsequent instruction transfer.
Real default-channel acceptance and deployment of this extension remain pending.

### Instruction Transfer

Secretary, reader, secretary self-check, and deputy self-check instructions now come from the registry.
Shared assistant and memory-trust components have one copy. Registry composition preserves the complete original input.
Legacy desk and document revision aliases retain their shared instruction dependencies without changing their provider paths.
The compiled registry hashes match every preceding instruction byte, including separators and self-check suffixes.

Formatting, vet, build, registry and provider race checks, resolved architecture checks, and targeted database checks passed.
The database checks also covered secretary transient retries, deadline preservation, failure classification, and request idempotency.
The [complete instruction-transfer CI](https://github.com/soaringjerry/PCAS/actions/runs/37878140730) passed all 13 jobs at `6a78a0d`.

### Interactive Storage Adapter

The existing gateway now selects storage adapters for leased background jobs or interactive executions.
The interactive policy identifies an admitted secretary request or an existing deputy lease.
The adapter does not create queue jobs. The candidate deputy self-check is its first application caller.

Each interactive invocation has its own result key, reservation, usage link, and safe failure metadata.
Input binding includes execution, stage, provider, instruction, schema, mode, context version, payload hash, and references.
Recovered accounting uses the original usage identity, presentation turn, tier, plan, and time.
The adapter does not need an available provider to recover a stored result.
Recovery validates the originally recorded provider and model. It does not replace them with current settings.

Secretary admission checks read the committed fence without acquiring its long-held transaction lock.
Late or invalidated results finish accounting before application rejection.
The existing source-deletion transaction removes interactive bodies through their storage owner.
It preserves accounting metadata and marks the input deleted. A later response cannot restore those bodies.

Unknown results retain their reservations and safe provider error classification.
Only an explicit, applicable secretary transient retry can replace an invocation.
The counter keeps the existing two-attempt limit. Reader and self-check stages have no new retry allowance.
The caller still owns the shared deadline and retry wait.

Synthetic database checks cover independent concurrent pages, admission-lock compatibility, input changes, and accounting recovery after restart.
They also cover result-write recovery, deleted inputs, late responses, and bounded unknown-outcome retry.
Existing background recovery checks still pass. Package race checks, formatting, vet, and build remain required before delivery.

The first unknown-outcome fixture omitted its required output schema.
Its protocol helper then failed before producing the requested model failure.
The private log retains that finding. The corrected fixture uses the actual secretary output mode; the checks then passed.

A later check found that JSON `null` in an unavailable receipt was incorrectly compared with the original payload.
Recovery now clears that body-free marker before input validation. The deletion contract verifies the rejection and retained accounting.

The [complete initial-adapter CI](https://github.com/soaringjerry/PCAS/actions/runs/37880846484) passed at `14d4b67`.
That revision does not include the worker accounting continuation below.
Migrated deputy and secretary state checks, strict replay, and real default-channel acceptance remain outstanding.
This adapter implementation does not establish complete recovery after every possible process crash.
A process can lose an unsaved response during a database outage; its started invocation must not be silently submitted again.

### Accounting Continuation

Status: candidate implementation. Complete continuation CI passed. Caller migration and complete interactive acceptance remain pending.
The existing deputy worker now continues interactive persistence and accounting without a repeated user request.
Recovery can save a retained response or finish an existing billing receipt. It cannot invoke a provider or apply business results.

| Sequence | Required result |
|---|---|
| Result storage fails, then the caller leaves. | The worker saves the retained response and finishes its original accounting. |
| Accounting fails after result storage, then the process restarts. | The worker uses the existing receipt without a provider or original body. |
| Input deletion occurs before accounting recovery. | Billing completes without restoring private bodies or applying the result. |
| An execution ends while its call remains prepared. | Record that the provider did not start. Release any linked reservation. |
| An execution ends while its call remains started. | Record an unknown outcome and retain its reservation. Do not submit a replacement. |
| A response arrives after interruption. | Keep the interruption evidence, record the returned usage, and reject business application. |
| Recovery repeats or runs concurrently. | Preserve one usage row, the original identities, and the existing invocation counter. |

Recovery uses the existing worker's single-item scheduling discipline and the existing five-second persistence boundary.
Remaining work stays pending in its cache or durable records. No item is dropped because another item was selected.
Recovery results expose pending counts and identify whether those counts are complete.
Background-job and interactive recovery policies remain separate.

Synthetic contracts verify actual worker recovery after restart, input deletion, canceled persistence, and interrupted execution classification.
They also verify late-response accounting, repeated recovery, and rejection after a deputy lease changes.
An initial late-response check found that the old held state prevented settlement of returned usage.
The adapter now returns that receipt to pending accounting. The same contract then passed.
These checks use synthetic providers and an empty owned test database. They do not establish live caller acceptance.

The first complete continuation CI at `cd59876` failed one existing source-budget sequence with an unexpected request-body EOF.
The other 12 jobs passed. The failing sequence passed ten local repetitions on both `14d4b67` and the current candidate.
The repeated job passed without changes to its assertions or error reporting. Its initial failure cause remains unconfirmed.
The [complete continuation CI](https://github.com/soaringjerry/PCAS/actions/runs/37883992486) passed at `cd59876`.
That result does not cover the later deputy self-check migration.

### Deputy Self-Check Migration

Deputy self-check now calls the existing gateway with its original run, lease token, agent, item, and memory versions.
The registered instructions, original draft context, ordinary output mode, and shared deadline remain unchanged.
Its call record links the reservation and usage to that run.
The workflow records application separately from provider success and accounting.

If self-check fails or accounting remains pending, the deputy keeps the original draft.
If returned output is empty or changes checklist authority, the deputy also keeps the original draft.
The existing result notice identifies incomplete self-check or rejected output. The stage record retains the failure reason.
These notices do not change the draft body or authorize extra actions.
Stored accounting can continue after the run finishes, without another provider call.

Synthetic contracts cover accepted checks, provider failures, empty output, and interrupted accounting.
Existing deputy tier, context, result notice, and interactive recovery contracts also pass.
An initial fallback check rejected an unsupported event status. The implementation now uses the existing failure status.
Complete deputy self-check CI failed at `2e83fb7`. Its parallel-reader case exhausted the check deadline before model submission.
The other 12 jobs passed. Candidate dependency checks now read references in batches, with the same access and version rules.
The original self-check deadline remains unchanged. Complete candidate CI and real default-channel acceptance remain pending.
Secretary generation and full deputy generation still need their assigned migrations.

### Shared Readers and Secretary Self-Check

Status: candidate implementation. Targeted local checks passed. Complete CI and real default-channel acceptance remain pending.
The existing memory-use wrapper now calls the gateway. It does not reserve, invoke, or account through a separate provider path.
Committed secretary admission or a committed deputy lease supplies each execution identity.
Selection and page reads use separate stage names. Initial reading and later requested reading also use separate stages.
The original selection usage identity and validated group plan remain available in `model_usage`.

Secretary self-check uses its registered instructions and schema, with the original draft and deadline.
If the check fails, the secretary keeps its original draft and returns an existing skipped receipt.
The receipt identifies unsupported output modes when applicable. The workflow also records the separate application outcome.
If group selection fails or returns no groups, existing group fallback remains available and has a visible notice.
These notices do not change model instructions or grant extra action authority.

The dependency verifier now reads claim versions, grants, and source visibility in batches.
It preserves nature, inference, retirement, project, exclusion, and current-version checks.
Returned-result retention locks input identities in one ordered batch before checking access.
Missing identities remain conflicts, including references with a zero version.

Business acceptance fixtures use an explicit synthetic model through the gateway.
Their ordinary HTTP decision oracle does not establish production provider capability.
Actual capability rejection and Codex transport checks retain the production registry.
Changed historical fixture files now have behavior names. Their existing semantic assertions remain in place.

Initial checks found an incorrectly encoded stored group plan. The candidate keeps the validated plan as a JSON object.
The parallel-reader case then passed, with its original call and usage assertions.
Some other local checks failed when the verification host exhausted its disk space.
Those findings remain recorded. The affected checks passed after the owned test copy received bounded WAL retention.
Two new dependency fixtures initially omitted their model principal. The corrected fixtures passed without changing their assertions.

Affected gateway, reader, self-check, recovery, access, trust, order, and undo checks passed with synthetic models.
Query-count contracts verify that database round trips do not grow with the number of dependency memories.
Formatting, vet, build, resolved architecture checks, and non-PostgreSQL package race checks passed.
Complete serial local checks remain omitted for the resource limits described above. Complete exact-revision CI is still required.

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
