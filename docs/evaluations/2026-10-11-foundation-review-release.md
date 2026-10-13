# Phase 3.9 Review Corrections: Release Record (2026-10-11)

Released revision: `a5c14831ab27311aef8d5b3d884ef6959b086e7e`. Earlier revision: `6411b4e`.
Pull requests #296 and #297 give the changes and their checks.

## Release

| Step | Result |
|---|---|
| CI before merge | #296: 21 checks pass. #297: 34 checks pass. Both include the browser suite. |
| Database migration | None in this release. |
| Stop | API and worker stopped with a 45-second limit. |
| Backup | Database dump, restore-list validation, environment file, private settings, and file volume. Database 374.7 MB, files 166.6 MB. The backup is under `/root/pcas-backups` with the prefix `foundation-review-a5c1483`. |
| Record counts before and after start | Equal: 101,944 memory records, 5,253 claims, 27,478 sources, 25 work items, 771 call records, 9,458 usage records. |
| Start and readiness | API and worker run the released image. `/readyz` returns 200. No container restarted. |

## Live Check

Three check-only turns in one smoke group used the real default Codex channel.

| Turn | Result | Duration |
|---|---|---|
| Answer | The requested fixed reply. | See the journal rows of this group. |
| Arrangement | Receipt `create_task`, done: a task for the next day at 15:00 with a reminder at 14:30. | 16.5 s |
| Change | Receipt `update`, done: the task moved to 13 October at 15:00. | 15.1 s |

Each turn has one `secretary` answer call on the Codex provider: returned, settled, with the `secretary-output` schema and no mode fallback.
The arrangement and the change each have one `query_embedding` call through the gateway: returned and settled, with the secretary's request as its cause.
The group was cleaned by its smoke identity. Claims, sources, and work items have the same counts as before the check.
The API log has no error after the release.

## Not Checked on the Live Instance

- A provider without output schema or web search. The live default is Codex. The browser suite and the storage tests cover the corrected fallback.
- Background embedding of a new source. No source was added by the check.

## Second Release on the Same Day

Released revision: `6dc23638196ec697b13aaabeb038e9ce140430c3`. Earlier revision: `a5c1483`.
Pull requests #298, #299, #300, #301, and #302 give the changes and their checks.

| Step | Result |
|---|---|
| CI before merge | #302: 33 checks pass, including the browser suite and the phase 3 acceptance tests that were skipped before. |
| Database migration | None. |
| Backup | Database 374.8 MB, files 166.4 MB, under `/root/pcas-backups` with the prefix `foundation-review-6dc2363`. |
| Record counts before and after start | Equal. |
| Start and readiness | API and worker run the released image. No container restarted. The API log has no error. |
| Release tool | `scripts/release.py` and `scripts/live_check.py` from the repository. |

The live check used the real default Codex channel: an answer, an arrangement (`create_task`, done), and a change (`update`, done), each between 16 and 17 seconds.
Each turn has one settled `secretary` call and one settled `query_embedding` call. Cleanup succeeded, and the record counts are equal to the counts before the check.

`GET /v1/workspace/activity/day` answered in 0.05 seconds for the current local day: 423 events, 158 invocations, and a settled cost of 0.00721.

Not checked on the live instance: a voice message, an image, and an audio file through the new recorded entries. No such input arrived during the check.
The stage deferral counts need a later reading, after the queue has passed several hourly windows on this revision.

## Third Release on the Same Day

Released revision: `a893628069f2e7b0f643f8c270be70f795cfc894`. Earlier revision: `6dc2363`.
Pull requests #303, #304, and #305 give the changes: the date review rules and the project handover rules left the storage package.

| Step | Result |
|---|---|
| CI before merge | #305: all checks pass, including the browser suite. |
| Database migration | None. |
| Backup | Validated, under `/root/pcas-backups` with the prefix `foundation-review-a893628`. |
| Record counts before and after start | Equal: 5,253 claims and 25 work items. |
| Start and readiness | API and worker run the released image. No container restarted. Neither log has an error. |

The live check used the real default Codex channel: an answer, an arrangement (`create_task`, done), and a change (`update`, done), between 15 and 19 seconds each.
Each turn has one settled `secretary` call and one settled `query_embedding` call. Cleanup succeeded and the record counts are equal.

