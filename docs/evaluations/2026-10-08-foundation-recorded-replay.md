# Foundation Recorded Replay: Initial Evidence

Initial date: 2026-10-08 UTC. Initial application behavior: released revision `935ad5b`.
Business-clock verification: 2026-10-09 UTC, based on `7c7b612` with the uncommitted approved clock extension.
The diagnostic implementation remains under [its assigned scope](../tasks/foundation-recorded-replay.md).
The [command reference](../../cmd/pcas-eval/production-replay/README.md) gives invocation and interpretation rules.
This record does not close Phase 3.9 or its recorded-replay layer.

## Isolation

The cases used the owned production-data copy prepared from the verified shared-skeleton release backup.
Each side started from a clone of the same unchanged template.
The runner verified the container identity, ownership labels, database name, and loopback endpoint before connecting.
No product server, unrestricted worker, polling, or notification delivery ran on the copy.
Inputs, credentials, replies, and raw snapshots remain outside Git in private storage.

The runner retains every owner-data field in compressed snapshots.
Snapshot hashes cover uncompressed bytes. The comparator uses row multisets, not physical SQL row order.
An early uncompressed snapshot was compressed and checked against its original hash before removing the duplicate file.
Identical starting snapshots share storage only after their complete compressed fingerprints match.

A redundant database-archive procedure exhausted available host storage and failed during cleanup.
The archive itself completed and passed its restore-list check before that failure.
The completed case snapshots and model transcripts remained intact.
The temporary archive was removed after the procedure failed.
Only the two verified owned clock-case databases were removed after checking complete evidence and no active connections.
The original backup, copied files, and unchanged replay template remain available.
Production database and API health checks remained healthy after space recovery; the worker remained running on the prior release.

## Observed Cases

| Case | Observed result | Limit |
|---|---|---|
| Historical source ingestion, chunking, and extraction | Recording and offline replay completed. Each side changed the claim count from 5253 to 5255. | Starting data matched exactly. Exact changes differ; identity correspondence and runtime rules remain incomplete. |
| Real secretary question | The default Codex recording completed. Strict replay rejected the changed model input. | Only the `deskNow` line changed. No recorded reply was consumed and no live provider started during replay. |
| Initial extraction fixture | Capture failed before any model submission because the diagnostic list omitted chunking. | Corrected the operation list. Retained the failed evidence; did not change product code. |
| Secretary question with approved business clock | New default Codex recording and strict offline replay completed. | The complete model input matched. One Codex reply and one HTTP embedding reply were consumed, with no live provider starts or live HTTP calls during replay. Complete state equivalence remains incomplete. |

The successful extraction replay used no Codex login file and no live API credentials.
It consumed one recorded Codex reply, with zero live provider starts and zero live HTTP calls.
All three selected operations completed without a recorded operation error.
The recording used the real default Codex configuration, not a free-model substitute.

The secretary's starting snapshots were equal under exact state comparison.
Its changes differed after the rejected reply, as expected for a failed replay.
The complete model input, reply, unmatched input, and field difference remain in private evidence.
The only input difference was the changing current minute.
No instruction, schema, model, source text, or other supplied context changed in that case.

## Current Gaps

The coordinator approved business-time injection after this initial recording.
The implementation now supplies an injectable business clock.
The new secretary recording and strict offline replay passed with the same fixed business time.
Time must remain part of model-input matching.
Lease and timeout clocks must remain real execution clocks.

The comparator excludes no fields and preserves random identities as differences.
Both cases started from equal data under exact row comparison.
Both have explicit state differences. No field or generated identity was removed to make comparison pass.
Generated-identity correspondence, runtime-field rules, receipts, and dependency comparisons need further work before owner migration.
The extraction case now has a bijective map for 20 generated identities, with no unresolved matching keys.
Its 51 affected rows pair through database primary keys after declared reference mapping.
Remaining field differences are execution timestamps, capture source-display timestamps, and measured model duration.
These fields remain in the raw data and comparison. No full state-equivalence claim follows from this partial result.
The clock-controlled secretary case also starts from exactly equal data.
Its strict transport replay completed without notices or operation errors.
The legacy secretary wrapper does not put its reservation ID into the usage record.
One reservation per side therefore lacks a declared correspondence.
Keep that identity difference visible; do not match reservations by equal cost.
Complete accounting equivalence remains incomplete until the corresponding gateway path records its links.
A successful transport replay is not full before-and-after refactor acceptance.
The fixture queue does not validate ordinary queue priority or candidate exclusion.
Those rules remain separate owner and concurrency requirements.

## Checks

The synthetic Codex and HTTP checks passed with the real application adapters and fictional provider responses.
They cover changed inputs, schema order, provider metadata, missing replies, repeated consumption, corrupted evidence, and capture failure.
Private-path, copy-identity, exact-data, decimal-precision, and evidence-preservation checks passed.
Resolved model architecture checks passed.
Formatting, diagnostic vet, and documentation link checks passed.

