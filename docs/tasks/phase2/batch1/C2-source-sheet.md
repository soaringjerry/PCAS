# 任务 C2：从依据卡片点开，直接看到当时那句话（前端）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 Opus 5.5（任务 C 的同一个执行者）。分支 `phase2/b1-C2-source-sheet`，从 `origin/phase2/batch1` 建；工作区 `/root/PCAS-wt/b1-C2`；PR base `phase2/batch1`。

先读 [本批总览与契约](README.md) 的 R8、R8a 和第 7、8 节，以及 [界面与交互原则](../../../design/principles.md)。这是任务 C 交付时提出来的后续：依据卡片里的原话现在能点开，但点开之后的面板是给资料库用的，不适合日常对话。

## 现在的问题

在秘书的依据卡片上点一条原话，打开的是 `SourceSheet`。面板里依次是：

- 标题下面一个「第 3 版」标签；
- 处理任务的状态标签（阶段名、状态、错误码）；
- 一块默认展开的「来源摘要与当前理解」，末尾写着「依据 N 条材料的有效版本生成；历史原话与当前理解分别保留。」；
- 最下面一个**折叠着**的「展开原文」。

用户点的是「我当时说的那句话」，看到的却是版本号和一段摘要，原话还要再点一次。标题取不到时显示「来源原文」。这些都是内部说法（交互原则：系统内部的概念不出现在日常界面上）。

## 要交付什么

给 `SourceSheet` 加一种「从对话里点开」的用法，只有秘书的依据卡片（`SecretaryCards.tsx` 里的 `Sources`，以及时间轴上带原文的条目）使用它。这种用法下：

1. **原文直接可见，不折叠，放在最上面。** 不用再点任何东西。
2. **定位到被引用的那一段。** 调用方把依据卡片上那段摘录传进来。摘录去掉首尾的「…」之后，如果能在原文里原样找到，就滚动到那一段并把它标出来（底色或左侧线，安静一点，不闪烁）；找不到就从头显示，不报错、不提示。记忆条目（`kind` 不是 `source`）点开时没有摘录，从头显示。
3. **不出现内部说法。** 不显示「第 N 版」；不显示处理任务的状态标签；不显示「来源摘要与当前理解」这一块和它下面那两行说明；标题取不到时用「原话」。「记录于 某时间」保留。
4. **该留的留着。** 有附件时的「下载原件」；原件不可用、内容是解析文本这两条提醒（它们关系到用户能不能信这段文字）。措辞可以改得更口语，意思不能丢。
5. 很长的原文（导入的聊天记录可能有几万字）面板内部滚动，页面本身不跟着滚；390px 宽度下不溢出。

其他入口（资料库页面、回忆面板、`Marks.tsx`）打开 `SourceSheet` 时**和现在完全一样**，一个像素都不要变。它们以后并进观测台，那里可以出现内部说法。

## 你独占的文件

`web/src/components/SourceSheet.tsx`、`web/src/components/SecretaryCards.tsx`（只改打开面板的那几处调用）、`web/src/styles/app.css`（只改或新增 `.source-*` 这一组样式）。

不要动：后端代码；`web/tests/`（验收执行者 T 的）；`MemorySummary.tsx`、`LibraryPage.tsx`、`RecallSheet.tsx`、`Marks.tsx`。不新增接口：原文用现有的 `GET /v1/memory/sources/{id}`。需要动别的文件先找协调者。

## 约束

- 不加按钮、设置项和新页面。
- 界面上不出现「来源」「陈述」「版本」「依赖」「摘要」。
- 已有的浏览器测试必须照常通过。如果有哪条已有测试的预期因为这次改动必须变，先列给协调者，不要自己改测试。

## 交付

Draft PR；`npm run lint && npm run type-check && npm run build` 通过；附四张截图（模拟数据，不用线上真实内容）：桌面 1440 和手机 390 各两张，一张是短的原话，一张是很长的原文里定位到被引用的那一段。截图放进 PR 说明即可，沿用任务 C 的截图分支 `phase2/b1-C-frontend-screenshots`，不要再开新的截图分支。说明里写清：定位和标出那一段是怎么做的，找不到时的表现，以及其他三个入口没有变化是怎么确认的。