## Reading After the Second Release

In the first 68 minutes on `6dc2363`, the topic project stage made two calls that reached the provider, and the call journal holds those two rows only.
The stage recorded one hourly-limit deferral. Five topic project jobs were queued in that time.
The queue was much shorter than on 2026-10-09, so this reading does not measure the reduction at that scale.

## Fourth Release on the Same Day

Released revision: `218ea5b0a0874704b1b796f6c2c7efdd13f5b96a`. Earlier revision: `a893628`.
Pull requests #307, #308, #309, and #310 give the changes: the global handover rules left the storage package, one response table drives worker registration, and the gateway request has a declared policy type.

The release had no database migration. The backup was validated, and the claim count is equal before and after the start.
The live check on the real default Codex channel passed: an answer, an arrangement (`create_task`, done), and a change (`update`, done), each about 17 seconds, with settled calls and a clean group cleanup.
Neither log has an error, and no container restarted.

## Fifth Release on the Same Day

Released revision: `3695c595eea3fa33f3ac4096a31de0919903d7c2`. Earlier revision: `218ea5b`.
Pull requests #312, #313, and #314 give the changes: the alias scan rules, the statement comparison rules, and the entity keeper rule left the storage package.

The release had no database migration. The backup was validated, and the claim count is equal before and after the start.
The live check on the real default Codex channel passed: an answer, an arrangement (`create_task`, done), and a change (`update`, done), each about 16 seconds, with settled calls and a clean group cleanup.
Neither log has an error, and no container restarted.

## Sixth Release on the Same Day

Released revision: `2036c14a2d7a5ae40923242e1f82b751b8a4c814`. Earlier revision: `3695c59`.
Pull requests #316, #317, and #318 give the changes: the job flows of statement comparison and entity comparison left the storage package.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 25 work items.
The live check on the real default Codex channel passed: an answer, an arrangement (`create_task`, done), and a change (`update`, done), each about 17 seconds, with settled calls and a clean group cleanup.
Neither log has an error, and no container restarted.
No comparison job ran in the first eight minutes after the start, so this record does not show a comparison call on the live instance.

## Seventh Release on the Same Day

Released revision: `43f3df86610dce75d36e698a786cfd66b047d567`. Earlier revision: `2036c14`.
Pull requests #319, #321, #322, #323, #324, and #326 give the changes: organize, attachment parsing, extraction, and the rules of conversation extraction left the storage package.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 25 work items.
The live check on the real default Codex channel passed: an answer, an arrangement (`create_task`, done), and a change (`update`, done), between 16 and 21 seconds each, with settled calls and a clean group cleanup.
Neither log has an error, and no container restarted.

The check-only turns do not save a source, so they start no extraction job. Pull request #326 said that they do. That statement was wrong.
In the first twelve minutes after the start, one global handover job ran and its call was settled. No organize, extraction, or attachment job ran.
The moved paths therefore have their CI results and no live result in this record. The next real input of the owner is their first live use.

## Eighth Release on the Same Day

Released revision: `a729396989512d6bd6871c6d3f9ca402192ed3e4`. Earlier revision: `43f3df8`.
Pull requests #325, #327, #329, #330, #331, and #332 give the changes: the rules of the secretary and of the deputy that need no storage left the storage package, with the rules of the secretary's actions and the workspace part of its model input.

One check of #332 failed once: `TestDoingTiersPrivateProductPipeline`, with the class `model_error`, which hides the cause. The second run passed, and eight local runs passed. The cause is not known. Pull request #333 makes the next failure readable.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 25 work items.
The live check on the real default Codex channel passed: an answer, an arrangement (`create_task`, done), and a change (`update`, done), between 15 and 17 seconds each, with settled calls and a clean group cleanup.
The two receipts came from the moved action rules: the first names the due time and the reminder 30 minutes before, and the second names the new date.
Neither log has an error, and no container restarted.

No real input arrived between the seventh release and this release. The organize, extraction, and attachment paths still have no live job.

## Ninth Release on the Same Day

