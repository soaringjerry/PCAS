# Phase 3.9 Handoff and Review Record

Sol handed off the Phase 3.9 work on 2026-10-09 at 19:40 UTC.
The coordinator reviewed that work on 2026-10-10 and continued Phase 3.9 through PR #404 (`4473ba7`) and the Phase 3.9 wrap-up batch.
The [delivery milestones](../status.md#foundation-delivery-milestones) and [architecture acceptance](../architecture.md#10-foundation-acceptance) separate completed foundation items from items deferred to Phase 4 or follow-up work.

## 1 Review Findings

| Finding | Evidence | Result |
|---|---|---|
| The storage package grew during a phase that must reduce it. | Non-test code in `internal/postgres` went from 26,455 to 28,355 lines between `4af34cd` and `25eef6f`. | Resolved. Business rules and orchestration moved into domain packages outside storage, and `internal/postgres` split into 25 focused packages over `kernel` (about 2,000 non-test lines remain in the root `internal/postgres` package). See [the pilot record](foundation-domain-pilot.md). |
| The released interactive gateway failed on every provider without output schema or web search. | The browser regression suite fails at `25eef6f`. The secretary returned an unsupported-capability receipt for an OpenAI-compatible provider. | Corrected. See the [correction](foundation-interactive-gateway.md#correction-2026-10-10). |
| Storage test fixtures replaced the capability check with one that accepted every mode. | `syntheticGatewayModel` in the storage, Telegram, and evaluation fixtures. | Removed. Tests use the actual check. |
| The released batches had no pull request and no acceptance by a different reviewer. | No merge commit on main after `4af34cd`. The browser regression workflow runs only for pull requests. | Corrected. Changes now merge to `main` via pull requests with CI runs. |
| The query embedding candidate saved query text and vectors for recovery. | Candidate `9f80205`: 454 production lines and a read-session lock. | Replaced. See the [query embedding page](foundation-query-embedding.md). |
| The background embedding scope planned a recovery mechanism for an unmeasured cost. | Its reproduction called the handler twice with one lease. The queue never does that. | Replaced. See the [background embedding page](foundation-background-embedding.md). |
| Budget deferrals appeared in the call journal as failed model calls. | Live journal: 523 topic-project rows with `topic_project_hourly_limit`, against 58 calls that reached a provider. | Corrected. A deferral leaves no journal row. |
| The legacy routing and answer interfaces had no client. | The web client and Telegram do not call them. | Removed. |
| The recorded-replay tools compare every field of every row. | About 2,700 lines in Go and Python. Each record states that complete state equivalence is not established. | Updated. `scripts/foundation_replay_identity.py` now pairs `recorded_events`, maps nested `model_calls` JSON references, and classifies field differences into runtime metadata versus business state while keeping every raw difference visible (`excluded_fields: []`). Full live production-copy replay remains an operational check when changing behavior. |
| The gateway request carried an untyped policy value, and the storage dispatcher repeated one decision in ten methods. | `Policy any` in `internal/modelcall/gateway.go`; `modelCallStorage` in `internal/postgres`. | Resolved. The policy is a declared type (`modelcall.Policy`), `ModelCallStorage` is removed, and `callstore.Journal` implements one unified call-journal lifecycle for both background jobs and interactive executions. |
| The phase 3 acceptance tests were skipped in a default run after phase 3 was released. | `phase3Finding` skipped each test that named a finding of the baseline before the implementation. The switches of phases 2.6 and 3.5 were already removed. | Corrected. With the switch off, 64 tests passed and one failed because its budget table predated phases 3.5 and 3.6. The switch is removed and that table is current. |
| Disposable test databases stayed on the host after an interrupted run. | 22 containers named `pcas-phase2_6-t-*` used about 3 GB of memory on 2026-10-11. | Corrected. Each container stops itself after two hours and is then removed. The owner approved removal of the leftover containers, volumes, images, and old backups on 2026-10-11. |
| The complete local check exceeds its thirty-minute package limit. | `make check` and `make test-integration` on the development host. | Mitigated. CI runs 12 parallel slices in `.github/workflows/memory.yml`, and local runs use `scripts/test-slices-local.sh` across parallel slices on one database. |

## 2 Delivery State

| Work | State |
|---|---|
| Shared model gateway and prompt registry | Complete. All production generation, embedding, vision, and transcription calls enter `internal/modelcall`; all production prompts and output schemas live in `internal/prompts` with `go:embed` and content hashes; temporary `prompts.FromText` is removed. |
| Unified call journal and storage boundaries | Complete. `callstore.Journal` unifies background and interactive call lifecycles; `deputystore` accesses call records through `callstore` and daily spend through `budgetstore.SpentSinceTx`; `itemstore` owns `work_items` writes. |
| Recorded replay diagnostics | Complete as a diagnostic layer. Exact multiset comparison, declared identity correspondence (including `recorded_events` and nested `model_calls` refs), and runtime-versus-business field classification are tested in CI. Running fresh production-copy replays remains an operational check for behavior-changing scopes. |
| Unused generation and temporary adapter cleanup | Complete. Unused card generation and legacy routing/answer interfaces are removed; all temporary migration parts listed in `foundation-domain-pilot.md` are removed. |
| Data-owner boundaries and domain extraction | Complete for Phase 3.9. All production business workflows, schedulers, commands, undo, erasure, access, secretary turns, and deputy runs decide in domain packages outside storage, and `internal/postgres` is split into 25 focused packages over `kernel` (which imports only `memory` and `worker`). |
| Activity queries | Complete for Phase 3.9. The [day activity query](../memory-service.md#day-activity) answers one local day's work, cost, and reasons with explicit link gaps. Visual observation UI and management controls move to Phase 4. |
| Declared event responses and causal limits | Delivered for queue events. One [registered table](../architecture.md#registered-responses) drives worker handlers, `recorded_events` records `work.queued` with root/cause/depth, chain depth is bounded at 200 with 90-day retention (`063_recorded_events.sql`, `064_event_limits.sql`). Committed-change events beyond `work.queued` and per-event response limits remain follow-up work. |
| Delivery path | Complete. Changes merge to `main` via pull requests with 12-slice PostgreSQL CI and browser regression workflows. |
| Five test layers and nightly evaluation | Delivered for Phase 3.9. Tightened architecture boundary tests, owner contract tests, replay diagnostics, concurrency/undo tests, and scheduled nightly real-model evaluation on Codex (`scripts/nightly-eval.sh`) are in place. Multi-night score trend analysis and live attachment-job observation remain ongoing operational checks. |

Do not derive a completion percentage from these unequal work items.

The provider boundary test declares no direct-call exception. Each production provider call is inside `internal/modelcall`.

## 3 Sol's Worktree

Sol's worktree is `/root/PCAS-worktrees/foundation-deputy-main` on branch `foundation/deputy-main`.
It contains an uncommitted partial background embedding implementation that does not compile.
The review branch does not use that code. Do not merge it.
Sol archived those files under the private evidence root before the handoff.

## 4 Private Evidence

These paths are local private evidence. Do not copy source bodies, credentials, or model outputs into Git.

| Location | Purpose |
|---|---|
| `/root/PCAS-private/foundation/query-embedding` | Contracts, replay, real Codex results, and CI records for the replaced query candidate. |
| `/root/PCAS-private/foundation/background-embedding` | Source record and replay baseline for the replaced background scope. |
| `background-embedding/handoff-latest.json` | Archive path for Sol's unfinished code and its manifest. |

Disposable database metadata is private. Do not print those files or their database URLs.
One fixture credential was exposed earlier and was rotated.
Verify container identities and owner labels before a lifecycle operation. Do not stop or remove an unverified container.

## 5 Release Rule

After accepted integration and passing CI, release needs no further routine permission.
First complete `pg_dump` and the necessary file and private-configuration backups.
Then deploy the pinned revision and examine readiness, logs, and the affected functions.
Test the real default Codex secretary and show failures.
Clean only the identities that the test created, then examine claim counts.
