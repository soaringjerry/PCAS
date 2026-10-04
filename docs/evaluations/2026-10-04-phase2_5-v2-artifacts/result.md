# Phase 2.5 doing evaluation

model `gpt-6.1-sol`; channel `codex`; fake=false; repeats=3; suite SHA `c8d38bad30381a308722ca25c07bdf3d84b537a3464dde62323e5130ab77396b`; revision `0d491476b9f20a9aef77d0dc6864b7bb0cd61c1d`.

| Method | Category | Run | Tasks | Must % | Bonus % | Forbidden hits | Usable % | Input chars | Model calls | Doing ms | Total ms | Disputed |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| current | all | 1 | 120 | 87.03 | 66.00 | 2 | 66.67 | 2758 | 360 | 10664 | 24880 | 2 |
| current | all | 2 | 120 | 87.03 | 66.00 | 3 | 65.83 | 2758 | 360 | 10870 | 24659 | 4 |
| current | all | 3 | 120 | 87.23 | 68.00 | 1 | 67.50 | 2758 | 360 | 10816 | 24543 | 5 |
| current | cross_group | 1 | 20 | 74.00 | 40.00 | 2 | 30.00 | 3020 | 60 | 10583 | 25560 | 0 |
| current | cross_group | 2 | 20 | 76.00 | 40.00 | 2 | 40.00 | 3020 | 60 | 11783 | 26859 | 1 |
| current | cross_group | 3 | 20 | 76.00 | 45.00 | 1 | 40.00 | 3020 | 60 | 10872 | 25600 | 0 |
| current | direct_recall | 1 | 20 | 98.33 | 100.00 | 0 | 95.00 | 2935 | 60 | 10147 | 22991 | 0 |
| current | direct_recall | 2 | 20 | 98.33 | 100.00 | 0 | 95.00 | 2935 | 60 | 10670 | 21961 | 0 |
| current | direct_recall | 3 | 20 | 98.33 | 100.00 | 0 | 95.00 | 2935 | 60 | 10526 | 22020 | 1 |
| current | indirect_use | 1 | 20 | 96.00 | 95.00 | 0 | 85.00 | 2989 | 60 | 15615 | 29005 | 0 |
| current | indirect_use | 2 | 20 | 95.00 | 95.00 | 0 | 75.00 | 2989 | 60 | 15381 | 28165 | 0 |
| current | indirect_use | 3 | 20 | 96.00 | 95.00 | 0 | 80.00 | 2989 | 60 | 15687 | 28632 | 1 |
| current | irrelevant | 1 | 20 | 100.00 | 0.00 | 0 | 100.00 | 1569 | 60 | 5575 | 17578 | 0 |
| current | irrelevant | 2 | 20 | 100.00 | 0.00 | 0 | 100.00 | 1569 | 60 | 5735 | 18158 | 0 |
| current | irrelevant | 3 | 20 | 100.00 | 0.00 | 0 | 100.00 | 1569 | 60 | 5435 | 17575 | 0 |
| current | outgoing | 1 | 20 | 68.00 | 10.00 | 0 | 5.00 | 3025 | 60 | 8369 | 26246 | 0 |
| current | outgoing | 2 | 20 | 68.00 | 5.00 | 1 | 0.00 | 3025 | 60 | 8724 | 25971 | 2 |
| current | outgoing | 3 | 20 | 68.00 | 10.00 | 0 | 5.00 | 3025 | 60 | 9096 | 26339 | 2 |
| current | updated_fact | 1 | 20 | 97.50 | 85.00 | 0 | 85.00 | 3007 | 60 | 13692 | 27899 | 2 |
| current | updated_fact | 2 | 20 | 96.25 | 90.00 | 0 | 85.00 | 3007 | 60 | 12926 | 26840 | 1 |
| current | updated_fact | 3 | 20 | 96.25 | 90.00 | 0 | 85.00 | 3007 | 60 | 13279 | 27095 | 1 |
| ideal | all | 1 | 120 | 98.40 | 0.00 | 0 | 91.67 | 459 | 360 | 9149 | 21591 | 4 |
| ideal | all | 2 | 120 | 98.80 | 1.00 | 0 | 95.00 | 459 | 360 | 9203 | 21458 | 2 |
| ideal | all | 3 | 120 | 98.60 | 1.00 | 0 | 92.50 | 459 | 360 | 9275 | 21466 | 3 |
| ideal | cross_group | 1 | 20 | 99.00 | 0.00 | 0 | 95.00 | 540 | 60 | 9280 | 21367 | 0 |
| ideal | cross_group | 2 | 20 | 99.00 | 0.00 | 0 | 95.00 | 540 | 60 | 9165 | 20938 | 1 |
| ideal | cross_group | 3 | 20 | 100.00 | 0.00 | 0 | 100.00 | 540 | 60 | 9272 | 21031 | 0 |
| ideal | direct_recall | 1 | 20 | 98.33 | 0.00 | 0 | 95.00 | 411 | 60 | 7560 | 21347 | 1 |
| ideal | direct_recall | 2 | 20 | 98.33 | 0.00 | 0 | 95.00 | 411 | 60 | 7095 | 18722 | 0 |
| ideal | direct_recall | 3 | 20 | 98.33 | 0.00 | 0 | 95.00 | 411 | 60 | 7514 | 19886 | 1 |
| ideal | indirect_use | 1 | 20 | 98.00 | 0.00 | 0 | 90.00 | 540 | 60 | 14874 | 26916 | 1 |
| ideal | indirect_use | 2 | 20 | 98.00 | 5.00 | 0 | 90.00 | 540 | 60 | 15040 | 27064 | 0 |
| ideal | indirect_use | 3 | 20 | 98.00 | 5.00 | 0 | 90.00 | 540 | 60 | 15719 | 27321 | 1 |
| ideal | irrelevant | 1 | 20 | 100.00 | 0.00 | 0 | 100.00 | 236 | 60 | 5811 | 18441 | 0 |
| ideal | irrelevant | 2 | 20 | 100.00 | 0.00 | 0 | 100.00 | 236 | 60 | 5653 | 18604 | 0 |
| ideal | irrelevant | 3 | 20 | 100.00 | 0.00 | 0 | 100.00 | 236 | 60 | 5729 | 17135 | 0 |
| ideal | outgoing | 1 | 20 | 98.00 | 0.00 | 0 | 90.00 | 532 | 60 | 7628 | 19541 | 0 |
| ideal | outgoing | 2 | 20 | 99.00 | 0.00 | 0 | 95.00 | 532 | 60 | 7634 | 20376 | 0 |
| ideal | outgoing | 3 | 20 | 99.00 | 0.00 | 0 | 90.00 | 532 | 60 | 7572 | 20661 | 0 |
| ideal | updated_fact | 1 | 20 | 97.50 | 0.00 | 0 | 80.00 | 496 | 60 | 9738 | 21938 | 2 |
| ideal | updated_fact | 2 | 20 | 98.75 | 0.00 | 0 | 95.00 | 496 | 60 | 10631 | 23042 | 1 |
| ideal | updated_fact | 3 | 20 | 96.25 | 0.00 | 0 | 80.00 | 496 | 60 | 9843 | 22761 | 1 |
| none | all | 1 | 120 | 44.31 | 4.00 | 18 | 16.67 | 248 | 360 | 10711 | 25163 | 6 |
| none | all | 2 | 120 | 44.11 | 3.00 | 16 | 16.67 | 248 | 360 | 11087 | 25638 | 5 |
| none | all | 3 | 120 | 44.31 | 5.00 | 16 | 16.67 | 248 | 360 | 10571 | 25068 | 6 |
| none | cross_group | 1 | 20 | 40.00 | 0.00 | 0 | 0.00 | 249 | 60 | 10622 | 25819 | 0 |
| none | cross_group | 2 | 20 | 40.00 | 0.00 | 0 | 0.00 | 249 | 60 | 10817 | 26477 | 1 |
| none | cross_group | 3 | 20 | 40.00 | 5.00 | 0 | 0.00 | 249 | 60 | 11352 | 25614 | 1 |
| none | direct_recall | 1 | 20 | 0.00 | 0.00 | 0 | 0.00 | 247 | 60 | 11001 | 23650 | 0 |
| none | direct_recall | 2 | 20 | 0.00 | 0.00 | 0 | 0.00 | 247 | 60 | 11760 | 24589 | 0 |
| none | direct_recall | 3 | 20 | 0.00 | 0.00 | 0 | 0.00 | 247 | 60 | 10351 | 22785 | 1 |
| none | indirect_use | 1 | 20 | 39.00 | 5.00 | 0 | 0.00 | 241 | 60 | 15609 | 29269 | 0 |
| none | indirect_use | 2 | 20 | 38.00 | 5.00 | 0 | 0.00 | 241 | 60 | 15565 | 29740 | 1 |
| none | indirect_use | 3 | 20 | 39.00 | 10.00 | 0 | 0.00 | 241 | 60 | 14504 | 28689 | 0 |
| none | irrelevant | 1 | 20 | 100.00 | 0.00 | 0 | 100.00 | 236 | 60 | 5155 | 18582 | 0 |
| none | irrelevant | 2 | 20 | 100.00 | 0.00 | 0 | 100.00 | 236 | 60 | 6065 | 18474 | 0 |
| none | irrelevant | 3 | 20 | 100.00 | 0.00 | 0 | 100.00 | 236 | 60 | 5187 | 17196 | 0 |
| none | outgoing | 1 | 20 | 60.00 | 5.00 | 0 | 0.00 | 242 | 60 | 9317 | 26141 | 1 |
| none | outgoing | 2 | 20 | 59.00 | 0.00 | 0 | 0.00 | 242 | 60 | 8938 | 25197 | 0 |
| none | outgoing | 3 | 20 | 60.00 | 0.00 | 0 | 0.00 | 242 | 60 | 9275 | 26992 | 3 |
| none | updated_fact | 1 | 20 | 27.50 | 10.00 | 18 | 0.00 | 275 | 60 | 12561 | 27516 | 5 |
| none | updated_fact | 2 | 20 | 28.75 | 10.00 | 16 | 0.00 | 275 | 60 | 13377 | 29349 | 3 |
| none | updated_fact | 3 | 20 | 27.50 | 10.00 | 16 | 0.00 | 275 | 60 | 12755 | 29134 | 1 |

