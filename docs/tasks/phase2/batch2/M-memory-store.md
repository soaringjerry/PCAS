# 任务 M：记忆库（列表、撤销只标记、副手补开、删除收尾）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 6.1 Sol。分支 `phase2/b2-M-memory-store`，从 `origin/phase2/batch2` 建；工作区 `/root/PCAS-wt/b2-M`；Draft PR 的 base 是 `phase2/batch2`。

先读 [并行方案与数据约定](../parallel.md) 和 [第 2 批契约](README.md) 的 R15–R21 和序列 M1–M11。

## 现在的代码

- `memories_read.go` 的 `memoriesTx`（S0 从 `claims.go` 搬过来的）：先查出全部记忆，再对每一条各查三次（历史版本、对哪些副手开着、出处）。三个调用方：工作台快照（`workspace.go`）、导办台问答（`desk.go`）、副手运行（`runs.go`）；秘书的上下文也走它。
- `actions_log.go` 的 `deleteUndoneTurnMemoriesTx`：撤销连带删记忆时调用通用的删除，于是用到这条记忆的旧回答被清空。
- `editing.go`：`deleteRecordsTx` 是删除的闭包；纠正一条记忆时把旧证据带到新版本。
- `workspace.go`：副手列表从模型配置生成；新建记忆时只对当时启用的副手打开。

## 要交付什么

### 1 读记忆（R15）

`memoriesTx` 的签名不变，调用方不用改。内部改成固定次数的查询，并填上新字段。秘书和副手这两条路只需要正文、性质、可用状态、项目、出处，不要为它们多查没用的东西。

### 2 列表、单条、分面（R16）

三个新接口加在 `internal/httpapi/workspace.go` 里。快照里的 `memories` 截到最近更新的 200 条，`memoryTotal` 是总数。秘书和副手不受这 200 条影响（M2 会验）。

翻页用游标，不用页码；游标里不要放内部主键以外的敏感内容。

### 3 撤销只标记（R17）

撤销连带删记忆这一条路不再清空用到它的回答和副手结果。做法由你定，但必须满足：

- 明确删除（资料库里删记忆、删资料）这条路的行为一个字节不变；
- 被撤掉的记忆本身照样删干净（检索索引、摘要里不再有它）；
- 用到它的对话轮之后读出来带 `outdated: true`（第 1 批 R7 的机制会因为依赖失效自动给出，你要确认它确实给出，而不是被判成「已清空」）。

### 4 后来启用的副手（R18）

「已经开过」记在副手自己的文档里。上线后第一次加载时，把已经启用的副手直接记为开过，不补开。

### 5 删除收尾（R19）和纠正带着走（R20）

- 删记忆之后清掉没人再提到的 AI 实体。写在删除闭包里，明确删除和撤销连带删除都要走到。
- 纠正产生新版本时把 `claim_mentions` 和事件时间带过去。
- `workspace.go` 里导出全部数据的那张表名清单，加上这一阶段新增的四张表。

### 6 提示文字（R21）

`workspace.go` 里把后台任务的错误类型翻成人话的地方，加上 `provider_unavailable` 和 `budget_deferred`。说清发生了什么、用户要不要做什么。

## 你独占的文件

`internal/postgres/memories_read.go`、`editing.go`、`actions_log.go`、`workspace.go`、`commands.go`、`internal/httpapi/workspace.go`、`internal/workspace/model.go`。

不要动：`claims.go` 和 `processing.go`（E1 的）、`desk_turn.go`、`runs.go`、`desk.go`（第 3 批 Q2 的；所以 `memoriesTx` 的签名不能变）。

## 交付

Draft PR。说明里写：每条规则对应的代码位置；读 2000 条记忆用了几次查询、大约多久；三个接口的请求和返回示例；撤销只标记是怎么做到「明确删除不变」的；`make check` 结果；需要改预期的已有测试清单（R17 会让第 1 批里「撤销后旧回答被清空」的断言变化，列出来，不要自己改）。

## 第二轮补充（2026-10-02）

列表接口多三个筛选参数（`project`、`epistemic`、`agent`），按 id 取单条的返回形状也定了，见契约第 8 节和序列 M12、M13。界面那边等着用它们。

## 跟进任务 M2（2026-10-02，协调者审查 PR #113 之后）

#113 已合入。有一处要补，原因是我在本任务包第 1 点写错了：我写的是「秘书和副手这两条路不要多查没用的东西」，于是你在给模型用的那条路（`memoriesTx(..., true)`）上把说话时间、事件时间、提到的人和地点都留空了。但第 3 批的提示词正是从这些记忆对象上取这几个字段，追加到每条记忆后面（第 3 批契约 R12）。现在这样，第 3 批给模型的记忆永远不带时间和地点。

要做的：给模型用的那条路也填上 `ExpressedAt`、事件时间、`Mentions`，和界面那条路的值一致。仍然用固定次数的查询（提及用一次批量查询），不要退回到每条记忆各查一次。历史版本、对哪些副手开着这两样模型用不到，可以继续不查。

另开分支 `phase2/b2-M2-model-fields`，从 `origin/phase2/batch2` 建，Draft PR 的 base 是 `phase2/batch2`。说明里附：2000 条记忆时这条路的查询次数和耗时，改动前后各一组。
