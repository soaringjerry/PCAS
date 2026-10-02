# 任务 E2：后台队列的优先级、重试和旧资料补做（后端）

执行者 6.1 Sol。分支 `phase2/b2-E2-backfill`，从 `origin/phase2/batch2` 建；工作区 `/root/PCAS-wt/b2-E2`；Draft PR 的 base 是 `phase2/batch2`。

先读 [并行方案与数据约定](../parallel.md) 和 [第 2 批契约](README.md) 的 R11–R14 和序列 F1–F8。

## 现在的代码

- `jobs.go`：领取任务的查询按 `available_at, created_at` 排序；`ProcessChunks` 分段之后排 `source.extract`、`source.embed`、`source.tokenize`。
- `internal/worker/worker.go`：处理失败时，`JobError` 带 `Code` 和 `Retry`；其他错误里只要是 `memory.ErrUnavailable` 就判成 `provider_not_configured` 并停住。线上已经出现过：第一次模型输出不合格，重试时通道暂时不可用，任务就此永久停住（待办第 18 项）。
- `budget.go` 的 `reserveModelCost`：超出每日预算时返回 `memory.ErrUnavailable`，结果同样是被停住。
- `embedding_backfill.go`：已有一个补做向量的小例子，可以参考它的接线方式。

## 要交付什么

### 1 优先级（R11、R14）

- 领取任务按 `priority` 再按原顺序。
- 排抽取任务时：资料属于上传归档（`archive_entries` 里有它）的，`priority` 写 10。
- 领取时跳过所属导入批次处于 `paused` 的任务（通过 `archive_entries.archive_id` 找到 `import_batches`）。`import_batches` 的状态由第 4 批的任务 I 写，你只读。

### 2 重试（R12）

- `JobError` 支持「到某个时间再试、不消耗次数」。
- `worker.go` 按契约 R12 那张表处理四类失败。`provider_unavailable` 的间隔是 1 分钟、5 分钟、15 分钟、1 小时。
- `budget.go`：超出预算返回可顺延的错误，顺延到用户时区第二天零点之后（加一点随机错开，不要所有任务同一秒醒来）。时区取用户设置；夏令时那一天也要算对。
- 向量、分词这些别的环节遇到通道暂时不可用，同样按可重试处理。

### 3 补做（R13）

- 新文件 `backfill.go`：一次检查做的事见 R13。「每小时最多开始 30 个」「等着的不超过 20 个」「两个进程同时检查不重复排」都要在数据库层面成立，不靠进程内的变量。
- 在后台进程里每 10 分钟调一次。接线写在 `cmd/pcas/` 里现有启动后台任务的地方，尽量少改。
- 20、30、10 分钟写成常量，不做成设置项。
- 「每小时最多 30 个」按**排入**的时间算，不按开始处理的时间算。
- 入口的名字和参数按契约第 6 节，T2 的测试会直接调用它们：`BackfillExtractions(ctx, now)`、`nextBudgetDay(now, loc)`，随机错开不超过 10 分钟。

## 你独占的文件

`internal/worker/worker.go`、`internal/postgres/jobs.go`、`budget.go`、`embedding_backfill.go`、`backfill.go`（新），`cmd/pcas/` 里启动后台任务的接线。可以给 `backfill.go` 和 `worker.go` 配自己的单元测试。

不要动：`processing.go`（E1 的；它负责返回对的错误类型）、`workspace.go`（M 的；新错误类型的提示文字归它）。

## 交付

Draft PR。说明里写：每条规则对应的代码位置；领取任务的新查询和它用到的索引；补做的限速在数据库里是怎么保证的；`make check` 结果；需要改预期的已有测试清单；用假模型模拟「不合格 → 不可用 → 成功」和「预算用完 → 第二天」的实际结果。

## 第二轮补充（2026-10-02）

- 迁移 028（`028_backfill_queue.sql`）分配给你：`memory_jobs` 加 `backfill_queued_at timestamptz`，按需加索引。用法见契约第 7 节。这个迁移只加这一列，别的不动。
- 「等着不超过 20 个」「每小时不超过 30 个」都按资料算，长资料拆出的子段不计。
- `Makefile` 里两处 `go test` 加 `-timeout 30m`，放在你的 PR 里。
