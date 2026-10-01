# T2 提醒与时间独立测试（2026-10-01）

基线 `c94b496`，分支 `stabilization/T2-time-tests`。依据稳定化 R1–R11、第一阶段契约 §3/§4，以及 B1 文档规定的模型 JSON 输入。只新增两个测试文件和本报告，没有修改产品、旧测试、共享夹具或 CI。

**稳定化未通过：11 个编号中 7 个后端序列通过，R1、R2、R5 整项暂存 skip，R10 重试/通知子项通过而日志子项暂存 skip。** 四项 skip 均先真实失败后暂存。默认测试命令退出 0 只表示暂存检查可运行，不表示四项缺陷通过。模拟通知也不表示生产通道或设备验收通过。

## 覆盖矩阵

| 编号 | 独立测试后缀（统一 `TestRn_` 前缀） | 检查与结果 |
|---|---|---|
| R1 | ElapsedLeadFallsBackAndElapsedDueHasNoReminder | 两个子例：5 分钟后截止却默认提前 30 分钟、截止 5 分钟前已经过去。提醒状态与提前发送检查失败；暂存 skip |
| R2 | DateOnlyCreatedAfterNineFallsBackTo2359 | 本地当天 09:00 已过，期望 23:59；仍保存 09:00 并提前发送；暂存 skip |
| R3 | RescheduleRetainsLeadAndRemovingDueDeletesReminder | 秘书改期沿用 `-2h`，旧点不生成通知；空字符串清空截止后触发器删除、新点也不发送；通过 |
| R4 | CompletedBeforeReminderNeverSends | 截止前完成，固定时刻检查两次，不生成 notice、不发送；通过 |
| R5 | UndoCompletionAfterOneHourNeverCatchesUp | 合成历史完成动作，撤销恢复 todo，却补发两小时前提醒；暂存 skip |
| R6 | NonexistentMelbourne0230Becomes0330 | 2026 原样例已通过，最终持续测试用同型远未来 2099-10-04 02:30 → 03:30 AEDT = 前一天 16:30Z，回执显示 03:30；通过 |
| R7 | ReminderAcrossDSTUsesMondayLocalTime | 2099 同型 DST：固定检查周六与周一到点前一秒不发，周一 09:00 AEDT = 周日 22:00Z 恰好发送一次；通过 |
| R8 | ChangingTimezonePreservesUTCAndChangesFutureInterpretation | 墨尔本改上海：旧 UTC 截止/触发器不变，新事项换算与回执用上海，旧提醒消息也用上海；后端通过，首页由 F8 验证 |
| R9 | RestartAndReentryNeverDuplicateNoticeOrSend | 关闭连接池、新建 Store 接同一独占 schema，再检查两次；notice 行数/ID 与发送次数均为 1；通过 |
| R10 | TelegramStopsAfterFiveFailuresAndRecordsCause | 本地 Telegram HTTP 500，固定时刻分发八轮；`retry_limit_and_notice` 的 5 次请求/停止/未关闭 notice 通过并保留正常回归；仅 `failure_log` 暂存 skip |
| R11 | ExpiredPushDeletedAndTelegramStillDelivers | PostgreSQL 持久订阅返回 410 后删除，Telegram 独立送达一次，重入不重发；通过 |
| R11 | ExpiredPushDoesNotBlockHealthySubscription | notify 包补充：410 删除失效订阅，同批健康订阅仍送达，第二批只发送健康订阅；通过 |

## 可复现的发现

以下输出取自 2026-10-01 06:07 UTC 的真实独立数据库运行。每项先运行失败，随后才加入 `t.Skip("finding Rn: ...")`。设置 `PCAS_STABILIZATION_REPRO=1` 会绕过 finding skip，执行原有预期；此开关不会更改断言。

### finding R1：提前量与截止均没有过去时间规则

预期：未来 5 分钟的截止，`nextAt` 回退到截止；截止本身过去，不创建提醒，回执说明「时间已过，没有设提醒」。

实际：

