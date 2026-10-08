# 任务 T1：第 1 批独立验收

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 6.1 Sol（不做本批实现的那一位）。分支 `phase2_5/b1-T1-acceptance`，从合入 O1a 之后的 `origin/phase2_5/batch1` 建；工作区 `/root/PCAS-wt/p25-T1`；Draft PR 的 base 是 `phase2_5/batch1`。

只读 [阶段入口](../README.md) 和 [第 1 批契约](README.md)。**不看 O1b 的实现，不参考它的测试。** 你和实现照着同一份契约各写各的，这样才能发现契约没说清或者实现理解错的地方。

## 要交付什么

1. `internal/postgres/phase2_5_b1_*_test.go`：契约第 4 节的 X1–X22，每条序列至少一个测试，测试名里带序列号（例如 `TestPhase25B1_X08_CorrectedDuringCall`）。
2. 一个随机序列测试：随机穿插「新增记忆、整理一批、纠正、删除、改规则版本」若干步，每一步之后检查这些不变量：
   - 没有任何记忆因为整理而多出修订；
   - 每条 `organized` 等于当前规则版本的记忆，类型不是空的，分组提及指向存在的实体；
   - 人、地点、机构的提及和整理之前一样；
   - 没有重复的分组实体（同类型同名）；
   - 快照的 `organize.done` 等于实际已整理的条数。
3. 发现清单，写进 PR 描述：编号 `F-B1-n`、对应的规则或序列、现象、你认为契约该怎么理解。

## 怎么写

- 模型用假的。O1b 会在 PR 描述里给出模型输出的 JSON 格式；在那之前，先按契约写不依赖输出格式的部分（数据约定、接口字段、筛选、删除），把依赖格式的测试写成能换一处就适配的样子。
- 测试数据全部虚构。记忆的文字自己编，不要抄任何真实内容。
- 触发整理要走产品的入口（后台任务处理函数），不要直接调内部的写入函数。
- 没通过的用例用 `t.Skip("finding F-B1-n")` 标出来并写进发现清单。不改产品代码，不改别人的测试。
- 并发的序列（X22）要真的起两个并发的处理者，跑足够多次。
- 模拟事务中途失败（X12）、调用期间被纠正或删除（X8、X9）时，用假模型的回调在「模型调用中」这个时间点做手脚，不要靠 sleep。

## 完成的标准

O1b 合入集成分支后，在它上面跑：X1–X22 和随机序列全部通过，或者没通过的每一条都有编号的发现。把最终一轮的结果写成 `docs/evaluations/` 下的一份验收记录（文件名以日期开头），并在阶段入口的文档表里加上链接。
