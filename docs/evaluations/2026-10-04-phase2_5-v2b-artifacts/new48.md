# Phase 2.5 doing evaluation

model `gpt-6.1-sol`; channel `codex`; fake=false; repeats=3; suite SHA `f26bfdff04c5b4d32b2b5eb9d83a091586a873ad09acdb2ea5a60e868731d0fa`; revision `c1fe86cff0eae2bb3499698f047e5f105d19358c`.

| Method | Category | Run | Tasks | Must % | Bonus % | Forbidden hits | Usable % | Input chars | Model calls | Doing ms | Total ms | Disputed |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| frozen-current | all | 1 | 48 | 93.12 | 80.00 | 1 | 81.25 | 3240 | 144 | 8038 | 18968 | 1 |
| frozen-current | all | 2 | 48 | 93.12 | 80.00 | 1 | 83.33 | 3240 | 144 | 8763 | 20357 | 2 |
| frozen-current | all | 3 | 48 | 93.65 | 80.00 | 1 | 81.25 | 3240 | 144 | 7966 | 20339 | 0 |
| frozen-current | cross_group | 1 | 12 | 92.86 | 100.00 | 0 | 83.33 | 3349 | 36 | 9684 | 21220 | 0 |
| frozen-current | cross_group | 2 | 12 | 92.86 | 100.00 | 0 | 83.33 | 3349 | 36 | 10881 | 23460 | 0 |
| frozen-current | cross_group | 3 | 12 | 92.86 | 100.00 | 0 | 75.00 | 3349 | 36 | 8815 | 21876 | 0 |
| frozen-current | direct_recall | 1 | 4 | 91.67 | 100.00 | 0 | 75.00 | 3261 | 12 | 5763 | 17634 | 0 |
| frozen-current | direct_recall | 2 | 4 | 91.67 | 100.00 | 0 | 75.00 | 3261 | 12 | 8756 | 17617 | 0 |
| frozen-current | direct_recall | 3 | 4 | 91.67 | 100.00 | 0 | 75.00 | 3261 | 12 | 4974 | 14151 | 0 |
| frozen-current | indirect_use | 1 | 4 | 70.00 | 100.00 | 0 | 50.00 | 3130 | 12 | 12403 | 23034 | 0 |
| frozen-current | indirect_use | 2 | 4 | 70.00 | 100.00 | 0 | 50.00 | 3130 | 12 | 11005 | 23732 | 1 |
| frozen-current | indirect_use | 3 | 4 | 70.00 | 100.00 | 0 | 50.00 | 3130 | 12 | 12562 | 27302 | 0 |
| frozen-current | irrelevant | 1 | 10 | 100.00 | 0.00 | 0 | 100.00 | 3033 | 30 | 5005 | 14836 | 0 |
| frozen-current | irrelevant | 2 | 10 | 100.00 | 0.00 | 0 | 100.00 | 3033 | 30 | 6558 | 16949 | 0 |
| frozen-current | irrelevant | 3 | 10 | 100.00 | 0.00 | 0 | 100.00 | 3033 | 30 | 5537 | 16364 | 0 |
| frozen-current | outgoing | 1 | 12 | 98.11 | 100.00 | 0 | 83.33 | 3285 | 36 | 8345 | 19194 | 0 |
| frozen-current | outgoing | 2 | 12 | 98.11 | 100.00 | 0 | 83.33 | 3285 | 36 | 7573 | 20318 | 1 |
| frozen-current | outgoing | 3 | 12 | 100.00 | 100.00 | 0 | 91.67 | 3285 | 36 | 8241 | 21413 | 0 |
| frozen-current | updated_fact | 1 | 6 | 94.44 | 0.00 | 1 | 66.67 | 3339 | 18 | 7791 | 19083 | 1 |
| frozen-current | updated_fact | 2 | 6 | 94.44 | 0.00 | 1 | 83.33 | 3339 | 18 | 9092 | 19486 | 0 |
| frozen-current | updated_fact | 3 | 6 | 94.44 | 0.00 | 1 | 66.67 | 3339 | 18 | 8695 | 21229 | 0 |
| ideal | all | 1 | 48 | 100.00 | 100.00 | 0 | 97.92 | 400 | 144 | 8356 | 19534 | 0 |
| ideal | all | 2 | 48 | 99.47 | 80.00 | 0 | 95.83 | 400 | 144 | 8145 | 19603 | 1 |
| ideal | all | 3 | 48 | 100.00 | 80.00 | 0 | 95.83 | 400 | 144 | 7446 | 19099 | 0 |
| ideal | cross_group | 1 | 12 | 100.00 | 100.00 | 0 | 100.00 | 448 | 36 | 9564 | 21540 | 0 |
| ideal | cross_group | 2 | 12 | 100.00 | 100.00 | 0 | 100.00 | 448 | 36 | 8605 | 20044 | 0 |
| ideal | cross_group | 3 | 12 | 100.00 | 100.00 | 0 | 100.00 | 448 | 36 | 8646 | 20830 | 0 |
| ideal | direct_recall | 1 | 4 | 100.00 | 100.00 | 0 | 100.00 | 400 | 12 | 7520 | 18206 | 0 |
| ideal | direct_recall | 2 | 4 | 100.00 | 100.00 | 0 | 100.00 | 400 | 12 | 9007 | 19263 | 0 |
| ideal | direct_recall | 3 | 4 | 100.00 | 100.00 | 0 | 100.00 | 400 | 12 | 6141 | 14495 | 0 |
| ideal | indirect_use | 1 | 4 | 100.00 | 100.00 | 0 | 100.00 | 392 | 12 | 9156 | 18768 | 0 |
| ideal | indirect_use | 2 | 4 | 100.00 | 100.00 | 0 | 100.00 | 392 | 12 | 7817 | 17410 | 0 |
| ideal | indirect_use | 3 | 4 | 100.00 | 100.00 | 0 | 100.00 | 392 | 12 | 7260 | 21459 | 0 |
| ideal | irrelevant | 1 | 10 | 100.00 | 0.00 | 0 | 100.00 | 257 | 30 | 6460 | 16879 | 0 |
| ideal | irrelevant | 2 | 10 | 100.00 | 0.00 | 0 | 100.00 | 257 | 30 | 6001 | 17590 | 0 |
| ideal | irrelevant | 3 | 10 | 100.00 | 0.00 | 0 | 100.00 | 257 | 30 | 4431 | 15836 | 0 |
| ideal | outgoing | 1 | 12 | 100.00 | 100.00 | 0 | 91.67 | 473 | 36 | 8644 | 20583 | 0 |
| ideal | outgoing | 2 | 12 | 98.11 | 100.00 | 0 | 83.33 | 473 | 36 | 8109 | 21110 | 1 |
| ideal | outgoing | 3 | 12 | 100.00 | 100.00 | 0 | 91.67 | 473 | 36 | 8560 | 20369 | 0 |
| ideal | updated_fact | 1 | 6 | 100.00 | 100.00 | 0 | 100.00 | 402 | 18 | 8545 | 19250 | 0 |
| ideal | updated_fact | 2 | 6 | 100.00 | 0.00 | 0 | 100.00 | 402 | 18 | 10512 | 20750 | 0 |
| ideal | updated_fact | 3 | 6 | 100.00 | 0.00 | 0 | 83.33 | 402 | 18 | 8834 | 20034 | 0 |
| none | all | 1 | 48 | 34.39 | 20.00 | 8 | 20.83 | 254 | 144 | 10266 | 23595 | 2 |
| none | all | 2 | 48 | 33.86 | 20.00 | 5 | 20.83 | 254 | 144 | 10587 | 23520 | 1 |
| none | all | 3 | 48 | 34.39 | 20.00 | 4 | 20.83 | 254 | 144 | 10100 | 23194 | 3 |
| none | cross_group | 1 | 12 | 23.21 | 0.00 | 3 | 0.00 | 256 | 36 | 11086 | 25456 | 1 |
| none | cross_group | 2 | 12 | 21.43 | 0.00 | 1 | 0.00 | 256 | 36 | 11685 | 25100 | 1 |
| none | cross_group | 3 | 12 | 21.43 | 0.00 | 2 | 0.00 | 256 | 36 | 10710 | 23600 | 2 |
| none | direct_recall | 1 | 4 | 0.00 | 0.00 | 1 | 0.00 | 249 | 12 | 10570 | 27241 | 0 |
| none | direct_recall | 2 | 4 | 0.00 | 0.00 | 0 | 0.00 | 249 | 12 | 15195 | 26222 | 0 |
| none | direct_recall | 3 | 4 | 0.00 | 0.00 | 0 | 0.00 | 249 | 12 | 9819 | 21249 | 0 |
| none | indirect_use | 1 | 4 | 0.00 | 0.00 | 0 | 0.00 | 253 | 12 | 11708 | 21860 | 0 |
| none | indirect_use | 2 | 4 | 0.00 | 0.00 | 0 | 0.00 | 253 | 12 | 11112 | 23905 | 0 |
| none | indirect_use | 3 | 4 | 0.00 | 0.00 | 0 | 0.00 | 253 | 12 | 15574 | 27311 | 0 |
| none | irrelevant | 1 | 10 | 100.00 | 0.00 | 0 | 100.00 | 257 | 30 | 5272 | 16553 | 0 |
| none | irrelevant | 2 | 10 | 100.00 | 0.00 | 0 | 100.00 | 257 | 30 | 6342 | 17084 | 0 |
| none | irrelevant | 3 | 10 | 100.00 | 0.00 | 0 | 100.00 | 257 | 30 | 6612 | 17526 | 0 |
| none | outgoing | 1 | 12 | 41.51 | 100.00 | 3 | 0.00 | 253 | 36 | 11135 | 26075 | 1 |
| none | outgoing | 2 | 12 | 41.51 | 100.00 | 3 | 0.00 | 253 | 36 | 10825 | 26421 | 0 |
| none | outgoing | 3 | 12 | 43.40 | 100.00 | 2 | 0.00 | 253 | 36 | 10167 | 26378 | 1 |
| none | updated_fact | 1 | 6 | 0.00 | 0.00 | 1 | 0.00 | 254 | 18 | 14046 | 25375 | 0 |
| none | updated_fact | 2 | 6 | 0.00 | 0.00 | 1 | 0.00 | 254 | 18 | 11569 | 23227 | 0 |
| none | updated_fact | 3 | 6 | 0.00 | 0.00 | 0 | 0.00 | 254 | 18 | 11094 | 24017 | 0 |

