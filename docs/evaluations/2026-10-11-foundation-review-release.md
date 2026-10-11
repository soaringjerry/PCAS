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

## Host Cleanup

The owner approved removal of leftover data on 2026-10-11.
Removed: 19 disposable test containers, 13 unreferenced volumes, 57 older image tags, 64 older release directories, deploy backups before `20261008T192836Z-6b62f27`, five older foundation backups, and 41 worktrees that were clean and fully merged.
Kept: the images and release directories of the last five releases, the four full backups of this day and the backup before `6411b4e`, each worktree with unmerged or uncommitted work, and the private evidence directory.
Free space on the root filesystem went from 7 GB to 39 GB. The API stayed ready during the cleanup.
