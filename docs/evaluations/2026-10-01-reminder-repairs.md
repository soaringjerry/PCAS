# F9 提醒边界与失败日志修复（2026-10-01）

分支 `stabilization/F9-reminder-repairs`；PR base 为 `stabilization/reminder-candidate`。依赖尚未合并的 #20（F7）和 #24（T2），没有向 main 合并或部署。

## 输入、复现与提交

输入 main `c94b49617764a90841a72bb88adb847ab8f51f33`、F7 `e63ee5eb5430ea8b8ebc5cefa766ae806c8fc451`、T2 `a11398834b3b968f1a90ad9b2e6758c48e84fce7`。在独立 worktree `/root/PCAS-wt/F9` 从指定 main 无冲突合并两份输入，候选 `64f0f7826830364635b20ad9ef9e6221e709897e` 已推远端；再创建修复分支。未改输入分支或其他执行者目录。

先在候选基线上设置 `PCAS_STABILIZATION_REPRO=1` 跑原有全部 R 测试，真实退出 1。2026-10-01 06:15 UTC 的观测：

| 发现 | 复现结果 | 修复与验证映射 |
|---|---|---|
| R1 | 未来 5 分钟的截止仍保存过去的默认 -30m 提醒；已过截止仍有触发器，回执称已设提醒 | 产品 `efb1468`；原始 `ElapsedLeadFallsBackAndElapsedDueHasNoReminder` 可观察行为断言保留；内部空数组断言经协调者批准调整（下文逐条披露）；新增 `FallbackRescheduleRestoresOriginalLead` 检查改到 2 小时后恢复 -30m、再次改到过去无有效提醒并说明 |
| R2 | 当天只有日期的任务仍保存已经过的 09:00，并提前发送 | 产品 `efb1468`；原始 `DateOnlyCreatedAfterNineFallsBackTo2359` 断言保留，补充公开改期后恢复次日 09:00；新增固定上海时区 08:00、09:00、15:00 与改期的独立 UTC 预期 |
| R5 | 公开撤销完成成功，但补发两小时前的提醒，生成 1 个 notice | 产品 `efb1468`；原始 `UndoCompletionAfterOneHourNeverCatchesUp` 保留；新增重启、连续撤销、严格 1 小时边界、未来/近期恢复、普通晚提醒补发、已有成功/关闭/失败重试数据与删除传播检查 |
| R10 | 八次检查下发送次数正确地停在 5，notice 保留；默认结构化日志完全为空 | 产品 `efb1468`；原始重试/notice 与日志断言保留，增加健康通道只成功一次、4 次 retry 与第 5 次 stopped、安全日志字段检查；新增未知通道错误中的合成正文/凭据 URL 不泄露检查 |

测试交付 `e61e3cb486f2ac506694cd52675b1ebe40b94ab1`。删除四处 finding skip 开关及其无用 os import。协调审查追加交叉发现后，有两处表示断言获明确批准调整，未降低发送/回执/通知的独立预期。原有 R3/R4/R6/R7/R8/R9/R11、R10 重试次数与首页数据断言全部保留。未修改 T2 作者报告。

### 协调审查补充的 R1/R3 交叉发现

在初次修复后，协调者要求复现 `已有 -2h → 截止改过去 → 只改截止回未来`。2026-10-01 06:22 UTC 的公开 DeskTurn 独立测试真实退出 1：未来四小时截止恢复成默认 `-30m`（nextAt 为 +3h30m），而应保留 `-2h`（nextAt 为 +2h）。这是删除过去提醒记录导致原 offset 丢失。修复与交叉测试提交 `ccde4c12b7d852f35a9f823a73e7e4a3a10afa5a`。

协调者基于正式契约 §3/R3 的原偏好重算和 R1 的「无有效提醒」要求，明确批准仅以下两处调整：

1. T2 原始 R1 `elapsed_due` 的 `len(Triggers)==0` 改成恰好一个 `due-reminder`、`Active=false`、`NextAt=空`、`Offset=-30m`。原 Due、回执说明、到点前后均无 notice/无发送断言保持不变。
2. F9 新增 `FallbackRescheduleRestoresOriginalLead` 的过去截止空数组断言作同样调整，保持原 offset。

新交叉序列继续独立要求 -2h 原值恢复，没有改成默认值来迎合实现。不能声称原 R1 内部表示断言原样全部通过。显式清空截止及 `remind=none` 仍删除固定记录（原 R3/offset 测试保留）。

## 行为与实现边界

R1/R2 在原有 `applyDueReminder` 计算层处理，新增私有可注入 now 的计算函数供固定边界测试。截止尚未到、按 offset 计算已过时，`nextAt` 回退到截止；原 offset 保留，所以未来改期仍按原偏好计算。截止已过则保留固定提醒的 inactive 偏好记录，NextAt 为空，未来改期按原 offset 激活；显式清空截止或 none 才移除。秘书新建/改期回执说明「时间已过，没有设提醒」。其他触发器保持原状。只读检查确认 ThingPage 的 dueRemindAt 和 hall/lines 都使用 active && nextAt；秘书回执只列 Active 提醒，没有改前端。