| Method | Category | Must range % | Usable range % | Forbidden range | Indifference floor pp |
|---|---|---:|---:|---:|---:|
| frozen-current | all | 93.12–93.65 | 81.25–83.33 | 1–1 | 2.08 |
| frozen-current | cross_group | 92.86–92.86 | 75.00–83.33 | 0–0 | 8.33 |
| frozen-current | direct_recall | 91.67–91.67 | 75.00–75.00 | 0–0 | 0.00 |
| frozen-current | indirect_use | 70.00–70.00 | 50.00–50.00 | 0–0 | 0.00 |
| frozen-current | irrelevant | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| frozen-current | outgoing | 98.11–100.00 | 83.33–91.67 | 0–0 | 8.33 |
| frozen-current | updated_fact | 94.44–94.44 | 66.67–83.33 | 1–1 | 16.67 |
| ideal | all | 99.47–100.00 | 95.83–97.92 | 0–0 | 2.08 |
| ideal | cross_group | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| ideal | direct_recall | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| ideal | indirect_use | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| ideal | irrelevant | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| ideal | outgoing | 98.11–100.00 | 83.33–91.67 | 0–0 | 8.33 |
| ideal | updated_fact | 100.00–100.00 | 83.33–100.00 | 0–0 | 16.67 |
| none | all | 33.86–34.39 | 20.83–20.83 | 4–8 | 0.53 |
| none | cross_group | 21.43–23.21 | 0.00–0.00 | 1–3 | 1.79 |
| none | direct_recall | 0.00–0.00 | 0.00–0.00 | 0–1 | 0.00 |
| none | indirect_use | 0.00–0.00 | 0.00–0.00 | 0–0 | 0.00 |
| none | irrelevant | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| none | outgoing | 41.51–43.40 | 0.00–0.00 | 2–3 | 1.89 |
| none | updated_fact | 0.00–0.00 | 0.00–0.00 | 0–1 | 0.00 |

