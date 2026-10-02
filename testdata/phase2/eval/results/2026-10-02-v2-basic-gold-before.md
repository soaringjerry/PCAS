# PCAS synthetic recall evaluation

Revision `bfa519b`; anchor 2026-10-03; model `synthetic-reflector`; fake=true; rendered corpus SHA256 `29e0d668fec2eddf6250018a5cceac83d220d1725407f7bfcdb15a3cd111ef9f`.

Native structured schema: true. Baseline 0.0000 (awaiting_coordinator).

Tier **basic**, mode retrieval, 120 documents, 36/36 questions, gold=true, seed=0.

| Query type | Questions | Evidence | Recall | Inversions | Interference |
|---|---:|---:|---:|---:|---:|
| time-entity | 0 | 0/0 | 0.000000 | 0/0 | 0.000000 |
| time-only | 0 | 0/0 | 0.000000 | 0/0 | 0.000000 |
| entity-only | 23 | 29/29 | 1.000000 | 4/60 | 0.066667 |
| wrong-time | 2 | 4/4 | 1.000000 | 1/8 | 0.125000 |
| ordinary | 11 | 13/13 | 1.000000 | 2/26 | 0.076923 |
| all | 36 | 46/46 | 1.000000 | 7/94 | 0.074468 |


| Question | Evidence | Recall | Inversions | Interference | Memories sent | Sources sent |
|---|---:|---:|---:|---:|---:|---:|
| nt-q1 | 1/1 | 1.000000 | 0/2 | 0.000000 | 5 | 6 |
| nt-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 5 | 6 |
| nt-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 13 | 6 |
| nc-q1 | 1/1 | 1.000000 | 0/2 | 0.000000 | 4 | 6 |
| nc-q2 | 2/2 | 1.000000 | 0/4 | 0.000000 | 5 | 6 |
| nc-q3 | 1/1 | 1.000000 | 0/1 | 0.000000 | 15 | 6 |
| ct-q1 | 2/2 | 1.000000 | 1/4 | 0.250000 | 14 | 6 |
| ct-q2 | 2/2 | 1.000000 | 1/4 | 0.250000 | 12 | 6 |
| ct-q3 | 1/1 | 1.000000 | 1/2 | 0.500000 | 4 | 5 |
| cc-q1 | 2/2 | 1.000000 | 1/4 | 0.250000 | 7 | 6 |
| cc-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 5 | 6 |
| cc-q3 | 2/2 | 1.000000 | 1/4 | 0.250000 | 7 | 6 |
| dl-q1 | 1/1 | 1.000000 | 0/3 | 0.000000 | 11 | 6 |
| dl-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 6 | 6 |
| dl-q3 | 1/1 | 1.000000 | 0/1 | 0.000000 | 5 | 6 |
| dr-q1 | 1/1 | 1.000000 | 0/3 | 0.000000 | 9 | 6 |
| dr-q2 | 2/2 | 1.000000 | 1/4 | 0.250000 | 5 | 6 |
| dr-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 6 | 6 |
| yp-q1 | 2/2 | 1.000000 | 1/4 | 0.250000 | 13 | 6 |
| yp-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 6 | 6 |
| yp-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 3 | 6 |
| yo-q1 | 2/2 | 1.000000 | 0/4 | 0.000000 | 4 | 6 |
| yo-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 15 | 6 |
| yo-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 2 | 6 |
| pm-q1 | 2/2 | 1.000000 | 0/4 | 0.000000 | 4 | 6 |
| pm-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 6 | 6 |
| pm-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 4 | 6 |
| pc-q1 | 2/2 | 1.000000 | 0/4 | 0.000000 | 8 | 6 |
| pc-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 5 | 6 |
| pc-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 2 | 3 |
| oc-q1 | 1/1 | 1.000000 | 0/2 | 0.000000 | 15 | 6 |
| oc-q2 | 1/1 | 1.000000 | 0/3 | 0.000000 | 11 | 6 |
| oc-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 5 | 6 |
| le-q1 | 1/1 | 1.000000 | 0/3 | 0.000000 | 8 | 6 |
| le-q2 | 1/1 | 1.000000 | 0/2 | 0.000000 | 15 | 6 |
| le-q3 | 1/1 | 1.000000 | 0/2 | 0.000000 | 15 | 6 |

Notes:

- Retrieval-only: one real secretary entry per question, with an HTTP fake model capturing the actual supplied context. No answer comparison or extraction is run.
- Gold is independently generated from source templates; complete evidence text and persisted source/item identity must both be delivered.
- Recall and interference are micro-averages of evidence counts and annotated distractor/evidence pairs. Missing evidence ranks after delivered distractors.
- Memories/sources sent count M/S aliases in the actual memory and source sections, excluding query/history. Hard time-only paraphrases share the same eight-evidence weekly set.
- Fake tokens/cost/latency are local plumbing measurements. They are not real-model answer quality.
