# 第 2–4 批：并行方案与数据约定

2026-10-02。用户要求第 2、3、4 批并行开发，尽快做完第二阶段。本页是三批共用的约定：谁先谁后、文件怎么分、数据长什么样、大家共同遵守什么。**所有执行者先读完本页，再读自己那一批的契约和自己的任务包。** 本页和任务包冲突时以本页为准，并立刻告诉协调者。

各批的规则和操作序列在各自的契约里：[第 2 批](batch2/README.md) · [第 3 批](batch3/README.md) · [第 4 批](batch4/README.md)。

## 1 怎么并行

开发并行，上线不并行。三批同时写；上线仍按 2、3、4 的顺序，每一批在线上验证过再上下一批。

并行靠两件事成立：

- **数据约定先定死**（第 3 节）。第 3 批查询要用的「人、地点、时间」，不等第 2 批的抽取跑出来，测试里直接按约定的表结构造数据。
- **一个文件只有一个写入者**（第 4 节）。三批改的文件互不相交；必须共用的接缝由骨架任务 S0 先开好。

分两波：

| 波次 | 任务 | 条件 |
|---|---|---|
| 第 0 波 | **S0 骨架**（进 main，行为不变）；同时可以开工的：Q1（纯函数）、V（评测集）、T2、T3、T4（先按契约写测试） | 立刻 |
| 第 1 波 | E1、E2、M、U2（第 2 批）；Q2、K、U3（第 3 批）；I、L、U4（第 4 批） | S0 合进 main 之后 |

集成分支各一条，都从 S0 合入后的 main 建：`phase2/batch2`、`phase2/batch3`、`phase2/batch4`。

第 3、4 批有几处要用到第 2 批的成果（记忆对象上的时间和地点由 M 填；抽取任务的优先级和暂停由 E2 管）。所以**协调者会把第 2 批的集成分支陆续合进第 3、4 批的集成分支**，合了之后通知那两批的执行者变基。在那之前，第 3、4 批里依赖这些成果的断言暂时不过是正常的，各任务包里写明了是哪些。

## 2 任务与执行者

| 任务 | 内容 | 执行者 | 批 |
|---|---|---|---|
| [S0](S0-skeleton.md) | 迁移 027、共用类型字段、两处纯搬移、用量记录的调用点 | 6.1 Sol | 共用 |
| [E1](batch2/E1-extraction.md) | 抽取出人、地点、时间、性质并写进记忆 | 6.1 Sol | 2 |
| [E2](batch2/E2-backfill.md) | 旧资料补做、后台队列的优先级和重试 | 6.1 Sol | 2 |
| [M](batch2/M-memory-store.md) | 记忆列表分页与筛选、撤销只标记、后启用的副手补开、删除收尾 | 6.1 Sol | 2 |
| [U2](batch2/U2-frontend.md) | 记忆卡片上的人、地点、时间；资料库按人按地点翻 | Opus 5.5 | 2 |
| [T2](batch2/T2-acceptance.md) | 第 2 批独立验收 | 6.1 Sol（不做本批实现的人） | 2 |
| [Q1](batch3/Q1-planner.md) | 把一句问话拆成条件（纯函数） | 6.1 Sol | 3 |
| [Q2](batch3/Q2-recall.md) | 按条件找记忆，再用原话和向量补漏；接进秘书和副手 | 6.1 Sol | 3 |
| [K](batch3/K-timeline.md) | 时间轴卡片：当时说的、后来怎样了 | 6.1 Sol | 3 |
| [U3](batch3/U3-frontend.md) | 时间轴卡片的界面 | Opus 5.5 | 3 |
| [T3](batch3/T3-acceptance.md) | 第 3 批独立验收 | 6.1 Sol（不做本批实现的人） | 3 |
| [I](batch4/I-import.md) | ChatGPT 历史导入：预览、进度、暂停 | 6.1 Sol | 4 |
| [L](batch4/L-usage.md) | 记下每次调用用了哪些记忆、花了多少 | 6.1 Sol | 4 |
| [U4](batch4/U4-frontend.md) | 导入界面 | Opus 5.5 | 4 |
| [V](batch4/V-eval.md) | 带干扰项的回忆评测集和三种做法的对比 | 6.1 Sol（不做任何实现的人） | 4 |
| [T4](batch4/T4-acceptance.md) | 第 4 批独立验收 | 6.1 Sol（不做本批实现的人） | 4 |

