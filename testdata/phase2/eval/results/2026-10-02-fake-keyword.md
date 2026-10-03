# PCAS synthetic recall evaluation

Revision `3c49393`; anchor 2026-10-02; model `synthetic-reflector`; fake=true; corpus SHA256 `25afd706c84d85c132eb68246a133cfaa9c1abd1bfbc8a927a51a0d38d2c414e`.

Vector baseline: **keyword-fallback**. Native structured schema: false. Baseline 0.0000 (awaiting_coordinator).

UTF-8 bytes <= input tokens allowance (conservative); provider usage used for measured token counts; input allowance 32000.

| Method | Fact hits | Fact rate | Input tokens | Cost CNY | Total ms | Truncated queries |
|---|---:|---:|---:|---:|---:|---:|
| all-context | 58/59 | 0.9831 | 124668 | 0.000000 | 113.22 | 36 |
| vector-only | 59/59 | 1.0000 | 30936 | 0.000000 | 166.92 | 3 |
| product | 59/59 | 1.0000 | 30790 | 0.000000 | 16236.25 | 0 |

Shared embedding precompute: 0 input tokens, CNY 0.000000. Answer costs include query embeddings.

| Extraction dimension | Correct/total | Accuracy |
|---|---:|---:|
| People (micro-F1) | 0/30 | 0.0000 |
| Places (micro-F1) | 0/41 | 0.0000 |
| Time | 98/110 | 0.8909 |
| Nature | 31/110 | 0.2818 |

Extraction input tokens 26545; cost CNY 0.000000; total ms 26.09.