Released revision: `81f4c9e0e72c4d721391adf317b99a14bccc16ea`. Earlier revision: `a729396`.
Pull requests #333 and #335 give the changes: the rules that change one item by one command left the storage package, and the evaluation runner names the product's fixed errors in a failure.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 25 work items.
The live check on the real default Codex channel passed: an answer, an arrangement (`create_task`, done), and a change (`update`, done), between 16 and 18 seconds each, with settled calls and a clean group cleanup.
The two actions sent their item commands through the moved rules. The receipts name the due time, the reminder 30 minutes before, and the new date.
Neither log has an error, and no container restarted.

## Tenth Release on the Same Day

Released revision: `8196ddcc757f4b05a7983fb5dd57bea9f8f56f41`. Earlier revision: `81f4c9e`.
Pull requests #336, #338, and #339 give the changes: the context builder and the assembly of the secretary's model input left the storage package.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 25 work items.
The live check on the real default Codex channel passed: an answer, an arrangement (`create_task`, done), and a change (`update`, done), between 17 and 19 seconds each, with settled calls and a clean group cleanup.
The three turns used the moved input assembly on the owner's real memory. The change turn named the task of the turn before it by its alias, and the correct task was changed.
Neither log has an error, and no container restarted.

The storage package has 25,307 lines without tests at this revision. It had 28,355 lines at the review.

## Eleventh Release on the Same Day

Released revision: `5fa573fb01f5d6c03225b34854ebff3f519619d9`. Earlier revision: `8196ddc`.
Pull requests #340, #342, and #343 give the changes: the assembly of the deputy's model input left the storage package, and the live check got a turn that delegates one run to the deputy.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 27 work items.
The two work items more than at the tenth release are projects that the topic project job made at 13:00 UTC. They are not from a check.

The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done).
The delegated run finished as done in 42 seconds from the start of its turn. Its deputy call was returned and settled. The cleanup succeeded and the record counts are equal.
This was the first deputy run in a live check. The brief of that run was built by the moved code on the owner's real memory.
The run recorded that six source excerpts and four source contexts did not fit the brief. These counts are a reading of the brief limit against the real amount of retrieved material; they are kept as a business case and are not changed in this phase.
Neither log has an error, and no container restarted.

Since the seventh release, the live instance ran handover, project handover, and topic project jobs. It ran no organize, extraction, or attachment job, because no new input arrived.

The storage package has 25,217 lines without tests at this revision. It had 28,355 lines at the review.

## Twelfth Release on the Same Day

Released revision: `a0ef58e6aac805f092e4b5903301608d9b29812c`. Earlier revision: `5fa573f`.
Pull requests #345, #346, and #347 give the changes: the rules of creation, candidate review, and bulk commands left the storage package, with the rules for applying a secretary answer.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 28 work items.
The one work item more than at the eleventh release was made by the live instance itself (project by background_topic at 14:02). It is not from a check.

The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The cleanup succeeded and the record counts are equal. Neither log has an error, and no container restarted.

The storage package has 25,082 lines without tests at this revision. It had 28,355 lines at the review.

## Thirteenth Release on the Same Day

Released revision: `68c80ea91c9737cd8380136561d2ed751e161c96`. Earlier revision: `a0ef58e`.
Pull requests #348, #350, #351, and #352 give the changes: the deputy's self-check and cost rules, the registered instruction sentences of the context builder, and the rules of a statement before its write.

One check of #351 failed on its first run: the plan test of the mocked browser suite. The run was in the first minutes after midnight of the workspace zone of the fixture, where the end of an overdue bar and the now line are on the same pixel. The test required a strict difference. It now requires that the bar does not pass now, and the check passed.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 28 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The cleanup succeeded and the record counts are equal. Neither log has an error, and no container restarted.

The storage package has 25,027 lines without tests at this revision. It had 28,355 lines at the review.

## Fourteenth Release on the Same Day

Released revision: `7b430126faee6fa764be959538cdd5aa4f295444`. Earlier revision: `68c80ea`.
Pull requests #353 and #355 give the change: the rules for a change of the owner's settings and of an agent's settings left the storage package.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 28 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The cleanup succeeded and the record counts are equal. Neither log has an error, and no container restarted.
The live check changes no setting, so the moved settings rules have their CI results and no live result in this record.