**人手安排（2026-10-02 调整）。** 任务还是这 16 个，功能一个不少；只是同一批里的后端任务交给同一个执行者按顺序做，每个任务仍然单独一个分支、单独一个 PR：

| 执行者 | 依次做 |
|---|---|
| 第 2 批后端（6.1 Sol） | E1 → E2 → M |
| 第 3 批后端（6.1 Sol） | K → Q2（Q1 已由另一位执行者交付） |
| 第 4 批后端（6.1 Sol） | L → I |
| 界面（Opus 5.5） | U2 → U3 → U4 |
| 独立验收（6.1 Sol，各一位） | T2、T3、T4、V，不变 |

原因：瓶颈不在写代码的人数，而在协调者审代码和用户转话。同一批的几个后端任务是一件事的几个部分，一个人顺着做，任务之间的接缝不用靠传话对齐。做完一个任务就交一个 PR，不要攒到最后一起交；前一个 PR 还在审时可以开始下一个，下一个的分支从集成分支建，需要用到前一个的改动时从自己前一个分支建并在 PR 里写明。

文件归属表（第 4 节）不变：它现在的意思是「这个任务的 PR 只能动这些文件」。

Astra 留作后备：某个任务两轮没收敛时换它接手。

## 3 数据约定

这一节的表、字段名、JSON 字段名是冻结的。要改先找协调者，不要各自改一份。

### 3.1 迁移 027（S0 写，别人不新增迁移）

第 2–4 批的主要迁移是 `027_structured_memory.sql`。别的任务确实需要再加表或列时，告诉协调者，由协调者分配编号。已分配：**028**（`028_backfill_queue.sql`，任务 E2）：`memory_jobs` 加一列 `backfill_queued_at timestamptz`，记录这个任务被补做检查排入的时间；只有补做检查排入的任务才有值。下一个可用的编号是 029。

| 对象 | 内容 | 用途 |
|---|---|---|
| `claim_revisions` 加三列 | `event_from timestamptz`、`event_to timestamptz`、`event_precision text`（`unknown`/`day`/`month`/`year`/`range`，默认 `unknown`）；约束 `event_from <= event_to` | 这条记忆说的事发生在什么时候。**不要**用 `record_versions.valid_from/valid_to`：那一对决定记忆现在是否适用，写进去会让下周的计划今天查不到 |
| 表 `claim_mentions` | `owner_id, claim_id, claim_version, entity_id, role`；`role` 是 `person`/`place`/`organization`/`thing`；主键是这五列；随陈述版本和实体级联删除；索引 `(owner_id, entity_id, role)` | 一条记忆提到了谁、哪里 |
| `record_versions` 加索引 | `(owner_id, expressed_at)`，只索引非空 | 按「什么时候说的」查 |
| `claim_revisions` 加索引 | `(owner_id, event_from)`，只索引非空 | 按「事情发生在什么时候」查 |
| 表 `source_extractions` | `owner_id, source_id, source_version, extractor integer, state`（`done`/`empty`/`failed`）`, items integer, updated_at`；主键前三列；随资料版本级联删除 | 每份资料有没有被新版抽取处理过。`empty` 是处理过但没抽出东西 |
| `memory_jobs` 加一列 | `priority smallint NOT NULL DEFAULT 0` | 0 是正常，10 是补做旧资料。数字小的先做 |
| 表 `model_usage` | `owner_id, id, at, purpose`（`secretary`/`deputy`/`answer`/`extraction`/`embedding`）`, agent_id, model, input_tokens, output_tokens, cost, turn_id, run_id, job_id, memory_refs jsonb, plan jsonb`；索引 `(owner_id, at)` | 每次模型调用的记录。`memory_refs` 只放 `{id,version,kind}`，**不放正文** |
| 表 `import_batches` | `owner_id, id, archive_id, name, state`（`importing`/`paused`/`done`/`failed`）`, total, stored, left_out, earliest, latest, error_code, created_at, updated_at`；随归档资料级联删除 | 一次历史导入的进度。`stored` 是已经存成原话的条数，`left_out` 是因为超过条数上限没有导入的条数 |

### 3.2 实体

实体已有表 `entities`、`entity_versions`、`aliases`。这一阶段约定 `entity_type` 用这几个值：`self`（用户本人，每个用户恰好一个）、`person`、`place`、`organization`、`thing`，以及旧数据里的 `unknown`。

