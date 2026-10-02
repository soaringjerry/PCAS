# 任务 D：旧测试里写死的日期（测试夹具）

执行者 6.1 Sol（做任务 B 的执行者）。分支 `fix/test-fixed-dates`，**从 `origin/main` 建**；工作区 `/root/PCAS-wt/fix-dates`；PR base 是 **`main`**（不是集成分支）。

这不是第 1 批的功能，而是主干上的旧问题，但它挡住了第 1 批：不修，`make check` 从 2026-10-02 起在任何分支上都不会全绿。

## 现象

一批已有测试把事项时间写死成 `2026-10-02T15:00` 这类日期。写的时候它在未来，系统会给它安排提醒；到了 2026-10-02 当天，时间一过，系统按设计不再给已过去的时间安排提醒，断言就失败了。任务 A 的执行者在未修改的基线上复现了四项：

- `TestCodexSecretaryAndLegacyFormats`（`desk_codex_test.go`）
- `TestSecretaryHistoryOneEntryPerAction`（`desk_history_test.go`）
- `TestSecretarySchedulesAndUpdatesRecentTask`（`desk_turn_test.go`）
- `TestDueReminderOffsets`（`undo_test.go`）

同样写法的日期还散在别的文件里（`2026-10-05`、`2026-10-01` 等），过几天会接着坏。产品行为是对的，不改产品代码。

## 要交付什么

1. **找全。** `internal/` 下所有 `_test.go` 和 `web/tests/` 下所有用例里，凡是「必须在未来才成立」的绝对日期，全部找出来。已知涉及：`internal/postgres/` 的 `actions_log_test.go`、`conversation_order_acceptance_test.go`、`desk_codex_test.go`、`desk_history_test.go`、`desk_receipt_test.go`、`desk_turn_order_test.go`、`desk_turn_test.go`、`stabilization_secretary_test.go`、`undo_test.go`，`internal/telegram/` 的 `format_test.go`、`integration_test.go`，以及 `web/tests/` 里约 26 处。逐处判断：只是当作一个任意时间戳、不依赖「在未来」的，可以不动，但要在 PR 说明里列出并说明为什么安全。
2. **改成相对今天。** 在测试辅助代码里加一个小函数（Go 一处、网页测试一处，网页那边 `web/tests/support/real.ts` 已有 `daysFromToday`、`upcomingWeekday`，优先复用），由「今天起第 N 天 + 时刻」算出日期，夹具和断言都从它取。断言里跟着变的派生值（提醒时间、换算成 UTC 的时间、回执上的星期几和日期文字）也从同一个值算，不要一半相对一半写死。
3. **不依赖今天是星期几、不依赖几点跑。** 用例在一天里任何时刻、一周里任何一天、月末年末跑都要通过。时区相关的换算按用例自己设的时区算，不按机器时区。
4. **只改日期的来源，不改断言的意思。** 每个测试原来验证什么，改完还验证什么。不许 `t.Skip`，不许删断言，不许放宽成「不为空」这类。

## 证明它真的修好了

- `make check` 全绿（这个分支从 main 建，没有第 1 批的三组旧预期问题）。
- 把系统时间拨到别的日子再跑一遍受影响的测试，至少三个时间点：某天 23:50（本地时区下已是第二天）、一个周日、一个月的最后一天。可以用 `faketime` 一类工具，或在临时容器里跑；怎么做的写进 PR 说明。跑不了「拨时间」的话直接说，不要用「今天跑通了」代替。
- 网页测试：`browser-mocked` 和三轮 `browser-real-backend` 通过。

## 你独占的文件

上面列出的已有测试文件里**只动日期夹具和跟着它变的断言**，外加测试辅助文件里新增的日期函数。

不要动：产品代码；`internal/postgres/phase2_b1_*_test.go`、`testdata/phase2/`、`web/tests/phase2-batch1*.spec.ts`（验收执行者 T 的，T 自己保证不写死日期）；下面三个测试函数由 T 按第 1 批规则改预期，你不要碰它们的函数体，同文件里的其他函数可以改：

- `desk_dependency_growth_test.go` 的 `TestSecretaryConversationDependenciesStayASet`
- `ux_regressions_test.go` 的 `TestDeskHistoryCannotBypassDestinationItemScope`
- `desk_turn_test.go` 的 `TestSecretaryStablePrefixAndVisibility`

## 环境和纪律

和第 1 批相同（[总览](README.md) 第 8 节）：自己起临时 PostgreSQL，用假模型；不连线上库，不读 `/root/PCAS/.env` 和 `config/`；不按名字杀进程，只清理自己起的进程和容器。

## 交付

Draft PR，base `main`。说明里写：找到的全部位置（改了的、判断不用改的各一张表）、辅助函数放在哪、拨时间验证的做法和结果、`make check` 结果。
