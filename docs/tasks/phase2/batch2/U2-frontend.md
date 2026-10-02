# 任务 U2：记忆卡片上的人、地点、时间；按人按地点翻（前端）

执行者 Opus 5.5。分支 `phase2/b2-U2-frontend`，从 `origin/phase2/batch2` 建；工作区 `/root/PCAS-wt/b2-U2`；Draft PR 的 base 是 `phase2/batch2`。

先读 [并行方案与数据约定](../parallel.md) 第 3.3、5 节、[第 2 批契约](README.md) 的 R16、R22 和序列 U1–U4，以及 [界面与交互原则](../../../design/principles.md)。

## 要交付什么

### 1 记忆卡片

资料库里的记忆（列表里的一行和点开后的详情）要让人一眼看到：提到了谁、在哪、什么时候说的、说的是哪天的事。

- 人和地点是可以点的：点一下，列表就只剩提到他（它）的记忆。
- 「什么时候说的」和「说的是哪天的事」是两件事，别混成一个日期。事件时间按精度显示：某一天、某个月、某一年、一段日子。接口给的是左闭右开的区间，一段日子要显示到实际覆盖的最后一天（`eventTo` 的前一天）。没有就不显示，不留空位。
- 怎么排版、用不用小标签由你定。保持安静，信息多的时候不要挤成一团。

### 2 列表改用新接口

- 记忆列表从 `GET /v1/workspace/memories` 翻页读，往下翻自动接着读。
- 筛选：文字、人、地点、性质。人和地点的备选来自 `GET /v1/workspace/memory-facets`，每个后面带条数。
- 筛选条件写进地址栏，刷新和后退都还在。
- 没有结果、读取失败、还在读，各有各的样子。

### 3 别的地方不要坏

工作台快照里的 `memories` 以后只有最近的 200 条，总数在 `memoryTotal`。把所有用到 `state.memories` 的地方过一遍（资料库、事项页、标记、深入查找等）：

- 显示总数的地方用 `memoryTotal`；
- 要找某一条而它不在这 200 条里时，用 `GET /v1/workspace/memories/{id}` 取；
- 列一张清单写进 PR：每一处原来怎么用、现在怎么处理。

后端接口没合进来之前，用模拟数据自己看效果；字段名按数据约定，不要自己起名。

## 你独占的文件

`web/src/pages/LibraryPage.tsx`、`web/src/pages/ThingPage.tsx`、`web/src/components/Marks.tsx`、`web/src/components/RecallSheet.tsx`、`web/src/store/`、`web/src/domain/types.ts`；`web/src/styles/app.css` 只在文件末尾追加自己的一段，用注释标出起止。

不要动：后端代码；`web/tests/`；秘书相关的组件（`Secretary.tsx`、`SecretaryCards.tsx`，第 3 批 U3 的）；导入相关的组件（第 4 批 U4 的）。

## 约束

- 界面上不出现「实体」「陈述」「来源」「版本」「抽取」「分面」这类内部说法。「提到的人」「地点」「什么时候说的」可以用。
- 手机宽度（390px）下不溢出。
- 已有的浏览器测试照常通过；有哪条的预期因为这次改动必须变，先列给协调者。

## 交付

Draft PR；`npm run lint && npm run type-check && npm run build` 通过；桌面 1440 和手机 390 各三张截图（模拟数据）：带人、地点、时间的记忆列表；按一个人筛选之后；一条记忆的详情。说明里写清你对卡片排版的选择和理由，以及第 3 点的清单。

## 跟进任务 U2b（2026-10-02，协调者审查 PR #99 之后）

PR #99 做得很好，等后端 M 合入后变基重跑、通过了就合。你提的几点已经定了，见契约第 8 节。跟进的活另开分支 `phase2/b2-U2b-frontend`（从 `origin/phase2/batch2` 建，Draft PR 的 base 是 `phase2/batch2`），等 M 的接口合入后做：

- 批准你动这三个原本不归你的文件：`web/src/domain/things.ts`、`web/src/components/CommandPalette.tsx`、`web/src/domain/agent.ts`，各自只改契约第 8 节表里写的那一处。
- 资料库恢复「可信度」筛选，用接口的 `epistemic` 参数。
- `domain/lines.ts` 和 `MemoryActivitySettings.tsx` 不动。
- 资料库页面原有的内部说法不动。
- 事件时间的格式化现在有两份（`Marks.tsx` 和 `desk.ts`），等两批都进了 main 再并成一份，这次不动。
- `app.css` 末尾两段在合并时都保留，由协调者处理冲突。
- 截图分支 `phase2/U-frontend-screenshots` 保留到这三批都进 main。