**同一个对象的判断**：同一种类型里，名字去掉首尾空白、不分大小写后和某个已有实体的任一别名**完全相同**，就是同一个；否则新建。不做模糊合并，不让模型判断两个名字是不是一个人。两个不同的「老王」会被当成一个，这是已知的限制，合并和拆分的界面留给观测台。

### 3.3 给界面的记忆对象

`workspace.Memory`（接口里的一条记忆）增加这些字段，旧字段不变：

```json
{
  "expressedAt": "2026-09-12T03:10:00Z",
  "eventFrom": "2026-09-14T00:00:00+10:00", "eventTo": "2026-09-21T00:00:00+10:00", "eventPrecision": "range",
  "mentions": [ { "entityId": "…", "name": "成都", "role": "place" }, { "entityId": "…", "name": "老王", "role": "person" } ]
}
```

没有的字段不出现（`mentions` 没有时是空数组）。事件时间是左闭右开的区间；接口里原样给出区间端点，**显示给人看时**用实际覆盖的最后一天（`[06-12, 06-15)` 显示成 6 月 12 日至 14 日）。工作台快照里的 `memories` 只放最近更新的 200 条，另有 `memoryTotal`；完整列表走新接口（见第 2 批契约）。

### 3.4 时间轴条目

`DeskTimelineItem` 增加 `eventFrom`、`eventTo`、`eventPrecision`、`mentions`（同上），`status` 增加一个取值 `changed`（后来改过）。已有的 `at` 仍然是「什么时候说的」。

### 3.5 接缝函数（S0 开好，名字和参数冻结）

| 函数 | 位置 | S0 做到什么 | 谁填 |
|---|---|---|---|
| `recordUsageTx(ctx, tx, usage modelUsage) error` | `internal/postgres/usage_log.go` | 空实现；S0 在秘书、导办台问答、副手、抽取四处拿到模型结果之后加上调用 | L |
| `secretaryCardsTx` | 从 `desk_turn.go` 原样搬到 `desk_cards.go` | 只搬不改 | K |
| `memoriesTx` | 从 `claims.go` 原样搬到 `memories_read.go` | 只搬不改 | M |

`modelUsage` 的字段和 `model_usage` 表一一对应。

## 4 文件归属

一个文件只有一个写入者。表里没有的产品文件，要动先找协调者。

| 写入者 | 文件 |
|---|---|
| S0 | `internal/postgres/migrations/027_structured_memory.sql`（新）、`internal/workspace/model.go` 和 `desk.go` 里第 3.3、3.4 节的字段、`web/src/domain/` 里对应的类型字段、`usage_log.go`（新，空实现）、`desk_cards.go`（新，搬移）、`memories_read.go`（新，搬移），以及四处 `recordUsageTx` 调用点。S0 合入后这些文件按下面的归属交出去 |
| E1 | `internal/postgres/processing.go`、`claims.go`、`source_context.go`、`entities.go`（新） |
| E2 | `internal/worker/worker.go`、`internal/postgres/jobs.go`、`budget.go`、`embedding_backfill.go`、`backfill.go`（新），`cmd/pcas/` 里启动后台任务的接线 |
| M | `internal/postgres/memories_read.go`、`editing.go`、`actions_log.go`、`workspace.go`、`commands.go`、`internal/httpapi/workspace.go`、`internal/workspace/model.go` |
| U2 | `web/src/pages/LibraryPage.tsx`、`web/src/pages/ThingPage.tsx`、`web/src/components/Marks.tsx`、`web/src/components/RecallSheet.tsx`、`web/src/store/`、`web/src/domain/types.ts`、`web/src/styles/app.css` 里记忆相关的样式 |
| Q1 | `internal/memory/query_plan.go`（新）和它自己的单元测试 |
| Q2 | `internal/postgres/retrieval.go`、`graph.go`、`expand.go`、`desk_turn.go`、`desk.go`、`run_context.go`、`runs.go`、`telegram_turn.go`、`internal/memory/contracts.go` |
| K | `internal/postgres/desk_cards.go`、`internal/workspace/desk.go` |
| U3 | `web/src/components/SecretaryCards.tsx`、`web/src/domain/desk.ts`、`web/src/styles/secretary.css` |
| I | `internal/connectors/`、`internal/postgres/connectors.go`、`connector_runner.go`、`attachments.go`、`import_batches.go`（新）、`internal/httpapi/connectors.go` |
| L | `internal/postgres/usage_log.go`、`internal/httpapi/usage.go`（新） |
| U4 | `web/src/components/ImportSheet.tsx`、`web/src/components/ConnectorSettings.tsx`、新增的导入组件和样式 |
| V | `testdata/phase2/eval/`（新）、`internal/postgres/phase2_eval_test.go`（新）、`cmd/pcas-eval/`（新）、评测报告 |
| T2、T3、T4 | `internal/postgres/phase2_b2_*_test.go`、`phase2_b3_*_test.go`、`phase2_b4_*_test.go`（新）、`testdata/phase2/b2-gold.json` 等、`web/tests/phase2-batch2*.spec.ts` 等、CI 里把新的浏览器用例加进去的那一行、各批的验收报告 |
| 协调者 | `docs/` 其余部分、集成分支、合并、部署 |

