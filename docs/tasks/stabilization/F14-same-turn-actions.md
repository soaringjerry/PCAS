# F14：一句话新建任务并继续加步骤

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 Sol / high。用户于 2026-10-01 确认：一句话「建交作业任务，再给它加查资料、写提纲两个步骤」应指向本轮刚建的任务并一次办完。基线 `59e25daa67df7447f0a09b7f0bd9e5d65edab65e`；工作区 `/root/PCAS-wt/F14`，分支 `stabilization/F14-same-turn-actions`；PR base `stabilization/acceptance-candidate`。

## 首先审查内部引用方案

建议沿用 actions 顺序，用 `N1` 表示 actions 数组第 1 个动作成功创建的事项（序号按原数组从 1 开始，不按成功次数重排）。后续动作只能引用更早且成功创建的对象；不新增自定义 id 字段，不重用 R1、THIS、delegate 的 new，不允许 UUID 或标题猜测。先读取现有解析和执行路径，向协调者回报兼容性与最小方案；协调者确认并更新正式契约后再写实现。这是技术审查，不再让用户选择内部字段。

技术审查已完成，协调者已将上述方案写入正式契约 §2.1 并授权实现：N 只进入独立动作引用表，可用于 ref/project/set.project，Used/Links/Show 仍用原上下文；只绑定 create_task/create_idea/create_project 成功提交后的对象，附带创建不绑定。

## 验收边界

- 实际 DeskTurn 入口、实际假模型 HTTP 请求，正确目标、回执、逐动作 action_log 及逆序撤销；失败引用保留跳过原因。
- 多个新对象、前置解析失败/创建失败、前向引用、序号越界、引用非创建动作、跨轮残留均不能误改旧事项。动作最多 10 个的规则保持。
- 同 requestId 重放不重复创建或加步骤。后续对话的 R 别名仍按已有语义工作。
- 只在动作子事务成功提交后绑定；不建立第二份持久状态库。每个动作仍有独立撤销记录。

## 文件归属与交付

独占 `internal/postgres/desk_turn.go`、`desk_actions.go`、`desk_parse.go`（只改有必要的文件），新增 `internal/postgres/same_turn_actions_test.go`、`docs/evaluations/2026-10-01-same-turn-actions.md`。不得改 `actions_log.go`（F13）、`stabilization_undo_test.go`（T4）、共享 helper 或他人报告。需要额外文件先报协调者。

读协调者工作区的本任务、[正式契约](../phase1/contracts.md) 与 [执行分工](dispatch.md)。隔离 tmpfs PostgreSQL、假模型/通知；Docker 分配数据库端口，HTTP 如需要 18154/18155；不访问生产/秘密/真实模型，不外发。只清理自己的资源。交付 make check、针对性集成回归、报告、最终提交和 Draft PR；不合 main、不部署。交付后停止写入，T4 独立补 U10 并在最终候选验收。
