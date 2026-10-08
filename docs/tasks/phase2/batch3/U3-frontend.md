# 任务 U3：时间轴卡片的界面（前端）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 Opus 5.5（可以是做完 U2 的同一个执行者）。分支 `phase2/b3-U3-frontend`，从 `origin/phase2/batch3` 建；工作区 `/root/PCAS-wt/b3-U3`；Draft PR 的 base 是 `phase2/batch3`。

先读 [并行方案与数据约定](../parallel.md) 第 3.4、5 节、[第 3 批契约](README.md) 的 R17 和序列 V1–V3，以及 [界面与交互原则](../../../design/principles.md)。这是一个小任务：只改秘书回答下面的时间轴卡片。

## 要交付什么

时间轴每一条现在只有一个日期、一段文字和「做完 / 放弃」的标记。后端会多给：说的是哪天的事（`eventFrom`、`eventTo`、`eventPrecision`）、提到的人和地点（`mentions`）、一个新状态 `changed`。

- **两个时间要分得清**：左边的日期是「哪天说的」；「说的是哪天的事」放在文字旁边，按精度写成某天、某月、某年或一段日子。接口给的是左闭右开的区间，一段日子显示到实际覆盖的最后一天（`eventTo` 的前一天）。两者是同一天时不重复写。
- **状态**：已完成、已取消、后来改过，各有一个不抢眼的标记；没有变化的不标。
- **没有说话时间**的条目显示「时间不详」，排在最后。
- **人和地点**安静地放在文字后面，多了就省略，不要把一行撑成三行。
- 只有一条时也要好看（这一批之后，回忆类的问题一条也会出时间轴）。
- 点一条打开原话，沿用现有行为（第 1 批 C2 做的面板）。

## 你独占的文件

`web/src/components/SecretaryCards.tsx`、`web/src/domain/desk.ts`、`web/src/styles/secretary.css`。

不要动：后端代码、`web/tests/`、资料库和导入相关的文件。

## 约束

界面上不出现内部说法。手机宽度（390px）下不溢出、不遮挡输入框。已有的浏览器测试照常通过；预期必须变的先列给协调者。

## 交付

Draft PR；`npm run lint && npm run type-check && npm run build` 通过；桌面 1440 和手机 390 各两张截图（模拟数据）：一张四条各一种状态的时间轴，一张只有一条的时间轴。说明里写清两个时间是怎么区分的。
