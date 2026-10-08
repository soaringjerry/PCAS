# 任务 D2：删按钮

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者：**Opus 5.5** · 分支：`phase1/D2-buttons` · 依赖：**B2 和 C1 已合并**；D1 按契约第 5 节并行，用 mock 数据开发
开工前读：[README.md](README.md)、[contracts.md](contracts.md) 第 3、4、5、6 节、[界面与交互原则](../../design/principles.md) 中的「按钮只剩三种」和「谁来做决定」。

**规则**：日常界面的按钮只剩三种：① 完成打勾 ② 撤销 / 改 ③ 往外发送、删除、花钱之前的确认。
- 可以撤销的删除，用 toast 撤销代替确认弹窗；不可撤销的删除才弹确认。
- 其余的决定，交给秘书（用户说一句）或者系统（自动判断）。
- 输入框和点进去就能改的文字不算按钮。

开工前先数一下「典型任务页」「典型项目页」「首页」三个页面各有多少个可点控件，完工后再数一次，两次的数字写进 PR 描述。

## 事项页结构（2026-09-30 修订，优先于下表）

**问题**："发生过什么"分散在三处：「工作记录」（副手结果、文档）、底部的秘书对话卡片、折叠的「来龙去脉」。用户分不清该去哪里看。

**新的页面顺序**：

```
‹ 所属项目
○ 标题                                    ← 完成圆圈；标题可以直接编辑
待办 · 周五 15:00 截止 · A 项目 · 14:30 提醒 ← 只读信息行，点击 = prefill 秘书
说明（直接编辑）
子任务（圆圈 + 加一步）
文档                                      ← 产出物单独成块，点开就能改；没有文档时整块隐藏
动态                                      ← 唯一的"发生了什么"，按时间倒序
<Secretary thingId variant="latest" />    ← 只显示最近一轮，贴在输入框上方
```

- **「动态」合并三处来源**：
  - `item.history`：显示 `by`（契约 5.1）对应的"你 / 秘书 / 副手"，加上 summary 和时间；
  - 这件事的 runs：已自动采纳的显示一行加【撤销】【看看】，其他状态沿用下表；
  - 想法的 `evolution`。
  删除原来的「工作记录」和「来龙去脉」两个区块。每条一行；超过 10 条时折叠成「更早的 N 条」。
- **「文档」** 从记录流里拿出来单独成块，每篇一行：标题、谁写的、更新时间。点开后原位展开编辑（规则见下表 DocCard）。「写点什么…」放在这一块的末尾。
- 空状态文案改成「对秘书说一句，结果会出现在这里」，不再提"让副手做点什么"。

## 首页布局（2026-09-30 修订）

- **桌面**：中间一栏基本全部给秘书对话（`variant="full"`），对话区自然撑满可用高度。
  - `DecisionStrip` 没有内容时整条隐藏。
  - 「你不在的时候，我做了这些」没有内容时整块隐藏；有内容时收成一行「你不在的时候：N 件」，点击展开，放在对话下方。
- **手机**：从上到下依次是「今天」精简版（到点了 + 在等你 + 今天的时间线，最多 5 条，多出的显示「还有 N 件」）→ `<Secretary variant="latest" />` → 固定在底部的输入框。项目和想法排在「今天」之后。
- **「今天」时间线**：今天到期、已经过点的事也放进"按时间"时间线里，显示为灰色，排在「现在」线之前。不能再出现「在等你」里列着今天的事，时间线却写着"今天没有定了时间的事"。

## 事项页 `web/src/pages/ThingPage.tsx`

B2 已经把底部换成了 `<Secretary thingId>`。本文件其余部分归你。

