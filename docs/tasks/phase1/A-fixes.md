# 任务 A：修三个硬伤

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者：**6.1 Sol** · 分支：`phase1/A-fixes` · 依赖：无 · 后续任务都在 A 合并后开始，所以请**尽快、只做本文件列出的内容**。

开工前读：[README.md](README.md)（规则、环境、文件归属）。

## 1 任务说明修改不保存

**现象**：在事项页修改任务或想法的「说明」，失焦后没有保存，刷新就丢。项目的说明（目标）正常。

**根因**：`web/src/pages/ThingPage.tsx` 的 `Header` 里，`notes` 文本框的 `onBlur` 有一个悬空的 `else`：

```ts
if (thing.kind === 'project') if (!(await dispatch({ type: 'updateProject', ... }))) return
else if (!(await dispatch({ type: 'setNotes', ... }))) return
```

这个 `else` 绑定到了内层的 `if`，所以任务和想法根本不会触发 `setNotes`。

**修复**：改成带花括号的显式 `if / else`：项目调用 `updateProject { goal }`，其他调用 `setNotes`。同一函数里标题的 `onBlur` 也有相同的单行嵌套 if 写法，一并改成带花括号的写法（它目前的行为是对的，只是容易让人误读）。

## 2 首页导办台草稿刷新会丢

**现象**：首页导办台里输入到一半，刷新页面，内容就没了。事项页底部输入框的草稿是能保留的。

**修复**：`web/src/pages/HallPage.tsx` 的 `Desk` 组件里，把输入框的值从组件内部的 `text` state 改为读写 `useShell().draft('desk')` 和 `setDraft('desk', …)`（实现在 `web/src/components/Shell.tsx`，已经持久化到 localStorage）。

注意：
- 这个组件还有一套「恢复未确认的委派」逻辑（`pendingDelegations`、`recovering`、`draftEdited`），会往输入框里回填问题文本。这套逻辑**保持原有行为**，只把存储位置换到 `draft('desk')`。任务 B2 会整体删掉它，这里不要重构。
- 所有原来调用 `setText('')` 清空输入框的地方，都改成 `setDraft('desk', '')`。

## 3 副手失败后不能重试

**现象**：事项页的副手卡片失败时，只显示「没做成。<错误原文>」，没有任何可以操作的按钮。

**修复**：`web/src/pages/ThingPage.tsx` 的 `RunCard`，在 `run.status === 'failed'` 分支里：
- 文案改成「没做成：<人话原因>」。新增一个小函数 `failureText(run)`：错误里含 `budget` 时显示「超过今天的额度」；含 `timeout`、`deadline` 时显示「等太久没回应」；含 `unavailable`、`not configured` 时显示「这个副手现在连不上」；其他情况显示「出了点问题」。原始错误放在 `title` 属性里，鼠标悬停可以看到。
- 加一个【重试】按钮：调用 `runAgent({ thingId: run.thingId, agentId: run.agentId, kind: run.kind, prompt: run.prompt })`。
- 如果当前事项默认的副手（`useShell().agentFor(thing.id)`）和 `run.agentId` 不同，并且这个副手已启用，再加一个【换 <名字> 重试】按钮。
- 失败卡片保留，不自动隐藏，新发起的 run 会作为一张新卡片出现。

## 不要做

- 不要加撤销（B1、B2 负责）。
- 不要改导办台的路由逻辑、Jev、问答卡片（B2 负责）。
- 不要动 Go 代码。

## 验收

- [ ] 在任务的说明里输入文字后点击别处，刷新页面，文字还在；对想法和项目重复这一步，同样还在。
- [ ] 在首页导办台输入「测试草稿」，不提交，刷新页面，文字还在；提交后输入框被清空，再刷新仍然是空的。
- [ ] 用 Playwright 构造一个 `status: 'failed'` 的 run（mock 写法参考 `web/tests/hall-ux.spec.ts`），能看到【重试】按钮；点击后发出的 `requestRun` 请求，`prompt` 和 `agentId` 都和原 run 相同。
- [ ] 以上三条写进 `web/tests/fixes.spec.ts`，用 mock API，不依赖真实后端。
- [ ] `npm run lint && npm run type-check && npm run build` 通过。
