# PCAS synthetic recall evaluation

Revision `3c49393`; anchor 2026-10-02; model `synthetic-reflector`; fake=true; corpus SHA256 `25afd706c84d85c132eb68246a133cfaa9c1abd1bfbc8a927a51a0d38d2c414e`.

Vector baseline: **cosine-vector**. Native structured schema: false. Baseline 0.0000 (awaiting_coordinator).

UTF-8 bytes <= input tokens allowance (conservative); provider usage used for measured token counts; input allowance 32000.

| Method | Fact hits | Fact rate | Input tokens | Cost CNY | Total ms | Truncated queries |
|---|---:|---:|---:|---:|---:|---:|
| all-context | 58/59 | 0.9831 | 124668 | 0.000000 | 107.66 | 36 |
| vector-only | 49/59 | 0.8305 | 43045 | 0.000000 | 60.72 | 5 |
| product | 59/59 | 1.0000 | 36644 | 0.000000 | 19387.76 | 0 |

Shared embedding precompute: 17319 input tokens, CNY 0.000000. Answer costs include query embeddings.

| Extraction dimension | Correct/total | Accuracy |
|---|---:|---:|
| People (micro-F1) | 0/30 | 0.0000 |
| Places (micro-F1) | 0/41 | 0.0000 |
| Time | 98/110 | 0.8909 |
| Nature | 31/110 | 0.2818 |

Extraction input tokens 26545; cost CNY 0.000000; total ms 22.08.

