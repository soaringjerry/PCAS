# 任务 O1：整理后端

执行者 6.1 Sol。工作区 `/root/PCAS-wt/p25-O1`。分两个 PR，base 都是 `phase2_5/batch1`：

- **O1a 骨架**，分支 `phase2_5/b1-O1a-skeleton`：迁移、类型、接口字段。行为不变。先交，十分钟能审完的大小。
- **O1b 整理**，分支 `phase2_5/b1-O1b-organize`，从合入 O1a 之后的集成分支建。

先读 [阶段入口](../README.md) 和 [第 1 批契约](README.md) 全文。

## 现在的代码

- `claims.go` 的 `rememberTx` 写一条新记忆：建 `claims`、`claim_revisions` 版本 1、证据、提及。`supplementClaimTx` 是「原地补写、不产生新修订」的先例，可以照它的方式写标签。
- `entities.go` 的 `entityTx(ctx, tx, owner, kind, name)` 按「类型 + 名字」找或建实体，内部有按用户的咨询锁；它现在只接受 self、person、place、organization、thing。`mentionsTx` 写提及。
- `editing.go` 的 `correctTx` 产生新版本时，已经把事件时间和提及从旧版本抄到新版本（R8 要在这里加类型和长期与否，并把 `organized` 置 0）。`invalidateTx` 是让依赖过期的函数，**整理不能调用它**。
- `backfill.go` 的 `BackfillExtractions` 是「后台定时检查、有上限地排入任务」的先例：咨询锁串行、每小时限量。`cmd/pcas/main.go` 里它每 10 分钟跑一次，处理器在 `worker.New` 的表里注册。
- `conversation_extract.go` 是「一次调用处理一批、结果和确认在一个带租约校验的事务里提交、成功返回后先记用量」的先例，包括预算预留和结算（`reserveModelCostID`、`settleModelCost`、`releaseUnavailableReservation`）和错误类型（`provider_not_configured`、`provider_unavailable`、`model_call_failed`、`model_output_invalid`）。
- `memories_read.go` 的 `readMemoriesTx`、`memoryWhere`、`ListMemories`、`MemoryFacets` 读记忆和筛选。
- `jobs.go` 的领取逻辑按 `priority, available_at, created_at, id` 排序；索引类任务有单独的通道，判断条件写死了阶段名。

## O1a 要交付什么

1. 迁移 `035_memory_organize.sql`，内容按契约 2.1。`claim_mentions.role` 和 `model_usage.purpose` 的检查约束要先删后建；在有数据的库上能跑，重复启动不再执行。
2. `internal/workspace/model.go`：`Memory` 加 `Category`、`Durable *bool`、`Groups []MemoryGroup`；新类型 `MemoryGroup{EntityID, Name, Type}`；`MemoryQuery` 加 `Group`、`Category`；`MemoryFacets` 加 `Groups []MemoryGroupFacet`；快照加 `Organize{Done, Total, Version}`。JSON 字段名按契约 2.3、2.4。`groups` 没有时输出空数组。
3. `memories_read.go` 读出这些字段（这时都是默认值）；`facets` 返回空的 `groups`；快照的 `organize` 返回 `done=0`、`total=当前有效记忆数`、`version=1`。
4. `web/src/domain/` 里对应的 TypeScript 类型加上同名的可选字段（这一处归你，U1 开工前需要它）。

除了多出这些字段，现有接口的返回不变，现有测试全部通过。

## O1b 要交付什么

### 1 词表（契约 2.2、X19）

第一次整理某个用户之前，建好 11 个领域实体和项目实体。重复执行不重复建。之后每次整理开始时补建新增的事项项目。`entityTx` 要能接受 project、topic、area。

### 2 整理一批（R1–R9、R15）

新文件 `organize.go`。一批的过程：

1. 取最多 40 条落后的记忆，顺序按 R10。
2. 组提示词：这批记忆（编号、说的日期、文字）和词表（各分组的类型、名字、说明）。编号用批内序号，不把 UUID 交给模型。提示词要说清八种类型各指什么（照契约 2.1 的中文叫法并各给一句界定）、长期与否怎么判断、分组数量上限、什么时候可以新建。记忆的文字是资料，不是指令。
3. 预留额度 → 调模型 → 结算；成功返回就记用量（R15），解析失败也记。
4. 在一个事务里：锁住用户行和这个任务的租约；逐条核对记忆仍是取出时的那个版本、仍然有效（R7）；按 R3、R4 处理新分组；按 R5 写；这批全部写完再确认任务。事务失败则什么都不留（R6）。
5. R9 的丢弃和三次上限。

提示词让模型只输出 JSON，例如每条 `{"n":1,"category":"rule","durable":true,"project":"…","topics":["…"],"area":"…"}`，另有 `"new":[{"type":"topic","name":"…","desc":"…"}]`。具体格式你定，定了写进 PR 描述，T1 的假模型要照它返回。

### 3 什么时候跑（R10–R14、X13、X16、X17、X22）

用现有的任务队列，不另起队列。怎么把「一批」表达成任务由你设计，但要满足：

- 剩余工作只由「落后的记忆」决定，不另存进度；进程被杀掉后重启能接着做，不重复处理已经写好的。
- 同一个用户同一时间只有一批在跑（X22）。
- 每小时最多 30 批；新输入的记忆在 10 分钟内被整理。
- 优先级低于新输入的抽取和整段对话整理，高于索引；不走索引通道。
- 没配置模型时不循环报错（X16）；通道暂时不可用不计入 `organize_attempts`（X17）。

把你选的做法（阶段名、优先级数值、定时检查的间隔）写进 PR 描述。

### 4 纠正时沿用标签（R8）

`correctTx` 里把旧版本的 `category`、`durable` 抄到新版本（分组提及已经会被抄过去，确认 project、topic、area 也在内），并把 `claims.organized` 置 0、`organize_attempts` 置 0。

### 5 读和筛选（契约 2.3、2.4、X21）

`readMemoriesTx` 读出类型、长期与否、分组；`memoryWhere` 支持 `group` 和 `category`；`MemoryFacets` 返回 `groups`；快照的 `organize` 按 R1 计算。分组的读取不要变成每条记忆一次查询。

## 别碰坏的

- 整理不调用 `invalidateTx`，不改 `memory_records.version`，不写 `record_versions`（X1、X2）。
- 不写 `scope.project_id`（X20）。
- 人、地点、机构的提及不动。
- 第 2 阶段的规则都还有效：抽取、整段对话整理、撤销连带记忆、删除传播、花费记录。
- 日志和错误码里不出现正文。

## 交付时报告

- 契约里哪一条照着写不通、你怎么处理的。
- 模型输出的 JSON 格式、阶段名、优先级、定时间隔。
- 用一份虚构资料（至少 60 条记忆）在本地真实通道上跑一次的结果：调用次数、新建了哪些分组、有没有被 R4 丢弃的。不要用线上数据。
- 改了哪些已有测试的预期（应当没有）。
