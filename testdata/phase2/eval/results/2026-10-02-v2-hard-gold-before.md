# PCAS synthetic recall evaluation

Revision `bfa519b`; anchor 2026-10-03; model `synthetic-reflector`; fake=true; rendered corpus SHA256 `182262cf2e6d5cd302ff67fa480d98950ed25a80ec63e09e7979ace70eeda0cd`.

Native structured schema: true. Baseline 0.0000 (awaiting_coordinator).

Tier **hard**, mode retrieval, 2728 documents, 40/40 questions, gold=true, seed=20261002.

| Query type | Questions | Evidence | Recall | Inversions | Interference |
|---|---:|---:|---:|---:|---:|
| time-entity | 8 | 3/8 | 0.375000 | 109/512 | 0.212891 |
| time-only | 8 | 0/64 | 0.000000 | 176/32768 | 0.005371 |
| entity-only | 8 | 8/8 | 1.000000 | 0/512 | 0.000000 |
| wrong-time | 8 | 5/8 | 0.625000 | 80/512 | 0.156250 |
| ordinary | 8 | 8/8 | 1.000000 | 0/512 | 0.000000 |
| all | 40 | 24/96 | 0.250000 | 365/34816 | 0.010484 |


| Question | Evidence | Recall | Inversions | Interference | Memories sent | Sources sent |
|---|---:|---:|---:|---:|---:|---:|
| hard-time-entity-01 | 0/1 | 0.000000 | 19/64 | 0.296875 | 15 | 6 |
| hard-time-entity-02 | 0/1 | 0.000000 | 19/64 | 0.296875 | 15 | 6 |
| hard-time-entity-03 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-time-entity-04 | 0/1 | 0.000000 | 16/64 | 0.250000 | 15 | 6 |
| hard-time-entity-05 | 0/1 | 0.000000 | 15/64 | 0.234375 | 15 | 6 |
| hard-time-entity-06 | 1/1 | 1.000000 | 12/64 | 0.187500 | 15 | 6 |
| hard-time-entity-07 | 0/1 | 0.000000 | 15/64 | 0.234375 | 15 | 6 |
| hard-time-entity-08 | 1/1 | 1.000000 | 13/64 | 0.203125 | 15 | 6 |
| hard-time-only-01 | 0/8 | 0.000000 | 40/4096 | 0.009766 | 15 | 6 |
| hard-time-only-02 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-time-only-03 | 0/8 | 0.000000 | 40/4096 | 0.009766 | 15 | 6 |
| hard-time-only-04 | 0/8 | 0.000000 | 32/4096 | 0.007812 | 15 | 6 |
| hard-time-only-05 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-time-only-06 | 0/8 | 0.000000 | 32/4096 | 0.007812 | 15 | 6 |
| hard-time-only-07 | 0/8 | 0.000000 | 32/4096 | 0.007812 | 15 | 6 |
| hard-time-only-08 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-entity-only-01 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-entity-only-02 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-entity-only-03 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-entity-only-04 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-entity-only-05 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-entity-only-06 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-entity-only-07 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-entity-only-08 | 1/1 | 1.000000 | 0/64 | 0.000000 | 15 | 6 |
| hard-wrong-time-01 | 1/1 | 1.000000 | 8/64 | 0.125000 | 15 | 6 |
| hard-wrong-time-02 | 1/1 | 1.000000 | 7/64 | 0.109375 | 15 | 6 |
| hard-wrong-time-03 | 1/1 | 1.000000 | 2/64 | 0.031250 | 15 | 6 |
| hard-wrong-time-04 | 1/1 | 1.000000 | 11/64 | 0.171875 | 15 | 6 |
| hard-wrong-time-05 | 0/1 | 0.000000 | 15/64 | 0.234375 | 15 | 6 |
| hard-wrong-time-06 | 0/1 | 0.000000 | 15/64 | 0.234375 | 15 | 6 |
| hard-wrong-time-07 | 0/1 | 0.000000 | 15/64 | 0.234375 | 15 | 6 |
| hard-wrong-time-08 | 1/1 | 1.000000 | 7/64 | 0.109375 | 15 | 6 |
| hard-ordinary-01 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |
| hard-ordinary-02 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |
| hard-ordinary-03 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |
| hard-ordinary-04 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |
| hard-ordinary-05 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |
| hard-ordinary-06 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |
| hard-ordinary-07 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |
| hard-ordinary-08 | 1/1 | 1.000000 | 0/64 | 0.000000 | 8 | 6 |

Notes:

- Retrieval-only: one real secretary entry per question, with an HTTP fake model capturing the actual supplied context. No answer comparison or extraction is run.
- Gold is independently generated from source templates; complete evidence text and persisted source/item identity must both be delivered.
- Recall and interference are micro-averages of evidence counts and annotated distractor/evidence pairs. Missing evidence ranks after delivered distractors.
- Memories/sources sent count M/S aliases in the actual memory and source sections, excluding query/history. Hard time-only paraphrases share the same eight-evidence weekly set.
- Fake tokens/cost/latency are local plumbing measurements. They are not real-model answer quality.
