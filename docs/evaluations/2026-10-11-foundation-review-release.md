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