| Method | Category | Must range % | Usable range % | Forbidden range | Indifference floor pp |
|---|---|---:|---:|---:|---:|
| current | all | 87.03–87.23 | 65.83–67.50 | 1–3 | 1.67 |
| current | cross_group | 74.00–76.00 | 30.00–40.00 | 1–2 | 10.00 |
| current | direct_recall | 98.33–98.33 | 95.00–95.00 | 0–0 | 0.00 |
| current | indirect_use | 95.00–96.00 | 75.00–85.00 | 0–0 | 10.00 |
| current | irrelevant | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| current | outgoing | 68.00–68.00 | 0.00–5.00 | 0–1 | 5.00 |
| current | updated_fact | 96.25–97.50 | 85.00–85.00 | 0–0 | 1.25 |
| ideal | all | 98.40–98.80 | 91.67–95.00 | 0–0 | 3.33 |
| ideal | cross_group | 99.00–100.00 | 95.00–100.00 | 0–0 | 5.00 |
| ideal | direct_recall | 98.33–98.33 | 95.00–95.00 | 0–0 | 0.00 |
| ideal | indirect_use | 98.00–98.00 | 90.00–90.00 | 0–0 | 0.00 |
| ideal | irrelevant | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| ideal | outgoing | 98.00–99.00 | 90.00–95.00 | 0–0 | 5.00 |
| ideal | updated_fact | 96.25–98.75 | 80.00–95.00 | 0–0 | 15.00 |
| none | all | 44.11–44.31 | 16.67–16.67 | 16–18 | 0.20 |
| none | cross_group | 40.00–40.00 | 0.00–0.00 | 0–0 | 0.00 |
| none | direct_recall | 0.00–0.00 | 0.00–0.00 | 0–0 | 0.00 |
| none | indirect_use | 38.00–39.00 | 0.00–0.00 | 0–0 | 1.00 |
| none | irrelevant | 100.00–100.00 | 100.00–100.00 | 0–0 | 0.00 |
| none | outgoing | 59.00–60.00 | 0.00–0.00 | 0–0 | 1.00 |
| none | updated_fact | 27.50–28.75 | 0.00–0.00 | 16–18 | 1.25 |