几条补充：

- **新接口的路由注册**写在各自的 `httpapi` 文件里已有的注册函数中。非动 `server.go` 不可时只加一行注册，并在 PR 说明里写明。
- **`web/src/styles/app.css`** 有两个人会加样式（U2、U4）：各自只在文件末尾追加自己的一段，用注释标出起止，不改已有规则。
- **实现者可以给自己新写的纯函数配单元测试**，文件名跟着自己的源文件（例如 `query_plan_test.go`、`entities_test.go`）。验收序列的测试只由 T 写；实现者不看、不改 T 的测试。
- **不改已有测试的预期**。因为新规则必须改的，先列给协调者，批准后交给对应批的 T 改。

## 5 共同规矩

- **基线**：S0 从 `origin/main` 建分支，PR 的 base 是 `main`。其余任务从自己那一批的集成分支建，PR 是 Draft，base 是那条集成分支。不合 main，不部署。
- **分支和工作区**：分支名 `phase2/b<批>-<任务>-<简称>`，工作区 `/root/PCAS-wt/b<批>-<任务>`，各任务包里写了具体名字。不在 `/root/PCAS` 里干活。
- **测试环境**：自己起临时 PostgreSQL（`pgvector/pgvector:0.8.2-pg16-bookworm`，tmpfs，随机本机端口），设好 `PCAS_TEST_DATABASE_URL` 后直接跑 `make check`。用假模型服务。不连线上库，不读 `/root/PCAS/.env` 和 `config/`，不调真实模型，不发真实通知。
- **本机规矩**：线上实例跑在同一台机器上。不按名字杀进程（`pkill`、`killall`）；自己起的进程记 pid、容器记 id，只清理自己的。
- **测试的写法**：新测试必须能在普通的 `make check` 下跑；不写死「必须在未来」的日期，用 `internal/testsupport` 的 `DateFromToday`；不依赖今天是星期几。
- **不做的事**：不改白皮书；不加任何授权步骤、绑定模型的判断、会让请求失败的容量上限；不加新的设置项；不新建平行的表去存已有表能存的东西。
- **出错要具体**：每种失败有自己的错误类型和给用户的一句处理建议，日志里写明环节和类型，不含正文和密钥。
- **交付时报告**：做了什么、每条规则对应的代码位置、`make check`（前端是 `npm run lint && npm run type-check && npm run build`）的结果、没做到或拿不准的地方、需要别人配合的地方。拿不准规则含义时先问协调者，不要自己定。
- **界面任务**：先读 [界面与交互原则](../../design/principles.md)。界面上不出现「来源」「陈述」「实体」「版本」「依赖」「抽取」这类内部说法；能用图和卡片表达的不用长文字。截图用模拟数据，不用线上真实内容。

## 6 合并和上线

1. S0 的 PR 进 main：CI 全绿，已有测试的预期一个不改。
2. 各批的实现 PR 由协调者通读代码后合进该批的集成分支。
3. 该批的 T 在集成分支的同一个提交上统一跑全量。失败就是发现，不跳过、不放宽、不反复重跑；协调者按文件归属派回修复。
4. 该批全绿后，协调者开 PR 进 main。部署等用户发话。部署后协调者在线上用真实默认通道验证并清掉测试数据。
5. 第 2 批的集成分支每合入一个任务，协调者就把它合进第 3、4 批的集成分支并通知变基。第 2 批进 main 之后，第 3 批进 main 的 PR 里就只剩第 3 批自己的改动，第 4 批同理。