The full local `make check` reached the 30-minute PostgreSQL-package timeout. It did not pass.
The active test was `TestPhase26G4T4OneMemoryEditRunsOnlyIts40AffectedComparisonCalls`.
No assertion failure was reported before the package timeout.
The same local timeout limitation was recorded during the earlier shared-skeleton work.
One older check was stopped by verified owned process IDs after the approved clock change superseded it.
That check remains incomplete.
The current `make check` attempt also reached the 30-minute PostgreSQL-package timeout in the same comparison test.
It reported no assertion failure before the timeout.
Current diagnostic checks, architecture checks, vet, and build passed separately.
The initial diagnostic capture did not change production files.
The approved business-clock extension changes `internal/postgres` and requires storage checks before delivery.
The focused storage contract passed: relative dates use the fixed business time in the owner's zone, and usage timestamps use actual time.
The full `make test-integration` attempt did not pass.
Four existing Phase 3 tests reported PostgreSQL storage-exhaustion errors during the host-space incident.
The package then reached its 30-minute timeout while loading the Phase 2.6 priority fixture.
After space recovery, the four affected tests and both clock checks passed together with the race detector.
The exact-revision CI slices subsequently passed, as recorded below.
No frontend file changed.
No production migration was added for this diagnostic work.

## Release Verification

Released revision: `b39a9eab41a1b8015c4f9fdd016582fce322b09f`, on 2026-10-09 UTC.
This release includes the diagnostics and approved business-clock extension.
It does not certify complete state equivalence or close Phase 3.9.

| Check | Observed result |
|---|---|
| [Phase CI](https://github.com/soaringjerry/PCAS/actions/runs/37868807375) | All thirteen jobs passed for the released revision. |
| [Main CI](https://github.com/soaringjerry/PCAS/actions/runs/37870089363) | All thirteen jobs passed for the same revision after direct integration. |
| Other main evaluation workflows | The doing and independent evaluation workflows passed for the same revision. |
| Backup | Custom-format `pg_dump` and complete file-volume archive completed. Restore-list checks passed. Private configuration was backed up. Both archives have recorded fingerprints. |
| Existing data | Owner memory, claim, source, and work-item counts were unchanged during deployment migration verification. No new migration was necessary. |
| Runtime | API and worker use the pinned release image. Source contents and image revision were verified. Readiness passed. |
| Real default channel | Codex used `gpt-6.1-sol`. The reply was the requested deployment-check answer, with measured input and output tokens. No notices or unexpected actions were returned. |
| Own-request cleanup | Cleanup succeeded. No source for the test request remained. Total and active claims each remained at 5253. |

Actual requests, replies, accounting details, backup locations, and release logs remain in private evidence outside Git.
The local timeout and storage-exhaustion findings above remain recorded failures; CI success does not change those historical results.

The follow-up documentation check attempted `env -u PCAS_TEST_DATABASE_URL make check`.
The older scale fixtures start their own databases even without that variable.
The executor stopped the verified check process tree and removed its database after checking the test process's connections.
This attempt remains incomplete. It is not a passing full local check.
The documentation commit changes no runtime source. Storage coverage comes from the exact-revision CI above.
Formatting, vet, build, and document index, link, anchor, and writing checks passed separately.

## Signed Difference Verification

The diagnostic extension uses the existing recordings and snapshots. It makes no additional model calls or database writes.
Signed export keeps removed rows, added rows, and duplicate occurrences.
It binds the raw delta to the exact report and rechecks every snapshot's bytes and row count.
Field analysis pairs rows by unique declared primary keys after reference mapping.
It preserves all fields, array positions, scalar types, decimal precision, and missing-versus-null distinctions.
Historical pinning includes declared JSON references whose original object no longer exists.

The column inventory came from the verified owned copy in a read-only transaction.
All 55 applied migration checksums matched the repository.
It contains 665 columns in 94 owner-scoped tables. Both cases have complete historical column coverage.
Private verification records the comparison source fingerprints.

| Existing case | Observed field report | Remaining finding |
|---|---|---|
| Extraction | 20 identities correspond. All 51 changed rows pair. Field differences remain in 28 rows. | Dates, capture source-display timestamps, and measured duration remain visible. Complete equivalence is not certified. |
| Secretary with fixed business clock | Six identities correspond. Fifteen rows pair; thirteen contain field differences. | Two unmatched reservation groups remain, one per side. The legacy secretary usage link is still absent. Complete equivalence is not certified. |

The synthetic state suite passed all 25 checks. The transport suite passed all eight checks.
Checks cover signed deletion, changed versions and dependencies, duplicate rows, stale snapshots, exact values, incomplete metadata, and preserved earlier results.
An additional whole-owner comparison checks references in otherwise identical rows.
Changed-row inspection alone does not establish complete mapping coverage.
Both complete snapshot sets passed source binding and historical coverage checks.
After identity mapping, extraction retains 56 differing row fingerprints. The secretary case retains 28.
These are comparison findings, not certified state equivalence or newly observed business regressions.
Formatting, vet, build, diagnostic race checks, resolved architecture checks, and document checks passed.
No Go runtime source or frontend source changed.
The full local `make check` was not repeated for this Python and documentation change.
Its unchanged Go source has the complete exact-revision CI results above; the previous local scale-fixture limitations remain recorded.
CI verification for this extension is separate from those earlier runtime results.
The [diagnostic CI](https://github.com/soaringjerry/PCAS/actions/runs/37875830826) passed all 13 jobs at `1165496` on main.
This extension changes offline diagnostics. The deployed runtime remains `b39a9ea`.
The coordinator approved the separate interactive gateway scope. This diagnostic change does not implement that migration.