Judge disagreements (task IDs and check IDs only):

- run=1 none OUT-16: handling

- run=1 ideal PLAN-12: M3

- run=1 ideal RECALL-16: M3

- run=1 none UPDATE-06: handling

- run=1 ideal UPDATE-07: handling

- run=1 none UPDATE-09: handling

- run=1 none UPDATE-12: handling

- run=1 none UPDATE-13: F1

- run=1 current UPDATE-15: handling

- run=1 current UPDATE-16: B1

- run=1 none UPDATE-18: handling

- run=1 ideal UPDATE-19: handling

- run=2 ideal CROSS-04: B1

- run=2 current CROSS-06: B1

- run=2 none CROSS-19: handling

- run=2 current OUT-01: handling

- run=2 current OUT-10: handling

- run=2 none PLAN-15: B1

- run=2 none UPDATE-02: handling

- run=2 none UPDATE-09: handling

- run=2 none UPDATE-13: handling

- run=2 ideal UPDATE-16: handling

- run=2 current UPDATE-19: handling

- run=3 none CROSS-12: handling

- run=3 none OUT-05: handling

- run=3 current OUT-10: handling

- run=3 current OUT-17: handling

- run=3 none OUT-18: handling

- run=3 none OUT-20: handling

- run=3 ideal PLAN-12: M3

