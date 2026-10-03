# PCAS synthetic recall evaluation

Revision `bfa519b`; anchor 2026-10-03; model `synthetic-reflector`; fake=true; rendered corpus SHA256 `182262cf2e6d5cd302ff67fa480d98950ed25a80ec63e09e7979ace70eeda0cd`.

Native structured schema: false. Baseline 0.0000 (awaiting_coordinator).

Tier **hard**, mode retrieval, 2728 documents, 40/40 questions, gold=false, seed=20261002.

| Query type | Questions | Evidence | Recall | Inversions | Interference |
|---|---:|---:|---:|---:|---:|
| time-entity | 8 | 0/8 | 0.000000 | 21/512 | 0.041016 |
| time-only | 8 | 0/64 | 0.000000 | 16/32768 | 0.000488 |
| entity-only | 8 | 6/8 | 0.750000 | 0/512 | 0.000000 |
| wrong-time | 8 | 0/8 | 0.000000 | 0/512 | 0.000000 |
| ordinary | 8 | 8/8 | 1.000000 | 0/512 | 0.000000 |
| all | 40 | 14/96 | 0.145833 | 37/34816 | 0.001063 |


| Question | Evidence | Recall | Inversions | Interference | Memories sent | Sources sent |
|---|---:|---:|---:|---:|---:|---:|
| hard-time-entity-01 | 0/1 | 0.000000 | 6/64 | 0.093750 | 0 | 6 |
| hard-time-entity-02 | 0/1 | 0.000000 | 5/64 | 0.078125 | 0 | 6 |
| hard-time-entity-03 | 0/1 | 0.000000 | 4/64 | 0.062500 | 0 | 6 |
| hard-time-entity-04 | 0/1 | 0.000000 | 3/64 | 0.046875 | 0 | 6 |
| hard-time-entity-05 | 0/1 | 0.000000 | 2/64 | 0.031250 | 0 | 6 |
| hard-time-entity-06 | 0/1 | 0.000000 | 1/64 | 0.015625 | 0 | 6 |
| hard-time-entity-07 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-time-entity-08 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-time-only-01 | 0/8 | 0.000000 | 8/4096 | 0.001953 | 0 | 6 |
| hard-time-only-02 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-time-only-03 | 0/8 | 0.000000 | 8/4096 | 0.001953 | 0 | 6 |
| hard-time-only-04 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-time-only-05 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-time-only-06 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-time-only-07 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-time-only-08 | 0/8 | 0.000000 | 0/4096 | 0.000000 | 0 | 6 |
| hard-entity-only-01 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-entity-only-02 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-entity-only-03 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-entity-only-04 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-entity-only-05 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-entity-only-06 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-entity-only-07 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-entity-only-08 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-01 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-02 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-03 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-04 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-05 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-06 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-07 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-wrong-time-08 | 0/1 | 0.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-01 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-02 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-03 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-04 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-05 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-06 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-07 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |
| hard-ordinary-08 | 1/1 | 1.000000 | 0/64 | 0.000000 | 0 | 6 |

Notes:

- Retrieval-only: one real secretary entry per question, with an HTTP fake model capturing the actual supplied context. No answer comparison or extraction is run.
- Gold is independently generated from source templates; complete evidence text and persisted source/item identity must both be delivered.
- Recall and interference are micro-averages of evidence counts and annotated distractor/evidence pairs. Missing evidence ranks after delivered distractors.
- Memories/sources sent count M/S aliases in the actual memory and source sections, excluding query/history. Hard time-only paraphrases share the same eight-evidence weekly set.
- Fake tokens/cost/latency are local plumbing measurements. They are not real-model answer quality.
