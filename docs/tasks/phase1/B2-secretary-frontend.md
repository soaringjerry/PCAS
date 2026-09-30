# 任务 B2：秘书前端

执行者：**Opus 5.5** · 分支：`phase1/B2-secretary-frontend` · 依赖：A 已合并；与 B1 并行，按 [contracts.md](contracts.md) 第 1.4、2、6 节写代码，后端用 mock。
开工前读：[README.md](README.md)、[contracts.md](contracts.md)、[界面与交互原则](../../design/principles.md)、[白皮书](../../whitepaper.md) 第 3、11 章。

这是用户每天第一眼看到的东西，**品味比功能数量重要**。原则就三条：一句话办事、先做后报一键撤销、能可视化就不写长文。

## 要做什么

### 1 `Secretary` 组件（新文件 `web/src/components/Secretary.tsx`）

`<Secretary thingId?: string />`：一段连续的对话，首页和事项页共用。

- **输入框永远可用。** 上一轮还在等回复时，也可以继续输入并发送；各轮按发送顺序排列，等待中的那一轮显示「正在想…」。
- 回车发送，Shift+回车换行，输入法组字（`isComposing`）时不发送。
- 草稿用 `useShell().draft(key)` / `setDraft(key, …)`，key 为 `thingId ?? 'desk'`（A 已经把首页接好了，沿用即可）。
- 对话 ID 按同一个 key 存进 localStorage（例如 `pcas.secretary.<key>`，读写都包 try/catch）。刷新后用 `GET /v1/desk/turns` 恢复。点 × 或按 Esc 结束对话：清掉对话 ID，下一句开启新对话。
- **发送不丢话**：
  - 发送前生成 `requestId`，连同原话存进 localStorage；网络失败时这一轮显示【重试】，重试用**同一个** `requestId` 和同样的请求体（服务端会去重）。
  - 收到响应后删掉这条暂存记录。刷新时如果还有暂存的，显示为待重试。
- 请求体：`{ requestId, conversationId, thingId, text, agentId: agentFor('desk') }`。
- 收到响应后，用 store 的 `applyState(response.state)` 更新全局数据（见第 3 节）。

每一轮的展示（从上到下）：
1. 用户原话：一行，弱化显示。
2. `reply`：如果有，用正常字号显示。
3. `cards`：由 `SecretaryCards` 渲染（见第 2 节）。
4. `receipts`：每条一行，前面放一个状态图标。
   - `done` 状态的回执后面跟【改】【撤销】（`undoable` 为 false 时不显示撤销）。
   - 点【撤销】调用 `undo(actionId)`；成功后这行文字加删除线并显示「已撤销」，失败时显示服务端给的原因。
   - 点【改】把「改一下：<回执里事项的标题>，」填进输入框并聚焦，不打开任何表单。
   - `skipped` 状态的回执置灰，显示 `reason`。
   - 回执里有 `thingId` 时，标题做成链接，指向 `/t/<thingId>`。
5. `ask`：显示问题，`options` 显示成一排可点的小按钮，点击后把该选项作为下一句发送；用户也可以直接打字回答。

视觉上要短：没有大段文字、没有编号列表、没有「AI 助手说：」这类前缀。回执就是一行。

### 2 卡片（新文件 `web/src/components/SecretaryCards.tsx`）

按契约 2.1 渲染四种卡片，遇到不认识的 `kind` 就忽略。

| kind | 呈现 |
|---|---|
| `sources` | 引文小卡片：一行摘录加时间，点击用现有的 `SourceSheet` 打开原文 |
| `links` | 只显示域名的小链接，新窗口打开 |
| `timeline` | 竖向时间轴：左边是日期，右边是一句话；`done` 状态打勾并置灰，`dropped` 加删除线；有 `thingId` 的可以点进去 |
| `tasks` | 迷你事项行：完成圆圈、标题、截止时间、项目。点圆圈用 `dispatchUndoable(setTaskStatus done, '做完了')` |

样式沿用 `web/src/styles/tokens.css` 的变量。放在 `hall.css` 或新建 `secretary.css` 都可以，二选一，不要两边都放。浅色、深色、手机宽度都要检查。

### 3 Store 与 toast（按契约第 6 节）