R5 在 Undo 恢复层处理，和事项恢复同一事务：只在当前 task 为 done、恢复到 todo/doing/waiting 时，对恢复的 active 提醒 occurrence 检查 `nextAt < undoNow - 1h`，恰好一小时不抑制。复用 `workspace_notices` 的 `(owner,item,trigger,due)` 去重键，`delivered._suppressed=true` 表示该 occurrence 不再分发。新插入的内部记录还有 `_suppressionOnly=true`，Notice、Job 和 Activity 都过滤它，避免把抑制说成「提醒了你」。已存在的真实 notice 只合并抑制标记；投递时间、attempts、retryAt、reason、id、关闭状态均不覆盖，也不会隐藏已有真实记录。已关闭 occurrence 不更新。分发列表和每通道发送前的重读均排除抑制标记。

这没有伪造通道成功、改任务触发器/截止/快照、扩大内容指纹排除字段、修改 30 天清理或删除传播规则，也没有新增表、队列或公共 API。选择既有 occurrence 账本，是因为它已经承担持久去重和事项删除级联；比修改 restored item 更能保持 F7 连续撤销指纹。普通未撤销任务即使晚了两小时，仍按原语义补发；新 due 对应的不同 occurrence 和尚未到点的恢复提醒正常发送。

R10 维持原重试策略、五次上限和通道独立性。失败日志使用默认 slog 的结构化字段 `channel`、`error_type`、`attempt`、`status`；类别来自已知 sentinel 或安全的 `channel_failed`，不输出原始 error、消息正文、事项 ID、token 或 URL。正式 runner 的长时间调度节奏未做真机等待验证。

协调者明确授权额外写入 `actions_log.go` 的一个恢复调用点和 `workspace.go` 的三类查询过滤；其他变更仅在预留的三个产品文件、交接后的 PostgreSQL T2 测试及本报告。没有修改前端、共享夹具、公共类型或其他任务报告；notify 包原 T2 R11 测试无需改动。

## 验证

仅使用本地 httptest 模型/通知、独占 PostgreSQL 随机 schema。全部 R1–R11 及边界共 21 个顶层测试正常执行，零 finding skip；最新专项命令退出 0（PostgreSQL 9.757s，notify 1.031s）：

```sh
PCAS_TEST_DATABASE_URL='<F9-disposable-test-database-url>' \
  go test -race -count=1 -v ./internal/postgres ./internal/notify -run '^TestR[0-9]+_'
```

- `make check`：退出 0；gofmt、go vet、race 单测、构建通过。此命令不带数据库变量，旧 DB helper 的环境 skip 不计作集成通过证据。
- 完整 `PCAS_TEST_DATABASE_URL=... make test-integration`：退出 0，最终 60.085s，执行到 `e61e3cb`；随后偏好收尾提交 `ccde4c1` 按协调指令仅重跑受影响回归，未将此前无关全量检查冒称为最终提交的完整重跑。
- 偏好收尾后 `make fmt-check lint build`：退出 0；独占 DB 的 `TestDueReminder*`、`TestSecretary*`、`TestUndo*`、删除快照与保留期回归：退出 0，25.992s。
- 重点边界：旧抑制 occurrence 重启后无发送且不进入 Notice/Job/Activity；完成→撤销→撤销前一次改名成功；原 due 留在 todo；改到未来新 occurrence 正常送达；未来/近期撤销恢复送达一次；普通旧提醒服务重启后补发一次；既有成功时间、失败重试状态、notice ID、dismissed 状态保留；严格 >1h、非完成恢复与 inactive 不抑制；事项删除清理内部 occurrence。
- F7 的既有撤销、业务内容变化拒绝、遗留指纹、采纳后继续撤销、删除传播与快照保留测试包含在完整集成验证中。没有将未裁定的 T1 U3/U4/U5 新错误码语义当成通过项。

## 环境、局限与清理

Go `go1.26.8 linux/amd64`；容器 `pcas-test-F9-reminders`，镜像 `pgvector/pgvector:0.8.2-pg16-bookworm`。数据库使用 1 GiB tmpfs，WAL `max_wal_size=128MB`/`min_wal_size=32MB`，Docker 随机分配 127.0.0.1:33249；没有占用前端验收端口。最大观测占用低于 tmpfs 配额，无资源故障。

DeskTurn 没有公开可注入时钟；原始 R1/R2 仍按运行时刻前后和可用当地白天时区构造。新增上午/下午及精确一小时边界使用私有计算/事务 helper 的固定时钟，是边界验证而非声称真实服务器已运行到那个时刻。公开 Undo、重建 Store、分发与状态投影均有真实数据库验证，但没有进程崩溃中途注入。首页只验证 State 条件，未新增浏览器渲染测试。真实默认模型通道、Telegram 账号、推送设备和线上 11 项验收仍待进行。

清理前查询自有测试库 `pcas_test_%` schema 数量为 0；容器仅使用 tmpfs，没有持久数据卷，已停止、删除并确认不再列出。所有 Go 测试/httptest 服务均已退出，未启动外部后台服务或遗留 PID；自有构建二进制已移除。只清理本任务容器和临时文件，不使用 pkill/killall，不连接、重启或读取生产及其他项目资源。清理命令最初因强制删除写法被自动审查拦截，改用明确容器名先 stop 再 rm 已完成。临时日志在 PR 发布后删除，报告保留经过整理的复现与结果。
