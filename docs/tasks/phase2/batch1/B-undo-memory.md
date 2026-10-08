# 任务 B：撤销连带记忆、清理 2.0 遗留

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 6.1 Sol / high。分支 `phase2/b1-B-undo-memory`，工作区 `/root/PCAS-wt/b1-B`，PR base `phase2/batch1`。

先读 [本批总览与契约](README.md)。本任务实现其中的 R9、R10。规则、文件归属以总览为准。

## 要交付什么

1. 一个共用的判断函数：某一轮秘书对话是不是「整轮动作都被撤销」。
2. 撤销使某一轮变成这种状态时，删除从这一轮原话抽出来的记忆。
3. 后台抽取遇到这种轮次的原话时，不再生成那些记忆。
4. 一个迁移，清掉 2.0 留在数据库里的空表、多余的列和迁移记录。

## 你独占的文件

`internal/postgres/undone_turns.go`（新建）、`actions_log.go`、`processing.go`、`internal/postgres/migrations/026_drop_phase2_0_leftovers.sql`（新建）。

不要动：`retrieval.go`、`desk_turn.go`、`runs.go`、`run_context.go`（A 的）；`web/`（C 的）；任何测试文件（T 的）。

## 顺序：先交判断函数

**第一个提交只包含 `undone_turns.go` 里的这一个函数**，推上分支后立刻告诉协调者。A 要调用它，协调者会先把这个提交合进集成分支。

```go
// 某一轮秘书对话是否至少有一条动作记录，并且全部已撤销。
// requestID 是这一轮的请求 id，也是它的原话资料的 external_id。
// 没有这一轮，或这一轮没有任何动作记录，返回 false。
func deskTurnFullyUndoneTx(ctx context.Context, tx pgx.Tx, ownerID memory.ID, requestID string) (bool, error)
```

依据的数据：`desk_turns`（`request_id`、`id`）和 `action_log`（`turn_id`、`undone_at`、`source='desk'`）。请求 id 的大小写按现有代码的比较方式处理（原话资料的 `external_id` 和 `desk_turns.request_id` 在别处是用 `lower()` 比较的）。

## 做法

### 1 撤销时删除记忆（R9 第 1 点）

在 `undoActionTx`（`actions_log.go`）里，撤销成功并写下 `undone_at` 之后、同一个事务内：

- 被撤销的动作属于某一轮秘书对话（`source='desk'` 且有 `turn_id`）才往下走。
- 用上面的函数判断这一轮是否已经整轮撤销。不是就结束。
- 找到这一轮的原话资料（来源类型 `desk`，`external_id` 等于这一轮的请求 id），再找证据来自它的陈述（`evidence` 表）。
- 按 R9 的三个条件筛选要删的陈述：证据全部来自这份原话；不是用户确认过的；性质是计划或意向，或者这一轮没有「记下了」回执。回执存在 `desk_turns.response` 里，`op` 为 `remember`。
- 删除用现有的删除流程（`deleteRecordsTx` 一类），只删陈述，不带原话，不设重导入阻断。这样检索索引、摘要、用到它的回答的清理都沿用现有的删除传播，不要另写一套。

撤销被拒绝（返回任何错误）时这一段不执行。重放同一个撤销请求得到「已撤销过」，不会再走到这里。

### 2 抽取时跳过（R9 第 2 点）

`ProcessExtraction`（`processing.go`）处理到来源类型为 `desk` 的原话时，先判断它那一轮是否整轮撤销。是的话，循环里按同样的条件跳过会被删的那些条目（计划、意向；没有「记下了」回执时全部记忆类条目）。其他条目照常。任务本身照常完成，不报错。

这是为了处理「先撤销、后抽取」的顺序：抽取是后台任务，可能在撤销之后才跑。

### 3 清理迁移（R10）

`026_drop_phase2_0_leftovers.sql`，内容见总览 R10。要点：

- 全部用 `DROP TABLE IF EXISTS`、`ALTER TABLE … DROP COLUMN IF EXISTS`、`DELETE FROM schema_migrations WHERE name IN (…)`。
- 表之间有外键，注意删除顺序，或者对这 6 张表用 `CASCADE`。只对这 6 张表用，不要波及别的表。
- 在一个从来没有这些东西的新库上执行不能报错。
- 2.0 的建表语句可以在标签 `archive/phase2-0/merged` 的 `internal/postgres/migrations/022`–`025` 里看到，用来核对名字，不要把它们搬回来。

## 容易错的地方

- **只删陈述，不删原话，不改对话记录。** 用户说过的话要留着。
- **用户确认过的记忆不删。** 证据不止来自这句话的记忆也不删。
- 一轮里有多个动作时，只有最后一个也撤销了才触发。先新建、再修改的两个动作分属两轮对话，各看各的轮次。
- 不是秘书动作的撤销（来源不是 `desk`）不触发。
- 删除记忆会让用到它的旧回答被清空，这是现有删除传播的行为，这一批不改。用户已决定第 2 批改成只标记（[第二阶段入口](../README.md) 决定 8），这一批不要提前做。

## 交付

Draft PR，说明里写 R9 三处和 R10 各自的代码位置；`make check` 通过（需要临时 PostgreSQL，见总览第 8 节）；列出你认为需要改预期的已有测试（不要自己改）；列出拿不准的地方。
