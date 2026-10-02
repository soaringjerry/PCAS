# 任务 S0：骨架（迁移、共用字段、接缝）

执行者 6.1 Sol。分支 `phase2/s0-skeleton`，**从 `origin/main` 建**；工作区 `/root/PCAS-wt/s0`；PR 的 base 是 **`main`**。

先读 [并行方案与数据约定](parallel.md) 全文。你做的是第 2–4 批共用的地基：十个执行者在等它，所以它要小、要快、要**不改变任何现有行为**。合入之后别人才开工。

## 要交付什么

### 1 迁移 027

`internal/postgres/migrations/027_structured_memory.sql`，内容按 parallel.md 第 3.1 节。

- 外键、主键的写法照 `001_memory.sql` 里已有表的样子（例如 `claim_revisions` 的主键是 `(owner_id, claim_id, version)`）。
- 只加不改：不动已有列的类型和约束，不删任何东西。
- 迁移在空库和有数据的库上都能跑，重复启动不再执行第二次。
- 写完自己核对一遍删除闭包：删一条陈述，它的 `claim_mentions` 跟着没；删一份资料，它的 `source_extractions` 跟着没；删一份归档，它的 `import_batches` 跟着没。靠外键级联做到，不要去改 `editing.go`。

第 3.1 节哪一行照着写不通（约束冲突、已有同名对象），不要自己换方案，告诉协调者。

### 2 共用字段

- `internal/workspace/model.go`：`Memory` 加 `ExpressedAt`、`EventFrom`、`EventTo`、`EventPrecision`、`Mentions`；新类型 `MemoryMention{EntityID, Name, Role}`；工作台快照加 `MemoryTotal`。JSON 字段名按 parallel.md 第 3.3 节。
- `internal/workspace/desk.go`：`DeskTimelineItem` 加 `EventFrom`、`EventTo`、`EventPrecision`、`Mentions`。
- `web/src/domain/` 里对应的 TypeScript 类型加上同名的可选字段。
- 这一步只加字段，不填值：现有接口的返回内容除了多出 `"mentions": []`、`"memoryTotal": <现有条数>` 之外不变。`memoryTotal` 先填当前返回的记忆条数。

### 3 两处纯搬移

- `secretaryCardsTx` 整个函数从 `desk_turn.go` 搬到新文件 `desk_cards.go`。
- `memoriesTx` 整个函数从 `claims.go` 搬到新文件 `memories_read.go`。

只搬，不改一个字符的逻辑；它们依赖的小函数留在原处。

### 4 用量记录的接缝

- 新文件 `internal/postgres/usage_log.go`：类型 `modelUsage`（字段和 `model_usage` 表一一对应：归属用户、用途、副手 id、模型名、输入输出 token、花费、对话轮 id、运行 id、后台任务 id、用到的记忆引用、查询条件）和函数 `recordUsageTx(ctx context.Context, tx pgx.Tx, usage modelUsage) error`，**函数体直接返回 nil**。
- 在四处拿到模型结果之后、提交结果的那个事务里各加一次调用，把手头已有的信息填进去：
  - 秘书一轮对话（`desk_turn.go`）：用途 `secretary`，带这一轮的依赖列表；
  - 旧的导办台问答（`desk.go` 的 `AnswerDesk`）：用途 `answer`；
  - 副手运行拿到结果时（`runs.go`）：用途 `deputy`，带 `run.ContextVersions`；
  - 抽取（`processing.go`）：用途 `extraction`，带后台任务 id。
- 调用的返回值照常处理（出错就让这个事务失败）。现在是空实现，所以行为不变。
- 哪一处没有现成的事务可用、或者拿不到 token 数，不要为此重构，记下来告诉协调者。

## 不做的事

- 不实现任何新行为：不写抽取、不写检索、不写界面。
- 不新增测试文件，不改已有测试的预期。已有测试必须一个不改地全绿，这是「行为不变」的证明。
- 不动白皮书和 `docs/`。

## 交付

PR 进 `main`（不是 Draft，CI 全绿后协调者审查合并）。说明里写：迁移里每个对象对应约定的哪一行、有没有偏离；四处调用点的文件和行号；两处搬移前后函数体逐字一致的验证方法（例如对函数体做 diff）；`make check` 结果；在一个带旧数据的库上跑迁移的结果。