```text
future_due:
NextAt:2026-10-01T05:42:32Z
want next="2026-10-01T06:12:32Z" offset="-30m"
sent before fallback due or for elapsed due
elapsed_due:
due="2026-10-01T06:02:32Z"
NextAt:2026-10-01T05:32:32Z Active:true
回执：已建：今天 06:02 喝水 · 05:32 提醒
```

影响：用户要求几分钟后提醒，却会在截止前立即收到过时提醒；已过截止也保留有效提醒。建议由产品执行者检查截止提醒计算与秘书回执生成模块。`elapsed_due` 在状态/回执断言即失败，后续发送断言未执行；不把未执行部分写成证据。

### finding R2：仅日期事项没有 23:59 回退

预期：上海当地 14:07 创建当天截止事项，当天 09:00 已过，应在 23:59 即 15:59Z 提醒。

实际：

```text
NextAt:2026-10-01T01:00:00Z Active:true
want next="2026-10-01T15:59:00Z" offset="09:00"
date-only reminder sent before 23:59
```

影响：当天早间时刻已过时，日期安排会提前补发，偏离用户预期。建议检查截止提醒计算模块。

### finding R5：撤销完成补发超过一小时的旧提醒

预期：完成后超过提醒两小时再撤销，恢复 todo 和原截止，保留逾期事项的展示资格，但不补发提醒。

实际：

```text
stale reminder caught up after undo: calls=1
DueAt:2026-10-01T04:07:33+00:00
CreatedAt:2026-10-01T06:07:33+00:00
```

影响：恢复一件旧事项会立即收到过期通知。建议检查撤销恢复后的提醒资格或提醒扫描模块。夹具只将动作记录的 `created_at` 标成三小时前，不修改被撤销业务字段或 `afterHash`，撤销本身成功，失败来自 notice 与发送副作用。

### finding R10：固定时刻分发入口缺少失败分类日志

预期：Telegram 连续失败五次后停止，notice 继续存在，并提供通道名和错误类型日志。

实际：

```text
missing log with Telegram channel and error type; sends=5 logs=
```

重试上限与 State 未关闭 notice 的部分通过；默认结构化日志捕获没有任何内容。建议检查通知分发的错误分类日志。`DispatchNotices` 没有 logger 参数，本测试捕获默认 slog；`RunNotify(ctx, logger, channels)` 的长时间循环没有可注入时钟，因此没有真实等待数小时验证 runner 的日志。此发现证明固定时刻公开分发入口没有日志，不推断生产 runner 的全部行为。

## 时钟、夹具和覆盖边界

- `DeskTurn` 无可注入时钟。R1 基于执行时刻前后 5 分钟；R2 从四个 IANA 时区中选择当地 10–20 点，避免午夜/09:00 边界漂移。模型返回确定的本地/UTC时间，不测试真实模型理解自然语言。
- `CheckReminders` 和 `DispatchNotices` 接受固定时刻。数据库 notice 的 `created_at` 使用真实墙钟；专用夹具把本轮新生成 notice 的该簿记字段对齐到模拟检查时刻，避免 2099 年提醒被误当已保存 73 年的旧 notice，或秒截断让新 notice 被当成未来记录。既有 notice、业务截止、触发器和投递状态不因此改动。
- R5 先通过公开命令创建 todo，以 schema 和公开 Item JSON 设置历史截止/触发器，通过公开命令完成，再把完成动作记录时间设为三小时前；完成状态下固定检查不发送，公开 Undo 恢复 todo 后固定检查观测到补发。这是历史状态夹具，不是实等三小时，也没有伪造动作内容指纹。
- 首轮验证了明确的 2026-10-04 夏令时样例；最终 R6/R7 使用同型远未来 2099-10-04（周日）切换与 2099-10-05（周一）到点，避开修复 R1 后历史提醒被移除和一年输入窗口。R7 实际建项发生在本次运行时刻，模拟检查包含周六；没有假称秘书服务器墙钟真的处于周六，或验证真实模型的「下周一」解释。未来 IANA 规则变化需要重新审查固定未来样例，不能据此声称预言 2099 年法规。
- R8 的首页、事项卡片与历史回执切时区后的渲染不在本任务归属内，由 F8 的前端证据补齐；R5「在等你」和 R10「到点了」仅验证其 State 条件，未做浏览器渲染断言。
- R9 重建 Store/连接池，检验数据库持久去重；没有启动/杀死真实服务进程，也未做中途崩溃注入。
- R11 的 API 只接受公网上 HTTPS 订阅，因此使用合成 `https://push.example.invalid/expired`，专用 RoundTripper 将所有请求直接路由到本地 TLS httptest，不解析或连接外网。Telegram 也只接本地服务。
- 首轮误夹具（秘书 `due:null` 不表示清空、HTTP 回环订阅不满足接口、DB 墙钟与模拟检查时刻不一致）已经纠正；未登记为产品缺陷。
- 没有生产默认通道、真实 Telegram 账号、真实推送设备或生产数据验收。