- `web/src/store/context.ts`、`web/src/store/StoreProvider.tsx`：
  - 新增 `applyState(state)`：复用现有的 `accept`，只接受 revision 不低于当前的状态。
  - 新增 `dispatchUndoable(action, label)`、`undo(actionId)`。`dispatchUndoable` 要拿到这次命令的 `requestId`（现在它在 `command()` 内部生成），可以把生成挪到外面，或者让 `command()` 返回它。
- `web/src/store/actions.ts`：加 `{ type: 'undoAction'; id: string }`。
- `web/src/store/toast.ts` 和 `web/src/components/Shell.tsx`（toast 在这里渲染）：`show(text, { link?, undo? })`；带 `undo` 时显示【撤销】，8 秒后消失；鼠标悬停时暂停计时。
- `web/src/store/api.ts`：`messages` 里加 `changed_since`、`work_started`、`already_undone` 三条文案（见契约 1.4）。
- `web/src/store/shell.ts` 和 `Shell.tsx`：加 `prefill(key, text)`，按契约第 6 节实现。`Secretary` 订阅它，被 prefill 时聚焦自己的输入框。

### 4 换掉旧导办台

- `web/src/pages/HallPage.tsx` 的 `Desk`：输入框、`route()`、`PickCard`、`AnswerCard`、「…」更多菜单、「翻完整历史」、receipt 提示、pending 委派恢复，**全部换成 `<Secretary />`**。`DecisionStrip` 原样保留在它上方（D2 会改它）。
- 删除 `web/src/store/pendingDelegations.ts`，以及 `web/src/domain/hall.ts` 里的 `looksLikeQuestion`、`looksLikeRequest`、`ambiguousDelegation`（如果它们还被别的地方用到，先确认没有了再删）。
- 前端不再调用 `/v1/desk/route` 和 `/v1/desk/answer`，后端保留不动。
- `web/src/pages/ThingPage.tsx`：**只把 `Composer` 替换成 `<Secretary thingId={thing.id} />`，并删除 `Composer` 函数**。快捷按钮、「带上 N 条记忆」、「交给谁」都随 `Composer` 一起消失。文件的其他部分归 D2，不要动。

### 5 测试

- 删除 `web/tests/desk-routing.spec.ts` 和 `web/tests/hall-ux.spec.ts`。它们测的是被删掉的旧流程；里面仍然有意义的场景（比如续问时带上服务端的 turn ID）改写进新文件。
- 新建 `web/tests/secretary.spec.ts`，用 `page.route` mock `/v1/workspace`、`/v1/desk/turn`、`/v1/desk/turns`、`/v1/workspace/commands`：
  - [ ] 发一句话 → 出现回执 → 刷新 → 对话从 `GET /v1/desk/turns` 恢复。
  - [ ] 第一轮还在等待时发出第二句：两个请求带同一个 `conversationId`，输入框始终可用。
  - [ ] 点回执的【撤销】：发出 `undoAction`，`id` 等于回执的 `actionId`；这行显示「已撤销」。mock 返回 409 `changed_since` 时显示对应文案。
  - [ ] `ask.options` 点击后作为下一句发送。
  - [ ] 第一次请求网络失败 → 显示【重试】，原话还在 → 重试时 `requestId` 与第一次相同。
  - [ ] 事项页的秘书发送的请求里，`thingId` 等于该事项的 ID。
  - [ ] 四种卡片都能渲染；遇到未知 `kind` 不报错。
  - [ ] `dispatchUndoable` 的 toast：点【撤销】发出 `undoAction`，`id` 等于原命令的 `requestId`。

## 不要做

- 不要改 `ThingPage.tsx` 里 `Composer` 以外的部分，也不要改 `HallPage.tsx` 的 `TodayWall`、`DecisionStrip`、`IdeaWall`（D2 负责）。
- 不要改 Go 代码；契约不够用时在 PR 里提出。
- 不要改 `web/src/domain/types.ts`；秘书相关的类型放在新文件 `web/src/domain/desk.ts`。

## 验收

- [ ] 第 5 节的用例全部通过；`npm run lint && npm run type-check && npm run build` 通过。
- [ ] B1 合并后（如果那时你还在），在临时实例上用假模型手动走一遍：发一句带时间的话 → 回执 → 撤销 → 事项消失。
- [ ] PR 附上首页和事项页在桌面浅色、桌面深色、手机宽度三种情况下的截图。
