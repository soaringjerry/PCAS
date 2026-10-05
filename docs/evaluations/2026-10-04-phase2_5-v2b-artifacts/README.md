# V2b 数字产物与重算

本目录属于 V2b，原 V2 数字目录完全不改。这里只保存逐行评分、编号、字符数、耗时、调用和来源指纹；不保存模型回答或检索上下文正文。实际快照为虚构数据，保留在执行主机的仓库外；数字清单保存其字节指纹、逐题字符、真实捕获耗时和本机调用。

2026-10-05 00:30:01 UTC 最后一份补测落盘。三方法各三次完整矩阵为 1512 行、4536 次保留调用，旧120/新48/合并168分别报告。新干扰十题所有对照仍满分，未达到区分目标；设计、中断、事后标准修订、时钟和实际成本见[同一评测记录第 6 节](../2026-10-04-phase2_5-v2-doing.md#6-v2b独立任务增补2026-10-04-起)。

## 产物

- `source-initial.json.gz`：实际中断的主跑；764 个完成行及 244 个失败/取消记录。
- `completion-run2.json.gz`：第二轮缺方法的 82 题三方法一起补跑，246 行，原两个未齐任务已有行废弃。
- `completion-run3.json.gz`：第三轮全 168 题三方法，504 行。
- `scope-repair.json.gz`：I-NOISE-07、I-NOISE-10 两项正向标准扩展后三方法/三遍全重跑，18 行。
- `source-before-gold.json.gz`：按缺失矩阵重建的三轮原标准结果；明确不是单一进程运行。
- `source.json.gz`：两题全部替换之后的最终 1512 行数字源；头部开始日期/修订沿用初始源，各补测实际日期和修订在来源记录中。
- `old120`、`new48`、`all168` 的 JSON.gz 和 Markdown：逐题双判、三次微平均、加分、输入/调用/耗时及波动范围；`*-categories.md` 为六类范围表，`overall-table.md` 为三题组总表。
- `noise-table.md`、`chain-table.md`、`disagreements-table.md`：逐条干扰、变更链及双判争议计数。
- `capture-manifest.json`、`noise-exposure.json`：不含正文的快照观测；全文存在性是精确字符串观测，不代表使用是否正确。
- `transport-provenance.json`、`gold-repair-provenance.json`、`ledger.json`：按完整性/标准差异重建，不按得分挑行；实际开销和未知在途上限单列。
- `discarded-group-audit.json.gz`、`discarded-ledger.json`：来源分组复核之前的早期 68 个完成行，全部废弃。

## 纯数字重算

不用读取上下文正文即可重算：

```bash
go run ./cmd/pcas-eval/doing-cohorts \
  -source=docs/evaluations/2026-10-04-phase2_5-v2b-artifacts/source.json.gz \
  -manifest=docs/evaluations/2026-10-04-phase2_5-v2b-artifacts/capture-manifest.json \
  -output=/var/tmp/v2b-recomputed
```

该入口检查原 120 题/671 条记忆的精确前缀、最终题集指纹、完整三方法/三遍矩阵、每行双判和保守计分、输出字数、上下文字符及输入字符。`context_json_chars` 是编码成 JSON 字符串后的字符数（包括两侧引号），用于在没有正文时验证转义后的输入长度。纯数字入口可以复核评分、字符数和来源一致性，不能重新验证语义或一次性检索的原始内容；后者需主机上保存的原虚构快照。原始源文件的传输补跑/标准替换证明由分别保存的 SHA 和记录提供；审计全部重建规则时，用下述完整重建入口及原快照。

捕获题集 SHA 为 `3b259cd267354b93ee285b7459feac33f1c2d71ef27ceb91c16c7411c6e38d90`，最终评分题集为 `f26bfdff04c5b4d32b2b5eb9d83a091586a873ad09acdb2ea5a60e868731d0fa`。合并器只允许两项指定正向标准文本变动，请求/记忆/证据完全相同后才允许复用快照。若持有执行主机原快照，可从原始源重建（旧标准题集从修订前提交读取到仓库外）：

```bash
git show c1fe86c:testdata/phase2_5/doing-independent/suite.json > /var/tmp/v2b-before-gold.json
go run ./cmd/pcas-eval/doing-cohorts \
  -source=docs/evaluations/2026-10-04-phase2_5-v2b-artifacts/source-initial.json.gz \
  -original-suite=/var/tmp/v2b-before-gold.json \
  -completion-run2=docs/evaluations/2026-10-04-phase2_5-v2b-artifacts/completion-run2.json.gz \
  -completion-run3=docs/evaluations/2026-10-04-phase2_5-v2b-artifacts/completion-run3.json.gz \
  -replacement=docs/evaluations/2026-10-04-phase2_5-v2b-artifacts/scope-repair.json.gz \
  -snapshot=/var/tmp/pcas-v2b-freeze.9HONUImQ/snapshot.json \
  -output=/var/tmp/v2b-rebuilt
```

原驱动数字源保留其 V2 通用注释；其中“120 题、实时 current、同日复测”需按本次 frozen-current 协议解释。派生报告注释已明确：真实检索只捕获一次，三轮重复固定输入的生成/判定，模型实际生成日期未冻结。旧 120 题在扩展的 801 条库上重测，不能当作原 671 条库的复现。

三次范围只是观测极差；比较方法取两者较大的重复性下限，以内不能凭这三次判断优劣，超过也不是显著性证据。新类别只有 4–12 题，粒度粗；回答和两个评分员同源，仍需独立人工复判。每次失败尝试、预检、废弃流程及未知在途界限不计入质量行，而在 ledger 另报。总尝试为 4828–4852，不能填写精确数。

## 指纹

下表为未压缩 JSON 字节的 SHA-256；gzip 使用固定时间戳 0。快照正文指纹及原/最终题集指纹另在捕获清单中。

| 文件 | 未压缩 JSON SHA-256 |
|---|---|
| `all168.json.gz` | `b7d221bc2ec4deba09908bfbfb7c969ac4fdf5797d70ffead53fcf97a62d089d` |
| `capture-manifest.json` | `91cb81288ad87c7c1f5ec472d5bab0228c30f5e926586b895f42e23ad616f479` |
| `completion-run2.json.gz` | `cc7e622959d80318c78bf4f502162006d6d27ffaa97fc2d4eaa7d26c65d1ab2d` |
| `completion-run3.json.gz` | `8534abea13d551d6ecd46a6baf94b5da1a3e2f10df01cf57db1a98e4b9f1f1ca` |
| `discarded-group-audit.json.gz` | `da1b01a3f758bcda90ec937d7343d2152c789a9f32a85c3130814470cfa88455` |
| `discarded-ledger.json` | `b310c50be4874e71e542b24b50937ca0ed853672195bdb3668a6401fce0584fc` |
| `gold-repair-provenance.json` | `89ed8b13c4c7db4561ec00f47c2cf635f202b6fb9db3479e0dce1292348f5013` |
| `ledger.json` | `1e2f6971538b3b71bc7e231c41942019cc5d994a1dd5ca02d60e33e7691c5f06` |
| `new48.json.gz` | `f2594e268fefa6d207764ae73653c16287579256a85ea3affe8d9138fcc81fae` |
| `noise-exposure.json` | `42124de10be23ae621445be9b5ebee46c40fcb5f187c48b9ac57ef6a032f56a6` |
| `old120.json.gz` | `9f5abf0faa2157ecbede19deb11786f532c816303307c605a999c1ac02ef153b` |
| `scope-repair.json.gz` | `7d9b3e9f4d37dc332772fcf8a47a6f9f47e82400c4cf4000964f0993bc0f48fc` |
| `source-before-gold.json.gz` | `0af9a9da5c699a5306ff5e2db8d36292918d85d6536811802a648722f4cb33f7` |
| `source-initial.json.gz` | `8152ed009c24b3a376ddc91049b1042fe720dfcae662838168a81aa9c600fed8` |
| `source.json.gz` | `b4b31abef4a97f7e4679f3d4b0f5def412b568afa1475ea6f7df4c495a548226` |
| `transport-provenance.json` | `e8143e892c58e1d0c3f8551719aae683cb3de90cb0e1f6ff6f460de3925182f6` |