| Question | Method | Fact rate | Input tokens | Cost CNY | ms | Truncated |
|---|---|---:|---:|---:|---:|---|
| nt-q1 | product | 1.0000 | 983 | 0.000000 | 632.69 | false |
| nt-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.02 | true |
| nt-q1 | vector-only | 1.0000 | 669 | 0.000000 | 0.94 | false |
| nt-q2 | product | 1.0000 | 978 | 0.000000 | 562.06 | false |
| nt-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.30 | true |
| nt-q2 | vector-only | 1.0000 | 666 | 0.000000 | 0.67 | false |
| nt-q3 | product | 1.0000 | 973 | 0.000000 | 525.23 | false |
| nt-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.64 | true |
| nt-q3 | vector-only | 1.0000 | 672 | 0.000000 | 0.65 | false |
| nc-q1 | product | 1.0000 | 990 | 0.000000 | 580.27 | false |
| nc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 7.21 | true |
| nc-q1 | vector-only | 1.0000 | 673 | 0.000000 | 1.01 | false |
| nc-q2 | product | 1.0000 | 989 | 0.000000 | 604.17 | false |
| nc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 5.21 | true |
| nc-q2 | vector-only | 1.0000 | 2977 | 0.000000 | 2.63 | true |
| nc-q3 | product | 1.0000 | 984 | 0.000000 | 562.62 | false |
| nc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 3.21 | true |
| nc-q3 | vector-only | 1.0000 | 666 | 0.000000 | 0.95 | false |
| ct-q1 | product | 1.0000 | 982 | 0.000000 | 521.77 | false |
| ct-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.32 | true |
| ct-q1 | vector-only | 1.0000 | 670 | 0.000000 | 1.36 | false |
| ct-q2 | product | 1.0000 | 973 | 0.000000 | 524.47 | false |
| ct-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.82 | true |
| ct-q2 | vector-only | 1.0000 | 672 | 0.000000 | 1.35 | false |
| ct-q3 | product | 1.0000 | 970 | 0.000000 | 583.36 | false |
| ct-q3 | all-context | 1.0000 | 3463 | 0.000000 | 3.52 | true |
| ct-q3 | vector-only | 0.0000 | 666 | 0.000000 | 7.72 | false |
| cc-q1 | product | 1.0000 | 979 | 0.000000 | 550.41 | false |
| cc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.66 | true |
| cc-q1 | vector-only | 1.0000 | 671 | 0.000000 | 0.79 | false |
| cc-q2 | product | 1.0000 | 983 | 0.000000 | 549.03 | false |
| cc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.91 | true |
| cc-q2 | vector-only | 1.0000 | 667 | 0.000000 | 0.49 | false |
| cc-q3 | product | 1.0000 | 977 | 0.000000 | 500.25 | false |
| cc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 4.50 | true |
| cc-q3 | vector-only | 0.0000 | 672 | 0.000000 | 1.34 | false |
| dl-q1 | product | 1.0000 | 978 | 0.000000 | 589.32 | false |
| dl-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.30 | true |
| dl-q1 | vector-only | 0.5000 | 667 | 0.000000 | 0.67 | false |
| dl-q2 | product | 1.0000 | 992 | 0.000000 | 496.52 | false |
| dl-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.82 | true |
| dl-q2 | vector-only | 1.0000 | 674 | 0.000000 | 0.58 | false |
| dl-q3 | product | 1.0000 | 973 | 0.000000 | 516.72 | false |
| dl-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.51 | true |
| dl-q3 | vector-only | 1.0000 | 666 | 0.000000 | 0.59 | false |
| dr-q1 | product | 1.0000 | 985 | 0.000000 | 460.89 | false |
| dr-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.26 | true |
| dr-q1 | vector-only | 1.0000 | 672 | 0.000000 | 0.92 | false |
| dr-q2 | product | 1.0000 | 982 | 0.000000 | 515.98 | false |
| dr-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.73 | true |
| dr-q2 | vector-only | 1.0000 | 668 | 0.000000 | 0.59 | false |
| dr-q3 | product | 1.0000 | 987 | 0.000000 | 522.47 | false |
| dr-q3 | all-context | 1.0000 | 3463 | 0.000000 | 3.07 | true |
| dr-q3 | vector-only | 1.0000 | 2970 | 0.000000 | 4.57 | true |
| yp-q1 | product | 1.0000 | 996 | 0.000000 | 551.52 | false |
| yp-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.05 | true |
| yp-q1 | vector-only | 1.0000 | 672 | 0.000000 | 0.74 | false |
| yp-q2 | product | 1.0000 | 995 | 0.000000 | 523.20 | false |
| yp-q2 | all-context | 1.0000 | 3463 | 0.000000 | 5.70 | true |
| yp-q2 | vector-only | 0.0000 | 669 | 0.000000 | 3.80 | false |
| yp-q3 | product | 1.0000 | 985 | 0.000000 | 483.39 | false |
| yp-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.17 | true |
| yp-q3 | vector-only | 1.0000 | 670 | 0.000000 | 0.72 | false |
| yo-q1 | product | 1.0000 | 993 | 0.000000 | 520.67 | false |
| yo-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.13 | true |
| yo-q1 | vector-only | 1.0000 | 2971 | 0.000000 | 2.95 | true |
| yo-q2 | product | 1.0000 | 1012 | 0.000000 | 525.49 | false |
| yo-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.75 | true |
| yo-q2 | vector-only | 0.0000 | 673 | 0.000000 | 0.67 | false |
| yo-q3 | product | 1.0000 | 980 | 0.000000 | 536.89 | false |
| yo-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.01 | true |
| yo-q3 | vector-only | 0.0000 | 668 | 0.000000 | 1.37 | false |
| pm-q1 | product | 1.0000 | 976 | 0.000000 | 608.07 | false |
| pm-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.08 | true |
| pm-q1 | vector-only | 1.0000 | 669 | 0.000000 | 0.78 | false |
| pm-q2 | product | 1.0000 | 972 | 0.000000 | 558.33 | false |
| pm-q2 | all-context | 1.0000 | 3463 | 0.000000 | 1.80 | true |
| pm-q2 | vector-only | 1.0000 | 672 | 0.000000 | 0.59 | false |
| pm-q3 | product | 1.0000 | 988 | 0.000000 | 514.66 | false |
| pm-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.13 | true |
| pm-q3 | vector-only | 1.0000 | 2954 | 0.000000 | 3.81 | true |
| pc-q1 | product | 1.0000 | 985 | 0.000000 | 533.66 | false |
| pc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.31 | true |
| pc-q1 | vector-only | 0.0000 | 682 | 0.000000 | 0.94 | false |
| pc-q2 | product | 1.0000 | 971 | 0.000000 | 528.64 | false |
| pc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.22 | true |
| pc-q2 | vector-only | 1.0000 | 2155 | 0.000000 | 1.77 | false |
| pc-q3 | product | 1.0000 | 1115 | 0.000000 | 502.05 | false |
| pc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.45 | true |
| pc-q3 | vector-only | 1.0000 | 2152 | 0.000000 | 1.61 | false |
| oc-q1 | product | 1.0000 | 1012 | 0.000000 | 548.71 | false |
| oc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.20 | true |
| oc-q1 | vector-only | 1.0000 | 2158 | 0.000000 | 2.75 | false |
| oc-q2 | product | 1.0000 | 992 | 0.000000 | 482.79 | false |
| oc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.26 | true |
| oc-q2 | vector-only | 1.0000 | 2160 | 0.000000 | 3.13 | false |
| oc-q3 | product | 1.0000 | 1131 | 0.000000 | 592.29 | false |
| oc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.25 | true |
| oc-q3 | vector-only | 1.0000 | 2155 | 0.000000 | 3.14 | false |
| le-q1 | product | 1.0000 | 1284 | 0.000000 | 536.43 | false |
| le-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.44 | true |
| le-q1 | vector-only | 0.5000 | 672 | 0.000000 | 0.64 | false |
| le-q2 | product | 1.0000 | 1298 | 0.000000 | 544.02 | false |
| le-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.03 | true |
| le-q2 | vector-only | 1.0000 | 670 | 0.000000 | 0.61 | false |
| le-q3 | product | 1.0000 | 1301 | 0.000000 | 498.74 | false |
| le-q3 | all-context | 0.5000 | 3463 | 0.000000 | 2.67 | true |
| le-q3 | vector-only | 1.0000 | 2965 | 0.000000 | 2.90 | true |

Notes:

- All answer methods use the same model and captured secretary system/framing. All gold facts are scoring-only.
- Cost is CNY from explicit per-million token prices; answer query embedding costs are included; shared corpus embedding costs are reported separately.
- Extraction uses the tool's R1 schema prompt, not the product extractor prompt; measures model extraction against frozen source gold.
- People/places use set micro-F1; time/nature use quote-aligned item accuracy, charging extra/missing items.
- String/alias answer matching does not check negation or reasoning; repeated-context fake results only test plumbing.
- Fake input/output tokens are synthetic rune/4 counts; fake cost is zero and latency is local.