Judge disagreements (task IDs and check IDs only):

- run=1 frozen-current I-UPDATE-01: handling

- run=1 none I-CROSS-07: F1

- run=1 none I-OUT-07: handling

- run=2 frozen-current I-OUT-08: M3

- run=2 frozen-current I-PLAN-01: handling

- run=2 ideal I-OUT-08: M3

- run=2 none I-CROSS-06: M2

- run=3 none I-CROSS-06: M2

- run=3 none I-CROSS-07: F1

- run=3 none I-OUT-07: handling

- Cohort=new48. Old tasks are unchanged but evaluated against the extended 801-memory corpus. frozen-current replays the actual product route captured once per task on the suite's UTC date; row evidence_ms is replay overhead, not retrieval latency. Raw method label is retained. See capture-manifest.json for one-off retrieval cost and provenance.

- Must and bonus require both independent judgments; forbidden counts either judgment. Usable requires all must, zero forbidden, both handling and at most 600 Unicode characters. Instructions and model are unchanged from V2.

- Initial capture measures actual DeskTurn keyword fallback without embeddings; seeded flat facts and no-action local capture remain the V2 protocol. Three model repetitions reuse the fixed snapshot; no retrieval variability or complete action execution is measured.

- The old120 cohort contains 20 shared scenarios; new48 contains independently authored requests. The min-max floor is descriptive repeatability, not statistical significance. Both judges use the same model family as the answers. See the evaluation record for start/end dates, clock limitations and any interrupted attempts.