The storage package has 24,987 lines without tests at this revision. It had 28,355 lines at the review.

## Fifteenth Release on the Same Day

Released revision: `85e7d2681d3018dbf250ad06b4f599fd1d48285c`. Earlier revision: `7b43012`.
Pull requests #356 and #358 give the change: the key, quotation, time, and scope rules of a memory write left the storage package.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 28 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The cleanup succeeded and the record counts are equal. Neither log has an error, and no container restarted.
The live check writes no memory, so the moved rules of a memory write have their CI results and no live result in this record.

The storage package has 24,966 lines without tests at this revision. It had 28,355 lines at the review.

## Sixteenth Release on the Same Day

Released revision: `9f190973b59aaabcb4eaaf141c5e0cd39fa82f7d`. Earlier revision: `85e7d26`.
Pull requests #360, #361, and #362 give the changes: more rules of the secretary turn and the rules of adopting a deputy result left the storage package.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 28 work items.

The first live check failed on its fourth turn. The Codex generation of that turn did not finish in the secretary's limit of 30 seconds (29.3 seconds at the provider adapter).
The product did what it does for a timeout: the call is recorded with the outcome `unknown` and a held reservation, and the owner's sentence was kept with a capture receipt. No run was requested.
The first three turns of that check passed, and its cleanup succeeded with equal record counts.
The second live check passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
In the 30 hours before this record the live instance made 58 secretary answer calls. This timeout is the only one that did not return.
The release was not rolled back: the failure is a provider timeout, and the changed rules ran in the turns that passed.
Neither log has an error, and no container restarted.

The storage package has 24,828 lines without tests at this revision. It had 28,355 lines at the review.

## Seventeenth Release

Released revision: `afac27eb7f8ca6f303ad6fadd946d19b08ba2fa1`, on 2026-10-12 UTC. Earlier revision: `9f19097`.
Pull requests #364, #365, #366, and #367 give the changes: the rules of the deputy run commands and of memory retrieval left the storage package, and an archive import that was stopped is no longer recorded as a storage failure.

The first run of #366 failed on `TestPhase2B4_I2_PartialImportOriginalReachesNewSecretaryConversation` with `import_storage_failed`. The test cancels its import at the end. The classification of an import failure looked at the error only, and the database driver can return an error that does not wrap the context's error. Pull request #367 lets the context decide, with a regression test. The failure was not reproduced on this host; the second run passed with the correction.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.
The work items that are new since 12:00 UTC on 2026-10-11 were made by the live instance itself (4 made by background_topic). They are not from a check.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The cleanup succeeded and the record counts are equal. Neither log has an error, and no container restarted.

The storage package has 24,641 lines without tests at this revision. It had 28,355 lines at the review.

## Eighteenth Release

Released revision: `f5a5fd6e75897bf1200d1892afb3e934e280197e`, on 2026-10-12 UTC. Earlier revision: `afac27e`.
Pull requests #369, #370, and #371 give the changes: queued work has causal identities and is a recorded event, and the applying of a secretary answer is orchestrated outside the storage package.

This release has a database migration, `063_recorded_events.sql`. The owner approved it on 2026-10-12.
Before the release the migration ran on a copy of the live database from the latest backup: 0.13 seconds, with 166,049 job rows and 5,253 claims before and after. The copy was removed.
On the live instance the backup was validated before the migration. The record counts are equal before and after it: 101,944 memory records, 5,253 claims, 27,478 sources, 29 work items, and 166,049 job rows. The schema is at `063_recorded_events.sql`.

The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The cleanup succeeded and the record counts are equal. Neither log has an error, and no container restarted.
`recorded_events` was empty after the check: the check-only turns queue no work. The first rows come with the next queued job.

The storage package has 24,625 lines without tests at this revision. It had 28,355 lines at the review.

## Nineteenth Release

Released revision: `d0af154409862153b027acab7dc43f1a0ed73a0a`, on 2026-10-12 UTC. Earlier revision: `f5a5fd6`.
Pull requests #372, #374, #375, and #376 give the changes: a secretary turn is conducted outside the storage package, and the PostgreSQL slices of CI are balanced by time.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
Each of the four turns ran through `secretary.Conduct`. The cleanup succeeded and the record counts are equal. Neither log has an error, and no container restarted.

