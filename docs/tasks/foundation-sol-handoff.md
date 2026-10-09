# Sol Foundation Handoff

Handoff requested by the coordinator on 2026-10-09 at 19:40 UTC.
Phase 3.9 is incomplete. This page does not authorize release of unfinished code.
Use the [architecture](../architecture.md), [workflow](process.md), and [executor rules](../../AGENTS.md).
The [delivery milestones](../status.md#foundation-delivery-milestones) retain the full phase scope.

## 1 Workspace and Current Revision

| Item | Verified state at handoff |
|---|---|
| Sol worktree | `/root/PCAS-worktrees/foundation-deputy-main`. |
| Sol branch | `foundation/deputy-main`. |
| Last committed revision before this document | `d0b0345f37699f70960046725652c333a6941481`. |
| Remote integration branch before this document | `phase3_9/main`, at the same revision. |
| Fetched `origin/main` | `cc637902467ecb6cbb7480e65d5a401d9a570a9e`. |
| Query implementation revision | `9f80205073078e766152856de79854a73555c8b6`. |
| Current uncommitted code | Partial background embedding migration. It does not compile. |
| Last recorded production release | `6411b4e09a3180387718dbdecf72da292f66c940`. Recheck the live revision before release. |

Do not switch, stage, or restore `/root/PCAS`.
Another window owns changes there, on `fix/projects-from-what-the-user-calls-a-project`.
Use the isolated Sol worktree. Examine its current diff before changing it.
No provider or test process was started by this handoff operation.

## 2 Delivered Work and Remaining Scope

| Work | Delivery state |
|---|---|
| Architecture, domain responsibilities, and original-path inventory | Initial documents and inventory exist. Refresh each affected path before migration. |
| Shared model gateway and prompt registry | Released for the initial background generation paths. Complete caller coverage remains outstanding. |
| Recorded replay diagnostics | Released. Transport replay works for supported paths. Complete owner-state equivalence is not established. |
| Secretary and reader generation, with self-checks | Released at `022a912`. |
| Main deputy generation and revision | Released at `f0ee817`. |
| Unused card generation cleanup | Released at `6411b4e`. Surviving queue compatibility remains documented. |
| Query embedding | Implemented at `9f80205`. Branch CI, strict replay, and isolated real Codex checks pass. Independent acceptance is outstanding. |
| Background embedding | Baseline work is complete for the controlled source case. Current implementation is partial and uncommitted. |
| Complete data-owner boundaries and domain extraction | Not started. |
| Unified activity queries, declared event responses, and causal limits | Outstanding. |
| Complete five-layer acceptance and nightly score trends | Outstanding. |

The query correction is not merged into main or released.
The following commit adds background scope documentation. It does not deploy query code.
Do not derive a completion percentage from these unequal work items.

The provider boundary test still declares these direct-call exceptions:

- File transcription in `internal/postgres/attachments.go`.
- Image interpretation in `internal/postgres/vision.go`.
- Background embedding in `internal/postgres/processing.go`.
- Telegram transcription in `internal/telegram/poller.go`.
- Jev routing in `internal/httpapi/model_settings.go` and `internal/httpapi/workspace.go`.
- Legacy `/v1/desk/answer` generation in `internal/postgres/desk.go`.

Migrate the complete applicable paths or retire verified unused paths.
Removing an exception without removing its actual provider call is not acceptance.

## 3 Query Batch Acceptance

The corrected query code retains history-source references and removes private query inputs during source deletion.
It preserves original source versions for historical query provenance.
Current retrieval still checks access and applicability independently.

The complete branch CI result for `9f80205` is successful:
[Recorded CI run](https://github.com/soaringjerry/PCAS/actions/runs/37971483395).
Recheck its revision and conclusion before integration.
The later documentation-only revision has no new memory workflow run because the workflow excludes documentation paths.

Focused race contracts, strict offline replay, and isolated real default Codex acceptance pass.
Complete raw snapshots and examined differences remain private.
These results do not prove complete owner-state equality or independent reviewer acceptance.

The unfiltered local checks did not pass:

- `make check` reached its package timeout during an existing comparison-scale case.
- The later integration run reached its package timeout during an existing scale case.
- An upgrade fixture lacked initialization of the default schema.

The isolated upgrade check passed after the same initialization used by CI.
Its original failure remains recorded. No expected value was changed to hide the failure.

Workflow section 3 requires acceptance by a different reviewer for significant state changes.
The coordinator's approval to continue is not evidence that this acceptance occurred.
Do not reopen a PR or send another window a message automatically.
Use the existing [Claude review prompt](foundation-query-embedding.md#claude-review-window-prompt) when review is assigned.

## 4 Background Baseline

Read the complete [background embedding scope](foundation-background-embedding.md) before implementation.
The current handler loses returned batches if its final vector transaction fails.
The retained fake-provider contract observed two original calls, then four calls after storage retry.
No partial vectors survived the failed transaction.
This is a controlled reproduction, not a production spending measurement.

The production snapshot had no ready source embedding job.
The controlled case ingested an unchanged stored source body with a new copy-only external ID.
Normal chunk and embedding handlers produced 30 chunks and one real embedding call.
Ordered inputs matched the original source chunks exactly.
Strict offline replay consumed that reply with no live provider call.
Claims were unchanged in both controlled runs.

The complete raw snapshots remain reconstructable without field exclusions.
All compared row differences have classifications, including generated identities and execution clocks.
Complete state equality is not certified.
This baseline covers a controlled source path, not original wire capture, claim embedding, or all backfill paths.
Both temporary copy databases and their original runtime roots were retired.
Do not attempt to resume their old handles or reuse their IDs.

## 5 Uncommitted Implementation

The following code is unfinished. It is not included in the handoff documentation commit.

| File | Partial change |
|---|---|
| `internal/postgres/model_calls_embedding.go` | New batch policy, invocation-keyed result adapter, private-input deletion, and accounting recovery methods. |
| `internal/postgres/model_calls.go` | Routes embedding operations to those methods. Adds actual invocation-stage recovery and absent prompt fields. |
| `internal/postgres/budget.go` | Separates queue stage from invocation stage. Starts admission transfer using rounded cumulative estimates. |
| `internal/postgres/database.go` | Adds the pending embedding response map. |
| `internal/postgres/background_model.go` | Excludes an input-only embedding plan from legacy paid-result loading. |
| `internal/postgres/editing.go` | Starts source-owner cleanup for embedding plans and results. |
| `internal/postgres/runs.go` | Starts embedding accounting recovery in the existing worker pass. |

`ProcessEmbedding` still calls `s.models.EmbedProviderUsage` directly.
No complete plan producer exists. The new adapter references an undefined `embeddingPlan` type.
Therefore, the current worktree cannot build or serve as a release candidate.
Do not claim that these edits fix duplicate calls yet.

The following gaps require implementation or correction:

1. Persist and validate a complete immutable plan before the first submission.
2. Connect `ProcessEmbedding` to `Gateway.Call` with stable batch identities and exact original ordered texts.
3. Load pending in-process responses before authorizing a replacement.
4. Keep the final vector application and queue acknowledgement atomic under the actual lease and current versions.
5. Preserve exact numeric allocation in SQL. Examine the current helper's numeric-to-float-to-numeric intermediate.
6. Return never-submitted allocations to the admission hold, including start failures and unavailable adapters.
7. Distinguish known failures, unknown outcomes, paused recovery, and storage failures without resetting the recovery allowance.
8. Reject concurrent duplicate submissions after a successful batch becomes settled.
9. Verify legacy schemas before cleanup or worker recovery queries reference `model_calls`.
10. Verify private-result hashes, reservation links, source deletion, edits, and late responses together.
11. Bind recovered accounting to original batch usage references. Do not silently replace them with root-source references.
12. Complete interrupted-job recovery and unused admission release through the existing worker.
13. Update the resolved owner boundary test for the new journal storage file after its ownership is complete.
14. Install behavior contracts, including the retained storage-failure reproduction. Migrate affected phase-coded tests when changed.

This list is a handoff inspection, not a completed review.
The next executor must examine the actual diff and can replace unsuitable partial code.
Do not add another model wrapper, table, queue, package structure, or recovery policy to complete this batch.
A necessary schema change requires the decision specified in `AGENTS.md` section 9.

## 6 Private Evidence and Recovery

These paths are local private evidence. Do not copy source bodies, credentials, or model outputs into Git.

| Location | Purpose |
|---|---|
| `/root/PCAS-private/foundation/query-embedding` | Query contracts, replay, real Codex, CI, local failures, and review packet. |
| `/root/PCAS-private/foundation/background-embedding` | Source record/replay baseline, complete snapshots, classification, and preparation failures. |
| `background-embedding/continuation-ready.json` | Previous baseline status. It predates the uncommitted implementation. |
| `background-embedding/handoff-latest.json` | Exact archive path for the current unfinished code and manifest. |
| `query-embedding/background_embedding_recovery_test.go` | Retained behavior contract. It fails on the unchanged handler. |
| `query-embedding/claude-review-packet.txt` | Corrected query review packet. Recheck its revision before use. |

The handoff archive retains each modified file and the original tracked diff.
Its manifest records the committed base, file paths, byte sizes, and SHA-256 values.
The untracked adapter is retained separately in the archive's source tree.
The tracked patch alone does not contain that adapter.
Verify each archived file before applying or replacing a current file.

The existing source dump is stored as a lossless zstd patch against an immutable replay dump.
`query-embedding/owned-source-base.dump.zstd-patch.json` records the reconstruction requirements.
Keep the base dump, exact-reference snapshot base, and final accepted query binary archive.
Never infer that a missing raw path permits deletion of its recovery dependencies.
Archive symlinks as symlinks. Do not follow them into shared installed binaries.

Disposable database metadata is private in `query-embedding/history-contract-db.json` and `history-final-integration-db.json`.
Never print these files or their database URLs.
One fixture credential was accidentally exposed earlier and was rotated. The proof remains private.
Verify full container IDs, owner labels, fixture labels, state, and capacity before reuse.
Do not stop or remove an unverified container.

At handoff, the root filesystem had about 1.1 GB available. `/dev/shm` had about 3.5 GB available.
These observations are not a new resource allowance. Recheck capacity before restoring data or building.
Use owned scratch directories. Preserve the release recovery margin and shared caches.
Do not start another production-copy replay until the new implementation compiles and its owner contracts pass.

## 7 Handoff Checks and Delivery Rules

`make fmt` and `git diff --check` ran for the partial code before archival.
`make check` failed during `go vet` because `embeddingPlan` is undefined.
The exact compiler errors and exit code are recorded beside the handoff archive.
Integration and real-model checks were not run for this handoff because the unfinished code cannot compile.
No background implementation acceptance, merge, deployment, or live secretary check occurred in this handoff.

Commit and push only the handoff document and its documentation-index entry.
Leave unfinished code available for the next executor. Do not stage the whole worktree.
The existing query review gate remains outstanding.

After accepted integration and passing CI, release remains authorized without another routine permission request.
First complete `pg_dump` and necessary file and private-configuration backups.
Then deploy the pinned revision and inspect readiness, logs, and affected functions.
Test the real default Codex secretary. Show failures.
Clean only the test's request or source IDs, then inspect claim counts.

## 8 Sol Window Prompt

```text
You are Sol, the executor continuing the complete PCAS Phase 3.9 foundation.
Reply in Chinese. Write code, commits, and repository documents in English under the writing guide.
Read AGENTS.md, docs/README.md, docs/architecture.md, docs/status.md, docs/tasks/process.md, and this complete handoff.
Read the whitepaper, service reference, and complete background embedding scope before implementation.
Use /root/PCAS-worktrees/foundation-deputy-main on foundation/deputy-main.
Do not switch, stage, or restore /root/PCAS. Another window owns changes there.
Recheck HEAD, remote branches, the uncommitted diff, check results, and archive manifest before changing files.
The last code candidate is query revision 9f80205073078e766152856de79854a73555c8b6.
It passed branch CI and isolated query checks. Independent acceptance, main integration, and release remain outstanding.
Current uncommitted background embedding code does not compile and is not a release candidate.
Examine and repair or replace it. Do not repeat completed baseline recording merely because retired handles are missing.
First finish the complete plan, numeric admission transfers, pending-result reuse, and Gateway.Call integration.
Preserve original texts, input order, provider/model selection, full-job admission, and final atomic vector application.
Use stable actual-job batch stages and unique invocation/reservation identities.
Save each paid batch before another submission. Storage or accounting recovery must not repeat a known paid call.
Retain unknown holds and use the existing bounded queue recovery. Do not silently change retry policy.
Check deletion, source edits, lease reclaim, duplicate jobs, late billing, and missing credentials together.
Use fake-provider owner contracts before new production-copy replay and real-model acceptance.
Keep complete raw comparison evidence and all failures visible.
Finish all remaining model paths or verified retirement, owner boundaries, domain migration, activity/event work, and five-layer acceptance.
No milestone count establishes a completion percentage. Do not close Phase 3.9 around already delivered gateway work.
Do not create PRs. Stage only assigned files and record required checks, failures, and omissions honestly.
State-changing delivery needs a different reviewer's actual acceptance. Approval to continue does not fabricate that evidence.
After accepted integration and passing CI, back up, release, and inspect the actual default Codex secretary without asking again.
Clean only own request/source identities. Check claim counts after cleanup.
Read the managed runtime skill before Docker or network work. Preserve proxy, CA, credential, and shared-cache settings.
Pin Docker to the local socket and verify full container IDs and owner labels before lifecycle operations.
Never print database URLs, credentials, raw private prompts, or source bodies.
Stop test services only by owned PID. Run browser checks with env -u DISPLAY.
Keep the full goal active until requirement-by-requirement evidence proves Phase 3.9 complete.
```