| Question | Method | Fact rate | Input tokens | Cost CNY | ms | Truncated |
|---|---|---:|---:|---:|---:|---|
| nt-q1 | product | 1.0000 | 767 | 0.000000 | 550.60 | false |
| nt-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.38 | true |
| nt-q1 | vector-only | 1.0000 | 668 | 0.000000 | 4.42 | false |
| nt-q2 | product | 1.0000 | 766 | 0.000000 | 433.29 | false |
| nt-q2 | all-context | 1.0000 | 3463 | 0.000000 | 5.15 | true |
| nt-q2 | vector-only | 1.0000 | 667 | 0.000000 | 5.03 | false |
| nt-q3 | product | 1.0000 | 931 | 0.000000 | 529.46 | false |
| nt-q3 | all-context | 1.0000 | 3463 | 0.000000 | 3.35 | true |
| nt-q3 | vector-only | 1.0000 | 668 | 0.000000 | 3.26 | false |
| nc-q1 | product | 1.0000 | 748 | 0.000000 | 396.09 | false |
| nc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 4.27 | true |
| nc-q1 | vector-only | 1.0000 | 670 | 0.000000 | 3.56 | false |
| nc-q2 | product | 1.0000 | 772 | 0.000000 | 419.92 | false |
| nc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.04 | true |
| nc-q2 | vector-only | 1.0000 | 671 | 0.000000 | 4.06 | false |
| nc-q3 | product | 1.0000 | 984 | 0.000000 | 432.58 | false |
| nc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.23 | true |
| nc-q3 | vector-only | 1.0000 | 673 | 0.000000 | 3.61 | false |
| ct-q1 | product | 1.0000 | 960 | 0.000000 | 439.65 | false |
| ct-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.20 | true |
| ct-q1 | vector-only | 1.0000 | 674 | 0.000000 | 3.99 | false |
| ct-q2 | product | 1.0000 | 906 | 0.000000 | 414.45 | false |
| ct-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.37 | true |
| ct-q2 | vector-only | 1.0000 | 667 | 0.000000 | 4.08 | false |
| ct-q3 | product | 1.0000 | 730 | 0.000000 | 428.15 | false |
| ct-q3 | all-context | 1.0000 | 3463 | 0.000000 | 3.18 | true |
| ct-q3 | vector-only | 1.0000 | 670 | 0.000000 | 3.72 | false |
| cc-q1 | product | 1.0000 | 804 | 0.000000 | 394.21 | false |
| cc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.31 | true |
| cc-q1 | vector-only | 1.0000 | 666 | 0.000000 | 3.41 | false |
| cc-q2 | product | 1.0000 | 761 | 0.000000 | 391.70 | false |
| cc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.29 | true |
| cc-q2 | vector-only | 1.0000 | 666 | 0.000000 | 4.97 | false |
| cc-q3 | product | 1.0000 | 801 | 0.000000 | 421.12 | false |
| cc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.83 | true |
| cc-q3 | vector-only | 1.0000 | 666 | 0.000000 | 4.33 | false |
| dl-q1 | product | 1.0000 | 892 | 0.000000 | 458.15 | false |
| dl-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.93 | true |
| dl-q1 | vector-only | 1.0000 | 665 | 0.000000 | 4.59 | false |
| dl-q2 | product | 1.0000 | 785 | 0.000000 | 450.01 | false |
| dl-q2 | all-context | 1.0000 | 3463 | 0.000000 | 4.22 | true |
| dl-q2 | vector-only | 1.0000 | 665 | 0.000000 | 6.10 | false |
| dl-q3 | product | 1.0000 | 762 | 0.000000 | 420.16 | false |
| dl-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.81 | true |
| dl-q3 | vector-only | 1.0000 | 666 | 0.000000 | 2.87 | false |
| dr-q1 | product | 1.0000 | 859 | 0.000000 | 432.98 | false |
| dr-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.40 | true |
| dr-q1 | vector-only | 1.0000 | 668 | 0.000000 | 6.35 | false |
| dr-q2 | product | 1.0000 | 766 | 0.000000 | 397.33 | false |
| dr-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.00 | true |
| dr-q2 | vector-only | 1.0000 | 668 | 0.000000 | 3.78 | false |
| dr-q3 | product | 1.0000 | 787 | 0.000000 | 465.83 | false |
| dr-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.86 | true |
| dr-q3 | vector-only | 1.0000 | 669 | 0.000000 | 3.07 | false |
| yp-q1 | product | 1.0000 | 952 | 0.000000 | 451.99 | false |
| yp-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.61 | true |
| yp-q1 | vector-only | 1.0000 | 675 | 0.000000 | 3.42 | false |
| yp-q2 | product | 1.0000 | 793 | 0.000000 | 444.34 | false |
| yp-q2 | all-context | 1.0000 | 3463 | 0.000000 | 5.16 | true |
| yp-q2 | vector-only | 1.0000 | 675 | 0.000000 | 3.74 | false |
| yp-q3 | product | 1.0000 | 724 | 0.000000 | 394.89 | false |
| yp-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.55 | true |
| yp-q3 | vector-only | 1.0000 | 669 | 0.000000 | 3.28 | false |
| yo-q1 | product | 1.0000 | 752 | 0.000000 | 386.53 | false |
| yo-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.63 | true |
| yo-q1 | vector-only | 1.0000 | 670 | 0.000000 | 3.86 | false |
| yo-q2 | product | 1.0000 | 1013 | 0.000000 | 505.94 | false |
| yo-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.08 | true |
| yo-q2 | vector-only | 1.0000 | 675 | 0.000000 | 6.19 | false |
| yo-q3 | product | 1.0000 | 698 | 0.000000 | 453.69 | false |
| yo-q3 | all-context | 1.0000 | 3463 | 0.000000 | 3.14 | true |
| yo-q3 | vector-only | 1.0000 | 667 | 0.000000 | 2.99 | false |
| pm-q1 | product | 1.0000 | 741 | 0.000000 | 409.21 | false |
| pm-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.70 | true |
| pm-q1 | vector-only | 1.0000 | 668 | 0.000000 | 3.90 | false |
| pm-q2 | product | 1.0000 | 776 | 0.000000 | 459.06 | false |
| pm-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.55 | true |
| pm-q2 | vector-only | 1.0000 | 662 | 0.000000 | 4.52 | false |
| pm-q3 | product | 1.0000 | 740 | 0.000000 | 473.23 | false |
| pm-q3 | all-context | 1.0000 | 3463 | 0.000000 | 1.99 | true |
| pm-q3 | vector-only | 1.0000 | 667 | 0.000000 | 5.02 | false |
| pc-q1 | product | 1.0000 | 824 | 0.000000 | 480.43 | false |
| pc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 2.48 | true |
| pc-q1 | vector-only | 1.0000 | 667 | 0.000000 | 4.16 | false |
| pc-q2 | product | 1.0000 | 762 | 0.000000 | 485.71 | false |
| pc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 3.50 | true |
| pc-q2 | vector-only | 1.0000 | 665 | 0.000000 | 5.16 | false |
| pc-q3 | product | 1.0000 | 661 | 0.000000 | 467.42 | false |
| pc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 7.20 | true |
| pc-q3 | vector-only | 1.0000 | 670 | 0.000000 | 3.01 | false |
| oc-q1 | product | 1.0000 | 1015 | 0.000000 | 615.13 | false |
| oc-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.63 | true |
| oc-q1 | vector-only | 1.0000 | 678 | 0.000000 | 5.15 | false |
| oc-q2 | product | 1.0000 | 902 | 0.000000 | 429.75 | false |
| oc-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.08 | true |
| oc-q2 | vector-only | 1.0000 | 671 | 0.000000 | 16.41 | false |
| oc-q3 | product | 1.0000 | 918 | 0.000000 | 432.84 | false |
| oc-q3 | all-context | 1.0000 | 3463 | 0.000000 | 2.49 | true |
| oc-q3 | vector-only | 1.0000 | 672 | 0.000000 | 3.53 | false |
| le-q1 | product | 1.0000 | 1136 | 0.000000 | 502.12 | false |
| le-q1 | all-context | 1.0000 | 3463 | 0.000000 | 3.27 | true |
| le-q1 | vector-only | 1.0000 | 2955 | 0.000000 | 8.09 | true |
| le-q2 | product | 1.0000 | 1301 | 0.000000 | 459.43 | false |
| le-q2 | all-context | 1.0000 | 3463 | 0.000000 | 2.52 | true |
| le-q2 | vector-only | 1.0000 | 2949 | 0.000000 | 4.24 | true |
| le-q3 | product | 1.0000 | 1301 | 0.000000 | 508.83 | false |
| le-q3 | all-context | 0.5000 | 3463 | 0.000000 | 1.80 | true |
| le-q3 | vector-only | 1.0000 | 2954 | 0.000000 | 5.05 | true |

Notes:

- All answer methods use the same model and captured secretary system/framing. All gold facts are scoring-only.
- Cost is CNY from explicit per-million token prices; answer query embedding costs are included; shared corpus embedding costs are reported separately.
- Extraction uses the tool's R1 schema prompt, not the product extractor prompt; measures model extraction against frozen source gold.
- People/places use set micro-F1; time/nature use quote-aligned item accuracy, charging extra/missing items.
- String/alias answer matching does not check negation or reasoning; repeated-context fake results only test plumbing.
- Fake input/output tokens are synthetic rune/4 counts; fake cost is zero and latency is local.
- No embedding model configured: vector-only is explicitly replaced with keyword overlap ranking.
