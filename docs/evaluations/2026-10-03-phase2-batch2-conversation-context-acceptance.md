# 第 2 批补充验收：R23 秘书原话的对话上文

状态：测试已准备，**X19–X23 尚未执行**。按协调者要求，等 E3 合入 main 并收到通知后再跑，当前没有通过或失败结论。

## 基线与预期冻结

- 已 `git fetch origin`；独立工作区 `/root/PCAS-wt/b2-T2-E3`，分支 `phase2/b2-T2-conversation-context-acceptance` 从 `origin/main` 的 `275f97fa3b6d08714de2848dd640665b9da808a7` 建立。Draft PR 的 base 为 `main`。
- 依据：[batch2 契约第 13 节 R23、X19–X23](../tasks/phase2/batch2/README.md#13-补充规则整理秘书对话时带上文2026-10-03任务-e3)，以及 [E3 任务包](../tasks/phase2/batch2/E3-conversation-context.md) 对来源类型、模型调用次数和禁止补写 `source_contexts` 的要求。
- 先提交 `7cc2dd8` 冻结 `testdata/phase2/b2-gold.json` 的新顶层项 `supplement_R23_275f97f`，再编写测试。原有 gold 的全部键和值逐项比对未改；第 1、2 批已有测试及断言未改。
- 未读取 E3 实现分支。复用 main 上既有的秘书、摄入、删除、修改、补做及 worker 入口和假模型辅助函数。全部内容为合成资料。

## 序列与测试

测试文件：`internal/postgres/phase2_b2_conversation_context_test.go`。5 个顶层测试，共 8 个验收场景，沿用 `testStore` 的集成测试约定，可随普通 `make check` 运行。

| 序列 | 测试名（`TestPhase2B2_` 前缀） | 固定断言 | 执行结果 |
|---|---|---|---|
| X19 | `X19_SecretaryExtractionReceivesEarlierExchange` | `desk` 和 `desk-incomplete` 的实际抽取 HTTP 请求含第一轮用户问题、秘书回答，且标明角色；当前原话保持完整；成都、老王保存为记忆提及，证据只关联当前资料版本。先写入后来一轮再抽取，排除当前回答及未来轮次。另建 8 轮，只给最后 6 轮的 12 条消息，每条为前 1200 个 Unicode 字符（含中文及 emoji），多余头两轮和尾部不给。 | 待通知 |
| X20 | `X20_ContextExcludesOtherConversationAndOwner` | 同用户另一段对话、不同用户同一对话 UUID 的内容均不给；同段上文仍完整提供。假模型故意返回杭州／钱叔、昆明／孙叔等越界提及，只有成都／老王可保存。 | 待通知 |
| X21 | `X21_ContextOmitsClearedAndReplacesOutdatedAnswer` | 通过真实删除原话清空一轮，模型相邻消息中完全排除；通过真实修改记忆使早前回答标为依据已更新，用户问题照给，秘书回答为冻结替换语。正常轮的问答照给；界面历史中的旧回答仍保留。 | 待通知 |
| X22 | `X22_BackfillReceivesOriginalConversationContext` | 把实际秘书来源设为相对今天 7 天前的旧资料、设置旧版抽取记录；第一句经抽取入口处理完，仅第二句待补做。真实 `BackfillExtractions` 排入 1 份，优先级 10、有 `backfill_queued_at`，实际 worker 抽取时仍带所属对话的前文；产物、提及和当前证据均核对。 | 待通知 |
| X23 | `X23_NeighborQuoteCannotBecomeCurrentEvidence` | 分别用前文用户问题和秘书回答做非法 quote。仅非法项时记忆为 0、状态 empty；混入当前原话的合法项时只保存 1 条、状态 done，合法证据仍来自当前资料。非法 quote 所在上文确实送达模型。 | 待通知 |

所有场景都检查目标抽取恰好发出 1 次实际模型 HTTP 调用，现有提示词的解指代、证据来源、原文指令和 assistant 提案限制仍在，`source_contexts` 没有新增行。对相邻消息按角色与正文的多重集合比较，契约没有规定数组排序。

`desk-incomplete` 子场景通过公开 `Ingest` 创建该类型资料，`external_id` 关联到真实 `DeskTurn` 的请求 id；没有伪造 `desk_turns` 或 `source_contexts`。它验证该类型的上文查找及抽取处理，不重复已有取消／恢复流程测试。

## 准备阶段检查

- `make fmt-check lint build`：通过；仅格式检查、`go vet`、构建。
- `go test -race -c -o /tmp/pcas-t2-r23-postgres-7cc2dd8.test ./internal/postgres`：通过；**仅编译测试程序，没有运行测试**。
- `git diff --check`：通过；原有 gold 逐项不变核对通过。

本次没有启动数据库、浏览器或 worker，没有运行 `make check`，没有连接线上服务或真实模型。完整测试执行、失败发现及被测 E3 合入提交号，在收到协调者通知后的报告中补记。

运行补充序列的筛选表达式为 `^TestPhase2B2_X(19|20|21|22|23)_`；使用独立测试库、`-race -count=1 -timeout 30m`，不重跑碰运气，不改冻结预期。