The full CI run of #376 was the first with the new slices: the slowest PostgreSQL slice took 12 minutes 36 seconds. It took 24 minutes 21 seconds on #371.

The storage package has 24,636 lines without tests at this revision. It had 28,355 lines at the review.

## Twentieth Release

Released revision: `1050c24473c1c061ce1574563525ec6f98a59daa`, on 2026-10-12 UTC. Earlier revision: `d0af154`.
Pull requests #377 and #378 give the changes: one CI run for one change, and the refresh of a run's memory part outside the storage package. Both went by one pull request straight into `main`.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call. That run refreshed its memory part through the moved rule.
The cleanup succeeded and the record counts are equal. The worker log has no error, and no container restarted.

The storage package has 24,620 lines without tests at this revision. It had 28,355 lines at the review.

## Twenty-First Release

Released revision: `ec9bd65f0c0ebe16e99b18c16b3d196921ba6147`, on 2026-10-12 UTC. Earlier revision: `1050c24`.
Pull request #380 gives the change: the storage package makes no model call and holds no gateway. Each of its ten calls has an owner in a domain package, and `internal/app` builds the gateway. The [domain pilot page](../tasks/foundation-domain-pilot.md#model-calls-outside-the-storage-package) gives each owner.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
Each turn made one settled query embedding call and one settled answer call without a fallback. These calls and the deputy's answer call went through the moved owners.
The cleanup succeeded and the record counts are equal. The API log and the worker log have no error, and no container restarted.

Not checked on live: no background job ran after the release, so the moved generation, extraction, attachment, and embedding job paths had no live job yet.

The storage package has 23,870 lines without tests at this revision. It had 28,355 lines at the review.

## Twenty-Second Release

Released revision: `8ca42aed9c182432ac9ee2ac63e43829855b54c8`, on 2026-10-12 UTC. Earlier revision: `ec9bd65`.
Pull requests #382, #383, and #384 give the changes: the first steps of the split of the PostgreSQL adapter by domain. The shared kernel holds the core of the store and the shared helpers, and five storage packages left the main one: `memoryread`, `memorystore`, `usagelog`, `schedulestore`, and `activitystore`. The [domain pilot page](../tasks/foundation-domain-pilot.md#storage-packages) gives each package.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The cleanup succeeded and the record counts are equal. The API log and the worker log have no error, and no container restarted.

Not checked on live: no background job ran after the release.

The main storage package has 21,429 lines without tests at this revision. It had 28,355 lines at the review.

## Twenty-Third Release

Released revision: `c9658b711cc53059f6b3f1a09971a23b3fb8cf62`, on 2026-10-12 UTC. Earlier revision: `8ca42ae`.
Pull requests #386, #387, and #388 give the changes. The PostgreSQL adapter is split into 25 storage packages by domain over a shared kernel. The items table has one owner, `internal/postgres/itemstore`, and a test forbids a write of `work_items` in each other storage package. The [domain pilot page](../tasks/foundation-domain-pilot.md#storage-packages) gives each package and the known limits.

The first CI run of #388 failed in five jobs. The causes were in tests: two helpers built a store without its packages, the store kept a second unused copy of the pending-result caches that two tests read, and the owner path of the call journal in the provider boundary test was the earlier file path. The second run passed.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
After the check, 19 read-only routes of the live API were called once each: the workspace snapshot, the memory list and one memory, the facets and groups, the schedule, the day activity, the usage views, the handover, the deadlines, the background health, the connectors, and the models. Each returned status 200.
The cleanup succeeded and the record counts are equal. The API log and the worker log have no error and no panic, and no container restarted.

Not checked on live: no background job ran after the release.

The main storage package has 1,983 lines without tests at this revision. It had 28,355 lines at the review.

## Twenty-Fourth Release

Released revision: `a39eacfa3ecfca6dae1539f0b443c18196fa6c98`, on 2026-10-12 UTC. Earlier revision: `c9658b7`.
Pull request #390 gives the change: `deputy.Conduct` in the deputy's domain package conducts one leased run. The storage package keeps the recovery of interrupted runs, the lease, the journal reads, and the last write.

