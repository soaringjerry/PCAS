# Phase 3.9 Handoff and Review Record

Sol handed off the Phase 3.9 work on 2026-10-09 at 19:40 UTC.
The coordinator reviewed that work on 2026-10-10 and continued it on branch `foundation/claude-corrections`.
Phase 3.9 is incomplete. The [delivery milestones](../status.md#foundation-delivery-milestones) give the full phase scope.

## 1 Review Findings

| Finding | Evidence | Result |
|---|---|---|
| The storage package grew during a phase that must reduce it. | Non-test code in `internal/postgres` went from 26,455 to 28,355 lines between `4af34cd` and `25eef6f`. | Open. The [pilot](foundation-domain-pilot.md) gives the extraction pattern. |
| The released interactive gateway failed on every provider without output schema or web search. | The browser regression suite fails at `25eef6f`. The secretary returned an unsupported-capability receipt for an OpenAI-compatible provider. | Corrected. See the [correction](foundation-interactive-gateway.md#correction-2026-10-10). |
| Storage test fixtures replaced the capability check with one that accepted every mode. | `syntheticGatewayModel` in the storage, Telegram, and evaluation fixtures. | Removed. Tests use the actual check. |
| The released batches had no pull request and no acceptance by a different reviewer. | No merge commit on main after `4af34cd`. The browser regression workflow runs only for pull requests. | The coordinator's review is the first independent check. Later work uses pull requests. |
| The query embedding candidate saved query text and vectors for recovery. | Candidate `9f80205`: 454 production lines and a read-session lock. | Replaced. See the [query embedding page](foundation-query-embedding.md). |
| The background embedding scope planned a recovery mechanism for an unmeasured cost. | Its reproduction called the handler twice with one lease. The queue never does that. | Replaced. See the [background embedding page](foundation-background-embedding.md). |
| Budget deferrals appeared in the call journal as failed model calls. | Live journal: 523 topic-project rows with `topic_project_hourly_limit`, against 58 calls that reached a provider. | Corrected. A deferral leaves no journal row. |
| The legacy routing and answer interfaces had no client. | The web client and Telegram do not call them. | Removed. |
| The recorded-replay tools compare every field of every row. | About 2,700 lines in Go and Python. Each record states that complete state equivalence is not established. | Open. The architecture requires comparison of state, evidence, actions, and quality. |
| The gateway request carried an untyped policy value, and the storage dispatcher repeated one decision in ten methods. | `Policy any` in `internal/modelcall/gateway.go`; `modelCallStorage` in `internal/postgres`. | Corrected in part. The policy is a declared type, and the dispatcher selects the journal in one place. The background journal and the interactive journal are still two implementations of one lifecycle. |
| The phase 3 acceptance tests were skipped in a default run after phase 3 was released. | `phase3Finding` skipped each test that named a finding of the baseline before the implementation. The switches of phases 2.6 and 3.5 were already removed. | Corrected. With the switch off, 64 tests passed and one failed because its budget table predated phases 3.5 and 3.6. The switch is removed and that table is current. |
| Disposable test databases stayed on the host after an interrupted run. | 22 containers named `pcas-phase2_6-t-*` used about 3 GB of memory on 2026-10-11. | Corrected. Each container stops itself after two hours and is then removed. The owner approved removal of the leftover containers, volumes, images, and old backups on 2026-10-11. |
| The complete local check exceeds its thirty-minute package limit. | `make check` and `make test-integration` on the development host. | Open. Four parallel slices on one database complete the package. |

## 2 Delivery State

| Work | State |
|---|---|
| Shared model gateway and prompt registry | Released for background generation, the secretary, the readers, the self-checks, and the deputy. |
| Recorded replay diagnostics | Released. Complete state equivalence is not established. |
| Unused card generation cleanup | Released at `6411b4e`. |
| Query and background embedding through the gateway | Released at `a5c1483` on 2026-10-11. |
| Mode fallback correction for providers without schema or search | Released at `a5c1483` on 2026-10-11. |
| Legacy routing and answer removal | Released at `a5c1483` on 2026-10-11. |
| Transcription and image interpretation through the gateway | Released at `6dc2363` on 2026-10-11. |
| Data-owner boundaries and domain extraction | Six background paths have their rules in domain packages: effort estimation, topic projects, date review, project handover, global handover, and the alias scan. The [pilot page](foundation-domain-pilot.md) lists them and what each leaves in storage. The secretary, the deputy, workspace commands, memory interpretation, and input remain in the storage package. |
| Activity queries | The [day activity query](../memory-service.md#day-activity) answers one local day's work, cost, and reasons. Its response lists the links that the records do not contain. |
| Declared event responses | One [registered table](../architecture.md#registered-responses) names each stage's trigger, producer, specialist, hourly limit, and duplicate key. The worker registers its handlers from it. Triggers are not yet recorded events with causal identities. |
| Causal limits | Not started. The [first live readings](../evaluations/2026-10-11-activity-baseline.md) measure the repeated deferrals that these limits must address. |
| Five test layers and nightly evaluation trends | Not started as a complete set. |

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
