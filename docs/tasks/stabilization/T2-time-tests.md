# T2：独立检验提醒与时间

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者：6.1 Sol，只写测试。先读 [执行分工](dispatch.md)、[状态序列表 §2.2](README.md) 和 [接口契约 §3、§4](../phase1/contracts.md)。基线是已合并 F6 的 main `c94b496`。

## 要做的事

按 R1–R11 原有序列逐条落成测试；每个序列一个可单独运行的测试，名称包含 R 编号。验证副作用与最终状态，不只检查 HTTP 成功。

- R1/R2：提前量落在过去、只有日期而早间提醒已过、截止时间本身已过。
- R3–R5：改期/移除截止、完成后不发送、撤销完成后超过一小时不补发。
- R6/R7：墨尔本不存在的 02:30 顺延到 03:30；跨夏令时仍按本地日期触发。
- R8：设置时区只影响后续解释和显示，不平移已存的 UTC 事项和提醒。
- R9–R11：重启/重入不重复通知；Telegram 失败停止重试并记录原因；410 推送订阅失效不妨碍其他通道。

## 归属

只新增 `internal/postgres/stabilization_time_test.go`、`internal/notify/stabilization_time_test.go` 及报告 `docs/evaluations/2026-10-01-stabilization-time.md`。如某序列只能在其他包访问公开 API，先报告，不自行改归属。

可复用现有测试 helpers，必要的专用 helper 放在新文件中、前缀 `stabilizationTime`。不修改共享 helpers、迁移、产品代码、前端或 CI。不能按当前实现改预期。

## 方法和交付

用独立 tmpfs 测试库、合成资料、本地假通知服务。优先固定时刻或调用现有允许传入时间的接口，避免长时间 sleep。没有可注入时间且无法可靠覆盖的，明确报告障碍，不伪称通过。

真实失败先保存测试命令、关键输出和观测到的状态，再按阶段规则用 `t.Skip("finding Rn: ...")` 暂存；报告列明预期、实际、是否影响用户、建议修复模块。暂存 skip 是未完成项，不算验收通过。已经通过的测试正常执行。

报告另列 R1–R11 覆盖矩阵、运行环境、结果与资源清理。完成 `make check` 和独立数据库集成检查，提交并开测试 PR。不要修复产品；由协调者据发现分派后续执行者。