| 位置 | 改成 |
|---|---|
| 标题 | 任务的标题前加一个完成圆圈，点击用 `dispatchUndoable(setTaskStatus done, '做完了')`；已完成的任务再点就恢复为 todo。标题仍然可以直接编辑 |
| `Meta`：状态、截止、项目三个下拉框和「XX 在等你 ×」 | 换成一行只读的信息：`进行中 · 周五 15:00 截止 · A 项目 · 14:30 提醒 · 张三在等你`，没有的部分省略。提醒时间取 `due-reminder` 触发器（契约第 3 节）。**点这一行就调用 `useShell().prefill(thing.id, '改一下这件事：')`**，由秘书处理修改 |
| 想法的 `Meta` | 同样改成只读一行，包括状态和项目 |
| 项目的 `Meta` | 同样改成只读一行，包括状态、进度最后一行、未完成数 |
| 说明文本框 | 保留（A 已修好保存问题） |
| `RetainedWriting` | 保留，但收进「来龙去脉」里，不在页面顶部显示 |
| 子任务 | 保留完成圆圈和「加一步」输入框。删除用 × 加撤销 toast，不弹确认 |
| `ProjectItems` | 保留列表和「加一件事」输入框 |
| `IdeaBanner` 被唤醒时 | 显示原因，下面两个快捷回复「转成待办」「再放放」，执行后弹撤销 toast |
| `IdeaBanner` 放着时 | 条件改成只读列表，每条后面 × 删除（配撤销 toast）；删除「再加一个条件」表单，条件由用户对秘书说 |
| `RunCard` 已被自动采纳 | 一行：`<副手名> · 已加入 5 个子任务` 或 `· 存成了文档` 或 `· 写进了进度`，后面跟【撤销】【看看】。撤销调用 `undo(run.adopted.actionId)`，看看就是展开原文 |
| `RunCard` 已完成但未采纳（被撤销过，或依据变了） | 输出折叠显示，点击展开。依据没变：【放回去】（手动 `adoptRun`，去向用现有的 `adoptAs`）。依据变了：提示「依据变了」加【重做】（用同样的 prompt 调 `runAgent`）。删除「改一下」「不要」「依据 N 条记忆」 |
| `RunCard` 失败 | 保留 A 做的重试，但要补上：请求提交中禁用按钮，防止连点发起多次付费请求。「放回去」「重做」按钮同理 |
| `RunCard` 等待手动交接 | 保留「复制给它的内容」、粘贴框和「放回来」。这个流程本身就要人手动操作；「算了」改成 × 加撤销 toast |
| `DocCard` | 点正文直接进入编辑，失焦自动保存（`updateDoc`），删掉「编辑 / 存好 / 取消」。删除放进一个很小的「…」菜单，删除后弹撤销 toast，不再用 ConfirmModal（B1 已经让 `deleteDoc` 可以撤销） |
| 「写文档」 | 删掉。工作记录最后放一行弱化的「写点什么…」，点击就在原位新建一篇空文档并进入编辑 |

## 首页 `web/src/pages/HallPage.tsx`（`TodayWall`、`DecisionStrip`、`IdeaWall`）

- `TaskRow` 的完成圆圈改用 `dispatchUndoable(..., '做完了')`。
- **到点的提醒置顶**：在 `TodayWall` 最上面加一组「到点了」，数据来自 `state.notices`（契约 4.1）。
  - 显示条件：`dismissedAt` 为空，并且对应事项还没完成或取消；按 `dueAt` 排序。
  - 每行有完成圆圈、标题和到点时间；右侧 × 调用 `POST /v1/notify/notices/{id}/dismiss`（这是关闭，不算删除，不需要确认）。
  - 这一组用温和的强调色，不用红色。
- **`DecisionStrip`**：
  - 只放两类：副手结果依据变了、需要重做的 run；等待手动交接的 run。
  - 删除「拿不准」这一类和「逐条确认」入口，`UnsureSheet` 不再从首页打开（组件文件保留，观测台以后会用）。
  - 没有待办时这一条整个隐藏，不再显示「没有要你拍板的事」。
- `IdeaWall` 被唤醒的想法：两个按钮改成执行后弹撤销 toast。

## 清理没有调用方的代码

B2 去掉 `Composer` 之后，下面这些代码已经没人调用了，由你删除（删之前先 grep 确认确实没有调用方）：
- `web/src/domain/agent.ts` 里的 `quickActions`、`contextFor`、`buildBrief`
- `web/src/domain/lines.ts` 里的 `estimateCost`
- `web/src/store/actions.ts` 里的 `toggleContextMemory` 动作类型（后端命令保留）

## 类型 `web/src/domain/types.ts`

- `Trigger` 加 `offset?: string`。
- `Run.adopted` 加 `actionId?: string`、`auto?: boolean`。

## 不要做

- 不要改 `Secretary` 组件和 store（B2 负责）；缺什么接口，在 PR 里提出来。
- 不要改 Go 代码。

## 验收

- [ ] 新建 `web/tests/buttons.spec.ts`，用 mock API，覆盖：
  - 任务页点信息行 → 秘书输入框里出现「改一下这件事：」，并且获得焦点；
  - 完成圆圈 → toast → 撤销；
  - 自动采纳的 run 显示一行加撤销，撤销后出现「放回去」；
  - 文档失焦自动保存；
  - `notices` 置顶显示，点 × 发出 dismiss 请求；
  - 首页没有「拿不准」入口。
- [ ] PR 描述里写明三个页面控件数量的前后对比，外加桌面浅色、深色和手机宽度的截图。
- [ ] `npm run lint && npm run type-check && npm run build` 通过，已有的 Playwright 用例仍然通过。