The release had no database migration. The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.

The first live check did not pass. The provider answered three calls with `serverOverloaded` in 80 seconds: two secretary answer calls and the deputy's answer call.
Each secretary turn made its one permitted second call and finished: the arrangement and the delegation were done. The check counts two answer calls in one turn as a failure.
The deputy's run ended as failed with its call recorded as failed and settled. A deputy run is not submitted again, which is the rule before this release.
The second live check, four minutes later, passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done with a settled call.
The two checks together show the moved run on live in both outcomes: a failed answer call and a returned one.
The cleanup of each check succeeded and the record counts are equal. The API log and the worker log have no error of the application and no panic, and no container restarted.

Not checked on live: no background job ran after the release.

## Twenty-Fifth Release

Released revision: `f47e9f8e54e957a265c3836ce4e3cedd01e77b75`, on 2026-10-12 UTC. Earlier revision: `a39eacf`.
Pull request #393 gives the change: a depth limit of 200 for a causal chain of queue jobs, and a retention of 90 days for recorded events. Pull request #392 added the nightly real-model evaluation script; it changes no product code.

The release applied migration `064_event_limits.sql` (three indexes). The backup was validated, and the record counts are equal before and after the start: 5,253 claims and 29 work items.
The live check on the real default Codex channel passed with four turns: an answer, an arrangement (`create_task`, done), a change (`update`, done), and a delegation (`delegate`, done) whose deputy run finished as done.
On the live database the statement of the limit uses the index `memory_jobs_deep_chain_idx`, and the statement of the retention reads the open jobs through an index. No job has the code `chain_limit`.
The cleanup succeeded and the record counts are equal. The API log and the worker log have no error and no panic.

The nightly evaluation is scheduled in the host's crontab at 15:30 UTC. It makes no call until a dedicated Codex home is signed in at `/root/PCAS-private/codex-eval`; the [nightly evaluation page](../tasks/foundation-nightly-evaluation.md) gives the limit and the rehearsal.

At the time of the release no background job had run on live, and the table of recorded events had no row.

### First Real Background Jobs After This Release

The owner used the product on 2026-10-13 at 00:26 UTC: three secretary turns. These are the first real jobs on the paths that phase 3.9 moved out of storage.

| Item | Result |
|---|---|
| Jobs from the three turns | 3 `source.chunk`, 3 `source.extract`, 3 `source.embed`, 3 `source.tokenize`, 4 `memory.summary`, 1 `memory.index`, 1 `memory.embed`, then 1 `memory.organize`, 1 `memory.entity_candidates`, and 5 `memory.compare`. Each job finished at its first attempt. |
| Model calls | 3 extraction, 4 background embedding, 3 query embedding, 4 secretary, 1 organize, 1 alias scan, 5 compare. Each call returned and each reservation is settled. |
| Scheduled jobs since the release | Handover, project handover, entity candidates, and topic project calls returned without a failure. |
| Recorded events | 28 rows. The deepest chain has depth 3 (`source.chunk`, `source.extract`, `memory.summary`, then `memory.index` and `memory.embed`). The limit is 200. No job has the error `chain_limit`. |
| Logs | The API and the worker wrote no warning and no error from 00:20 UTC to 00:44 UTC. |
| Stage outcomes | 25 successes on that day. The other counts are the counted overflows of the recall budgets, which this release did not change. |

Not yet observed on live: an attachment job (`source.parse`). The three turns had no attachment.

## Host Cleanup

The owner approved removal of leftover data on 2026-10-11.
Removed: 19 disposable test containers, 13 unreferenced volumes, 57 older image tags, 64 older release directories, deploy backups before `20261008T192836Z-6b62f27`, five older foundation backups, and 41 worktrees that were clean and fully merged.
Kept: the images and release directories of the last five releases, the four full backups of this day and the backup before `6411b4e`, each worktree with unmerged or uncommitted work, and the private evidence directory.
Free space on the root filesystem went from 7 GB to 39 GB. The API stayed ready during the cleanup.

The owner approved removal of old backups on 2026-10-12. Removed: 19 release backups before `1050c24`. Kept: the backups of the last three releases at that time, the backup before `6411b4e`, and the early small dumps.
