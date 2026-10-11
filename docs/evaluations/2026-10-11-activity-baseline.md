# Day Activity Query: First Live Readings (2026-10-11)

The [day activity query](../memory-service.md#day-activity) ran read-only on the live database on 2026-10-11.
It returned counts and totals only. No event text left the database.
The live revision was `a5c1483`. The days are local days in the owner's time zone.

## Readings

| Measure | 2026-10-09 | 2026-10-10 |
|---|---|---|
| Events | 3,759 | 814 |
| Background stage events | 2,792 | 491 |
| Call journal rows | 372 | 312 |
| Usage rows without a call record | 361 | 0 |
| Applied actions | 118 | 10 |
| Reversals | 12 | 0 |
| Date review decisions | 104 | 1 |
| Settled cost, in the configured currency | 0.061723 | 0.017476 |
| Query duration | 152 ms | Not recorded |

Settled cost on 2026-10-09 by function: comparison 0.018945, global handover 0.013216, project handover 0.010916, date review 0.009771, secretary 0.004388, topic projects 0.004081.

Largest groups of background stage events on 2026-10-09:

| Stage | Outcome | Reason | Events | Sum of counts |
|---|---|---|---|---|
| Topic projects | deferred | `topic_project_hourly_limit` | 734 | 734 |
| Date review | deferred | `date_tidy_hourly_limit` | 600 | 600 |
| Topic projects | success | | 396 | 396 |
| Topic projects | overflow | `topic_project_evidence_left_in_group` | 346 | 12,305 |
| Topic projects | overflow | `topic_project_characters_left_in_group` | 189 | 17,174 |
| Comparison | success | | 176 | 176 |
| Date review | success | | 125 | 125 |
| Comparison | deferred | `compare_hourly_limit` | 59 | 59 |
| Global handover | failure | `schedule_background_write_busy` | 19 | 19 |

## What These Readings Show

- Recorded model spending for one day is a few hundredths of one currency unit. The readings do not show a cost problem in money.
- Queue deferrals are the largest group of events: about 1,400 on one day for two stages. A job is taken, meets its hourly limit, and returns to the queue, many times.
- On both days most call journal rows for topic projects are deferrals, not calls. Revision `a5c1483` stops this for new rows.
- Topic projects left evidence out of a group 346 times on one day. The record gives the count and not which evidence.
- Global handover scheduling failed 19 times on one day with `schedule_background_write_busy`.

## Limits

- Two days are not a trend.
- The usage rows without a call record on 2026-10-09 were written by paths that entered the gateway on that day or later.
- The query cannot show reasons that were never recorded. Its response lists these gaps.