- run=3 current PLAN-18: M3

- run=3 none RECALL-07: handling

- run=3 current RECALL-16: M3

- run=3 ideal RECALL-16: M3

- run=3 current UPDATE-05: handling

- run=3 ideal UPDATE-14: handling

- run=3 none UPDATE-16: handling

- Must and bonus count only when both judges agree true; forbidden counts when either judge says true. Usable also requires both handling judgments and <=600 Unicode characters.

- Doing latency includes evidence construction and answer; total includes both judgments. Seeding and one model preflight call are reported outside per-task measurements.

- Current uses the actual DeskTurn recall route with a local no-action capture model, followed by the fixed answer instruction. This tests supplied-context strategies, not the complete secretary action executor.

- No embedding provider configured: current is the product keyword fallback. Adapter model_calls are declared by the adapter; local capture calls are separate.

- Repeatability floor is the maximum of the must and usable percentage-point ranges for that method/category. Differences within the larger compared floor cannot establish superiority; three runs are not a significance test.

- The 120 tasks share 20 scenarios; cluster-aware uncertainty and independent human judge audit are still needed. Judge calls use the same model as answers, with independent threads and no method identity.

- The product retrieval planner uses host time. Compare runs only on the same host date with unchanged anchored suite; no clock is injected into product code.

- Reconstructed from an interrupted run, one completion run (mapped to repetition 3), and all-three-repetition gold repair. Selection used transport completeness and changed task definitions only; see provenance.json. Row times include each row's actual measurements, not the wall time of discarded work.
