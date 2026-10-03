# 第 2 批补充验收：R23 秘书原话的对话上文

状态：**X19–X23 全部通过**。E3 合入 main 后按协调者通知完成一轮实际验收：5 条序列、8 个场景，0 失败、0 跳过、无重跑、无 DATA RACE。

## 基线与预期冻结

- 本轮已 `git fetch origin` 并变基到 `origin/main` 的 `8cb6700`（E3 PR #123 合入）。实际执行提交为 `4ac3ae599d2bd2bf121ef7a9c073d26f329ba5b2`，执行期间 HEAD 和测试文件保持不变；测试及冻结预期与变基前逐字核对未改。
- 最初准备时的独立工作区 `/root/PCAS-wt/b2-T2-E3`，分支 `phase2/b2-T2-conversation-context-acceptance` 从 `origin/main` 的 `275f97fa3b6d08714de2848dd640665b9da808a7` 建立。Draft PR [#125](https://github.com/soaringjerry/PCAS/pull/125) 的 base 为 `main`。
- 依据：[batch2 契约第 13 节 R23、X19–X23](../tasks/phase2/batch2/README.md#13-补充规则整理秘书对话时带上文2026-10-03任务-e3)，以及 [E3 任务包](../tasks/phase2/batch2/E3-conversation-context.md) 对来源类型、模型调用次数和禁止补写 `source_contexts` 的要求。
- 先提交 `7cc2dd8` 冻结 `testdata/phase2/b2-gold.json` 的新顶层项 `supplement_R23_275f97f`，再编写测试。原有 gold 的全部键和值逐项比对未改；第 1、2 批已有测试及断言未改。
- 准备测试时未读取 E3 实现分支。复用 main 上既有的秘书、摄入、删除、修改、补做及 worker 入口和假模型辅助函数。全部内容为合成资料。

## 序列与测试

测试文件：`internal/postgres/phase2_b2_conversation_context_test.go`。5 个顶层测试，共 8 个验收场景，沿用 `testStore` 的集成测试约定，可随普通 `make check` 运行。

| 序列 | 测试名（`TestPhase2B2_` 前缀） | 固定断言 | 执行结果 |
|---|---|---|---|
| X19 | `X19_SecretaryExtractionReceivesEarlierExchange` | `desk` 和 `desk-incomplete` 的实际抽取 HTTP 请求含第一轮用户问题、秘书回答，且标明角色；当前原话保持完整；成都、老王保存为记忆提及，证据只关联当前资料版本。先写入后来一轮再抽取，排除当前回答及未来轮次。另建 8 轮，只给最后 6 轮的 12 条消息，每条为前 1200 个 Unicode 字符（含中文及 emoji），多余头两轮和尾部不给。 | 通过 |
| X20 | `X20_ContextExcludesOtherConversationAndOwner` | 同用户另一段对话、不同用户同一对话 UUID 的内容均不给；同段上文仍完整提供。假模型故意返回杭州／钱叔、昆明／孙叔等越界提及，只有成都／老王可保存。 | 通过 |
| X21 | `X21_ContextOmitsClearedAndReplacesOutdatedAnswer` | 通过真实删除原话清空一轮，模型相邻消息中完全排除；通过真实修改记忆使早前回答标为依据已更新，用户问题照给，秘书回答为冻结替换语。正常轮的问答照给；界面历史中的旧回答仍保留。 | 通过 |
| X22 | `X22_BackfillReceivesOriginalConversationContext` | 把实际秘书来源设为相对今天 7 天前的旧资料、设置旧版抽取记录；第一句经抽取入口处理完，仅第二句待补做。真实 `BackfillExtractions` 排入 1 份，优先级 10、有 `backfill_queued_at`，实际 worker 抽取时仍带所属对话的前文；产物、提及和当前证据均核对。 | 通过 |
| X23 | `X23_NeighborQuoteCannotBecomeCurrentEvidence` | 分别用前文用户问题和秘书回答做非法 quote。仅非法项时记忆为 0、状态 empty；混入当前原话的合法项时只保存 1 条、状态 done，合法证据仍来自当前资料。非法 quote 所在上文确实送达模型。 | 通过 |

所有场景都检查目标抽取恰好发出 1 次实际模型 HTTP 调用，现有提示词的解指代、证据来源、原文指令和 assistant 提案限制仍在，`source_contexts` 没有新增行。对相邻消息按角色与正文的多重集合比较，契约没有规定数组排序。

`desk-incomplete` 子场景通过公开 `Ingest` 创建该类型资料，`external_id` 关联到真实 `DeskTurn` 的请求 id；没有伪造 `desk_turns` 或 `source_contexts`。它验证该类型的上文查找及抽取处理，不重复已有取消／恢复流程测试。

## 本轮实际执行（2026-10-03）

```sh
go test -race -count=1 -timeout 30m -json -run '^TestPhase2B2_X(19|20|21|22|23)_' ./internal/postgres
```

在上述固定提交上运行一次，退出码 0。各序列的实际耗时：X19 1.89 秒（desk 0.46、desk-incomplete 0.45、6 轮及 Unicode 边界 0.97），X20 0.45 秒，X21 0.66 秒，X22 0.42 秒，X23 0.74 秒（仅非法项 0.34、混合项 0.39）。包括编译等开销在内的命令总计 12.38 秒。没有失败发现。

独立临时 PostgreSQL 16 / pgvector 0.8.2、UTF8、tmpfs、随机本机端口，设置独立 `PCAS_TEST_DATABASE_URL`。每个测试独立 schema；只使用合成资料、本地假模型，不读取线上配置、不连接线上库、不调用真实模型、不发送通知。没有运行浏览器或完整 `make check`，本轮范围为协调者指定的 X19–X23。

证据目录：`/tmp/pcas-t2-r23-final-p8xzhdse`。`manifest.json` 记录命令与提交；`go-test.json` 保存完整实际请求检查与逐测试输出；`summary.json`、`exit.txt` 为结果；`cleanup.json` 确认仅本轮记录的自建容器已移除。

## 准备阶段检查（历史：合入前，仅编译）

- `make fmt-check lint build`：通过；仅格式检查、`go vet`、构建。
- `go test -race -c -o /tmp/pcas-t2-r23-postgres-7cc2dd8.test ./internal/postgres`：通过；**仅编译测试程序，没有运行测试**。
- `git diff --check`：通过；原有 gold 逐项不变核对通过。

准备阶段没有启动数据库、浏览器或 worker，没有运行 `make check`，没有连接线上服务或真实模型。本轮实际执行见上节。

原定执行范围和参数已按上述命令落实；冻结预期及全部验收断言未改。
