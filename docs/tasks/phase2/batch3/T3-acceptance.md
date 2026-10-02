# 任务 T3：第 3 批独立验收

执行者 6.1 Sol，**不能是做 Q1、Q2、K 的执行者**。分支 `phase2/b3-T3-acceptance`，从 `origin/phase2/batch3` 建（集成分支建好之前先从 `origin/main` 建，之后变基）；工作区 `/root/PCAS-wt/b3-T3`；Draft PR 的 base 是 `phase2/batch3`。

先读 [并行方案与数据约定](../parallel.md) 和 [第 3 批契约](README.md)。你只看契约写测试，不看实现的做法。

## 要交付什么

契约第 5 节的全部序列（P1–P7、S1–S13、K1–K10），每条至少一个测试，测试名带编号（`TestPhase2B3_…`）。再加浏览器用例 V1–V3。

写法沿用第 1 批验收（`phase2_b1_*_test.go` 里的辅助函数可以直接用，不要改）：

- **结构化数据你自己造。** 按数据约定直接往 `claim_mentions`、`claim_revisions` 的事件时间列、`record_versions.expressed_at`、实体和别名表里写。不依赖第 2 批的抽取，这样第 3 批可以独立验收。写一个造数据的辅助函数，别的测试都用它。
- **断言模型实际收到的内容。** 「排在最前」「收不到」都看假模型服务收到的请求正文里的顺序和有无。
- **P 序列的预期你独立算。** 每个时间说法的区间由你按日历算出来写进冻结文件，不调用被测函数去生成预期。
- **S6 和 S10 是对比测试。** S10 沿用第 1 批的做法，改动前后各跑一遍对比字节。S6 需要一个「没有任何条件」的基准：用同一批数据、一句不含时间和实体的话，断言给模型的内容和第 3 批之前的行为一致（可以在集成分支的基线提交上先跑出基准，存进冻结文件）。
- **时间。** 「去年」「上周」这类相对今天的数据用相对日期造；时区至少覆盖悉尼和上海。
- **冻结预期。** `testdata/phase2/b3-gold.json`，先提交再写测试，之后只增不改。

## 文件

`internal/postgres/phase2_b3_*_test.go`、`internal/memory/phase2_b3_plan_test.go`（P 序列）、`testdata/phase2/b3-gold.json`、`web/tests/phase2-batch3.spec.ts`、`web/tests/phase2-batch3-backend.spec.ts`、CI 里加新浏览器用例的那一行、`docs/evaluations/<日期>-phase2-batch3-acceptance.md`。

不改产品代码。不改已有测试的预期；协调者批准要改的会单独交给你。

## 纪律和报告

和 [T2](../batch2/T2-acceptance.md) 的「纪律」「验收报告」两节相同。