## 命令与环境

Go `go1.26.8 linux/amd64`；自建容器 `pcas-test-T2-time`，镜像 `pgvector/pgvector:0.8.2-pg16-bookworm`；数据库 `pcas_t2`；首轮随机端口 33246，重建后随机端口 33248，均绑定 127.0.0.1，未占用 18138/18139。每个 PostgreSQL 测试复用现有 helper 创建独占随机 schema，退出时删除。

最初 512 MiB tmpfs 在首轮完整 integration 通过后，被默认 WAL 填满：06:09:14 `PANIC: could not write to file pg_wal/xlogtemp.566: No space left on device`。随后的新库验证因数据库不可达失败，属于测试资源故障，没有登记产品 finding。仅删除并重建自己的容器，使用 1 GiB tmpfs、`max_wal_size=128MB`、`min_wal_size=32MB`、`checkpoint_timeout=30s`，重新运行最终验证。

完整复现命令（替换为自己的临时数据库 URL）：

```sh
PCAS_TEST_DATABASE_URL='<disposable-test-database-url>' \
PCAS_STABILIZATION_REPRO=1 \
go test -race -count=1 -v ./internal/postgres ./internal/notify -run '^TestR[0-9]+_'
```

2026-10-01 06:07 UTC 原样例与 06:10 UTC 最终持久测试实际结果均退出 1：R1/R2/R5/R10 日志失败，其余编号通过，notify 补充 R11 通过；最终 R10 重试/通知子项通过。仅运行一个编号可以将 `-run` 改成 `'^TestR5_'`；R1 单独子例可选 `'^TestR1_.*/elapsed_due$'`，R10 日志子项可选 `'^TestR10_.*/failure_log$'`。

默认暂存命令（不设置复现开关）同上，退出 0：PostgreSQL 7 个完整正常通过、3 项 finding skip，R10 重试/通知子项通过且日志子项 skip；notify R11 正常通过。

- 最终 `make check`：退出 0，完成 gofmt 检查、go vet、race 单测与构建。该命令没有数据库环境变量，其跳过的 PostgreSQL 集成测试不算数据库验证。
- 最终独立数据库 `PCAS_TEST_DATABASE_URL=... make test-integration`：退出 0，`go test -race -count=1 ./internal/postgres` 用时 48.296 秒。包含默认暂存的 3 项 finding skip 和 R10 日志子项 skip，不能据此声称 11/11 通过。
- 最终编号专项暂存检查：退出 0，PostgreSQL 用时 3.282 秒、notify 用时 1.026 秒；复现开关专项检查退出 1，失败仍是 R1/R2/R5/R10 日志，R10 重试/通知子项正常通过。
- 清理前在自己的库查询 `pcas_test_%` schema 数量为 0。容器使用 tmpfs，无持久卷；已删除 `pcas-test-T2-time` 并确认不再列出。测试 httptest 服务和 Go 测试进程均已退出，没有后台 PID；自己的构建二进制已移除。原始日志关键信息保存在本报告，临时日志目录在开 PR 后删除；没有连接或修改生产容器及其他项目。
