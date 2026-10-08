# 任务 Q1：把一句问话拆成条件（纯函数）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 6.1 Sol。分支 `phase2/b3-Q1-planner`，从 `origin/main` 建（不用等 S0）；工作区 `/root/PCAS-wt/b3-Q1`；Draft PR 的 base 是 `phase2/batch3`（协调者建好后告诉你，在那之前先在自己的分支上做）。

先读 [并行方案与数据约定](../parallel.md) 第 4、5 节和 [第 3 批契约](README.md) 的 R1–R4 和序列 P1–P7。

## 要交付什么

一个文件 `internal/memory/query_plan.go`：类型 `QueryPlan`、`PlanTime` 和函数 `PlanQuery`，形状按契约 R1，一个字段都不要改名。

- 不查库，不调模型，不读系统时钟（`now` 是参数），不依赖机器时区（`loc` 是参数）。
- 时间说法按 R2 的表。「最近」「这几天」是唯一终点不在今天零点的：终点是明天零点（包含今天）。一周从周一开始。区间左闭右开，起止都是用户时区的零点；夏令时切换那天也是完整的一天（用日历加减，不用加 24 小时）。
- 轴按 R3，性质提示和回忆标记按 R4。
- 认不出来的不猜。输入很长、是空的、全是标点时都不能出错。
- 只用标准库，不加新的依赖。

配一份自己的表驱动单元测试 `query_plan_test.go`。验收序列 P1–P7 由 T3 另外独立写，你不用管它的文件。

## 你独占的文件

`internal/memory/query_plan.go` 和 `internal/memory/query_plan_test.go`。别的都不动。Q2 会调用你的函数，所以签名和字段名定了就不要再变；发现契约里的形状不够用，先找协调者。

## 交付

Draft PR。说明里写：支持的每一种说法和对应的区间；你发现的有歧义的说法是怎么处理的（例如「下周」在周日说）；`go test ./internal/memory/` 和 `make check` 的结果。
