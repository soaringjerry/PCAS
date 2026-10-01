# 第 1 阶段接口契约

本文件固定各任务之间的接口。**实现方必须按这里实现，使用方可以在对方合并前按这里写代码和 mock。**
要改契约，先在 PR 描述里提出，由用户确认后改本文件，再改代码。

---

## 1 动作记录与撤销（B1 实现；B2、D1、D2 使用）

### 1.1 表 `action_log`（迁移 `016_action_log.sql`）

| 列 | 类型 | 说明 |
|---|---|---|
| `owner_id` | uuid | |
| `id` | uuid | 动作 ID。前端命令：等于该命令的 `requestId`；秘书动作：服务端生成，等于回执的 `actionId`；worker：服务端生成 |
| `source` | text | `command` / `desk` / `worker` |
| `turn_id` | uuid，可空 | 秘书动作所属的 `desk_turns.id` |
| `summary` | text | 给人看的一句话，例如「完成：给张三回邮件」 |
| `changes` | jsonb | 见 1.2 |
| `created_at` | timestamptz | |
| `undone_at` | timestamptz，可空 | |

主键 `(owner_id, id)`。

### 1.2 `changes` 的格式

数组，按写入顺序排列。同一行在一个动作里被写入多次时，只记录第一次写入前的状态：

```json
[{ "table": "work_items" | "work_documents" | "agent_runs",
   "id": "<行 ID>",
   "before": { ...写入前的 document... } | null,
   "afterHash": "<写入后 document 的 sha256 十六进制>" | null }]
```

- `before` 为 `null` 表示这一行是本次新建的；`afterHash` 为 `null` 表示这一行被本次删除。
- **指纹算法**（2026-10-01 修订，F7）：`afterHash` 和撤销时的比对，都基于去掉 `recordVersion`、`updatedAt`、`history`、`evolution`、`sources` 之后的文档计算，这样连续撤销不会因为簿记字段的变化而误判。
- 记录方式：由 B1 在 `saveItem`、`saveDoc`、删除文档，以及写入 `agent_runs` document 的位置统一挂钩，**自动收集**同一事务里的全部写入。调用方只需在事务开始时开启收集（例如 `ctx = withActionLog(ctx, ...)`），不需要逐个命令手写 before 快照。

### 1.3 哪些命令会被记录

走 `/v1/workspace/commands` 的以下命令会写入 `action_log`，`id = requestId`：

`addTask` `addIdea` `addProject` `updateTask` `updateProject` `setTaskStatus` `renameThing` `setNotes` `moveThing` `deferTask`
`addCheck` `toggleCheck` `removeCheck` `ideaPromote` `ideaSnooze` `ideaShelve` `ideaDrop` `ideaContinue`
`addCondition` `removeCondition` `adoptRun` `discardRun` `createDoc` `updateDoc` `deleteDoc` `bulkStatus` `bulkDefer` `bulkMove`

其余命令（记忆、设置、候选、`capture`、`requestRun`、`delegateTask` 等）不记录，也不能撤销。

### 1.4 撤销命令 `undoAction`

仍然走 `/v1/workspace/commands`，信封格式照旧（`requestId`、`expectedRevision`）：

```json
{ "type": "undoAction", "id": "<动作 ID>", "requestId": "...", "expectedRevision": 12 }
```

**判定**：先检查动作是否已撤销或已过期，再检查 `changes` 涉及的行是否有尚未撤销的后续动作。有则返回 `newer_action`；没有时，对每一行检查当前 document 的业务指纹是否等于 `afterHash`（`afterHash` 为 null 的行则要求该行当前不存在），不符返回 `changed_since`。已撤销优先返回 `already_undone`，过期按 1.4.2 返回 `expired`。

**全部符合时**，按逆序恢复：
- `before == null`：删除这一行。
  - 如果是 `work_items`，而且它有 `agent_runs`：状态全是 `queued` 的，连 run 一起删除，并释放预留费用；只要有一个不是 `queued`，整个撤销失败，返回 `work_started`。
- `before != null`：把 `before` 写回。`work_items` 走 `saveAction`，summary 写成「撤销：<原 summary>」，item 的 `Version` 在当前值上加 1，不回退版本号。
- 最后把该动作的 `undone_at` 设为当前时间。撤销动作本身不写入 `action_log`，也不能再撤销。

**错误**，一律返回 HTTP 409，body 为 `{"error": code}`：

| code | 含义 | 前端文案（写进 `web/src/store/api.ts` 的 `messages`） |
|---|---|---|
| `changed_since` | 没有待撤销的后续动作记录，但业务内容已变化 | 这件事之后又改过，没法直接撤销。 |
| `work_started` | 副手已经开始做了 | 副手已经开始做了，没法撤销。 |
| `already_undone` | 已经撤销过 | 已经撤销过了。 |

动作 ID 不存在时返回 404 `not_found`。

**2026-10-01 补充（稳定化 §3）**：再细分两种情况，都返回 409：
- `newer_action`：同一对象后面还有没撤销的动作。提示「后面还有改动，请先撤销它」。
- `expired`：快照已经作废（超过 30 天，或相关资料已被删除）。提示「超过 30 天或相关资料已删除，无法撤销」。

**2026-10-01 用户确认**：同一事项从最后一次修改往回撤销。区分依据是后续动作记录，不是操作者身份：可追溯且尚未撤销的后续动作，无论来自界面、秘书或后台，均为 `newer_action`；未记录的外部业务变化才为 `changed_since`。同一轮的多个动作也必须有确定顺序；不涉及相同行的无关事项互不阻挡。

新增 sentinel error：`workspace.ErrChangedSince`、`workspace.ErrWorkStarted`、`workspace.ErrAlreadyUndone`。
B1 负责在 `internal/httpapi/server.go` 的 `fail()` 里加上这三个映射。

### 1.4.1 服务端内部调用

B1 另外导出 `(s *Store) Undo(ctx, scope, actionID string) (workspace.State, error)`，供 C2 等服务端代码直接调用。它不需要 `expectedRevision`，语义和错误都与 `undoAction` 相同。

### 1.4.2 快照的保留与删除传播（2026-09-30 补充）

`changes.before` 保存的是旧文档的完整内容。为了不让撤销变成"删了还能找回来"的后门，并控制存储占用：

- **删除传播**：记忆 / 来源删除流程（`internal/postgres/editing.go`）修改或删除了某些 `work_items` / `work_documents` 行时，凡是 `changes` 涉及这些行的 `action_log` 记录，一律把 `changes` 清成 `[]`，并写入 `expired_at`。之后对这些记录执行撤销，返回 `expired`（与已确认并实现的 F10 规则统一）。
- **保留期**：`before` 快照只保留 30 天。超过 30 天的记录同样把 `changes` 清成 `[]` 并写入 `expired_at`。清理时机：每次 `flushActionLog` 时顺带清理当前 owner 的过期记录，靠 `created_at` 索引，开销很小。
- `id`、`source`、`turn_id`、`summary`、`created_at`、`undone_at` 永久保留，第 6 阶段用作"撤销 / 保留"的训练信号。
- 迁移 016 给 `action_log` 加 `expired_at timestamptz` 列。

### 1.5 训练信号

`action_log` 就是撤销和保留的原始记录，第 1 阶段只需要存下来。转成训练样本是第 6 阶段的事，现在**不要**写入 `training_samples`。

---

## 2 秘书接口（B1 实现；B2 使用）

### 2.1 `POST /v1/desk/turn`

**同轮连续操作（2026-10-01 用户确认，F14 实现）**：用户可一句话新建任务并继续给它加步骤。模型 `actions` 按原数组顺序执行；内部引用 `N1` 指第 1 个动作成功创建的事项，`N2` 指第 2 个动作成功创建的事项，以此类推。序号从 1 开始，包含失败和解析错误的位置，不能按成功数量重排。

- 只有更早的 `create_task`、`create_idea`、`create_project` 动作，在子事务提交成功且回执为 `done` 后才绑定该别名。`delegate:new` 或 `project:new:名称` 的附带创建不产生 N 别名。
- N 别名只存在于本轮的动作引用表，不写入持久状态、不替换模型原始上下文。可用于后续动作的 `ref`、`project`、`set.project`，仍受既有事项类型与权限校验约束；`Used`、`Links`、`Show` 不接受新增的 N 别名。
- 前向、越界、非创建动作、创建失败的 N 引用均跳过依赖动作并说明原因，不能退回 THIS、旧 R 别名、标题匹配或任意 UUID。执行上限仍为原数组前 10 个动作。
- 原有 T/P/I/R/THIS 和 `delegate:new` 语义保持；尤其 R1 仍是上下文提供的已有对话事项，不是当前轮的新对象。下一轮重新建立引用表，不能沿用上一轮的 N 别名。
- 每个成功动作继续拥有独立回执与 action_log。同 requestId 重放返回保存的结果，不再次创建或加步骤；撤销遵循 1.4 的同对象逆序规则。

示例：`[{"op":"create_task","title":"交作业"},{"op":"add_steps","ref":"N1","steps":["查资料","写提纲"]}]`。

请求：

```json
{ "requestId": "uuid",
  "conversationId": "uuid" | null,
  "thingId": "uuid" | null,
  "text": "周五下午三点给张三回邮件，算在 A 项目里",
  "agentId": "model-id" }
```

- `conversationId` 由**前端生成**：开启新对话时，前端生成一个新的 UUID 直接使用，这样同一段新对话的几句话可以同时发出，不必排队等服务端返回 ID。服务端遇到还没有记录的 conversationId，就当作新对话处理；查询一律限定在当前 owner 范围内。为兼容起见，传 null 时仍由服务端生成（2026-09-30 修订）。
- `thingId` 不为空时，表示这是事项页里的秘书，该事项就是默认操作对象。
- 请求**不带** `expectedRevision`；并发安全由服务端按行的版本保证。
- `text` 去掉首尾空白后必须非空，长度不超过 4000 字符，否则返回 400 `invalid_input`。
- 同一个 `requestId`：请求体相同就返回已保存的 `turn`，加上**当前**的 `state`，不重复执行；请求体不同则返回 409 `version_conflict`。
- `desk_turns.response` **只保存 `conversationId` 和 `turn`，不保存 `state`**。State 是整个工作区的快照，存下来既占空间，又会让删除的内容留在历史里。
- **删除传播**：删除流程清空某一轮的 `question/answer` 时，同时清理 `response.turn`：`text` 和 `reply` 置空，`cards` 置为 `[]`，每条回执的 `text` 换成「（内容已删除）」，`actionId`、`op`、`status` 保留。重放这一轮时，返回的就是这个清理后的 turn。

响应 200：

```json
{ "conversationId": "uuid",
  "turn": {
    "id": "uuid",
    "text": "用户原话",
    "reply": "简短回答，纯文本，可以为空字符串",
    "cards": [ Card ],
    "receipts": [ Receipt ],
    "ask": { "question": "张三是指哪一位？", "options": ["张三（同事）", "张三（房东）"] } | null,
    "agent": "模型显示名",
    "createdAt": "RFC3339" },
  "state": { ...完整的 workspace State，与 GET /v1/workspace 相同... } }
```

**Receipt**（每个执行过或被跳过的动作一条）：

```json
{ "actionId": "uuid" | null,
  "op": "create_task" | "update" | "create_idea" | "create_project" | "add_steps" | "remember" | "delegate" | "capture",
  "text": "已建：周五 15:00 给张三回邮件 · A 项目 · 14:30 提醒",
  "thingId": "uuid" | null,
  "undoable": true,
  "undone": false,
  "status": "done" | "skipped",
  "reason": "跳过原因，status=skipped 时才有" }
```

- 回执文案由服务端根据**执行结果**生成，不直接使用模型写的文字。时间一律按用户时区显示。
- `remember` 和 `capture` 的 `undoable` 为 false，`actionId` 为 null。
- `undone` 在读取时根据 `action_log.undone_at` 实时计算：刚执行完的响应里为 false，`GET /v1/desk/turns` 和重放时反映当前是否已撤销。前端据此在刷新后直接显示「已撤销」。

**Card**，按 `kind` 区分：

```json
{ "kind": "sources",  "items": [{ "memoryId": "...", "version": 3, "text": "...", "sourceId": "...", "sourceVersion": 1, "at": "RFC3339|null" }] }
{ "kind": "links",    "items": [{ "url": "https://...", "host": "example.com" }] }
{ "kind": "timeline", "title": "去年关于成都", "items": [{ "at": "RFC3339|null", "text": "...", "status": "open|done|dropped", "memoryId": "...|null", "thingId": "...|null" }] }
{ "kind": "tasks",    "items": [{ "thingId": "...", "title": "...", "due": "RFC3339|null", "project": "名称|null", "status": "todo|doing|waiting|done|cancelled" }] }
```

- `sources` 只列回答里真正用到的记忆。`sourceId` 和 `sourceVersion` 取该记忆的第一条来源，用来打开 `SourceSheet`。
- `timeline` 由服务端在用到的记忆不少于 2 条、且带有时间时自动生成，按时间排序。
- `tasks` 在模型要求展示事项时生成，例如回答「我今天有什么事」。
- 前端遇到不认识的 `kind` 时直接忽略。

**失败时不丢话**：没有可直连的模型、模型调用失败或超过每日额度时，服务端都照 `capture` 的方式保存原话，然后照常返回 200，回执如下：

```json
{ "actionId": null, "op": "capture", "text": "已记下原话；模型暂时不可用，稍后会自动整理", "thingId": null, "undoable": false, "status": "done" }
```

`reply` 此时为空，`cards` 为空数组。

### 2.1.1 服务端内部调用

HTTP 处理函数背后是 `(s *Store) DeskTurn(ctx, scope, workspace.DeskTurnRequest) (workspace.DeskTurnResponse, error)`，C2 直接调用它。两个类型与 2.1 的 JSON 一一对应。`agentId` 为空时，使用工作区的默认 agent。

**串行与长度**（2026-10-01 补充）：同一个 `conversationId` 的轮次在服务端串行处理，后一轮能看到前一轮的结果（R* 别名）。`reply` 超过 2000 字时截断，并附上「回答太长，已截断」。

### 2.2 `GET /v1/desk/turns?conversationId=<uuid>`

返回 `{ "conversationId": "...", "turns": [ turn, ... ] }`，按时间排列，最多 50 轮，用于刷新后恢复对话。

如果某轮回答依据的记忆已被撤回或删除，该轮的 `reply` 换成「（这条回答依据的记忆已变更）」，`cards` 置为空。

### 2.3 保留不删

`/v1/desk/route`、`/v1/desk/answer`、`internal/ai/jev.go` 保留原样，第 1 阶段只是前端不再调用它们。

---

## 3 事项提醒约定（B1 写入；C1 发送；D2 展示）

- 任务上由秘书创建的提醒，是一个固定 ID 的触发器：

  ```json
  { "id": "due-reminder", "kind": "time", "description": "<标题>", "nextAt": "RFC3339 UTC", "active": true, "offset": "-30m" }
  ```

- `offset` 是 `workspace.Trigger` 和前端 `Trigger` 的新增字段（B1 在 Go 端加，D2 在前端类型加）。取值：
  - `-<N>m` / `-<N>h`：截止时间前 N 分钟 / N 小时；
  - `at`：到截止时间提醒；
  - `HH:MM`：只有日期、没有具体时间时，在截止当天的这个时刻提醒。
- 截止时间改变时，B1 按 `offset` 重算 `nextAt`；截止时间被去掉时，删除这个触发器。
- **提醒时间已经过去**（2026-10-01 补充）：按提前量算出的提醒时间已经过去、但截止时间还没到时，在截止时间提醒；截止时间也过去了，就不设提醒，回执里写明。完成后被撤销的事项，提醒时间已经过去超过 1 小时的，不补发。
- **夏令时**：本地时间不存在时（例如墨尔本夏令时开始那天的 02:30），顺延到切换后的同一时刻（03:30）。
- 提醒只在设置项 `followUps` 为 true 时触发。新 owner 的默认值已经是 true（`workspace.go` 里的默认设置），不做迁移，也不改用户已有的设置。

---

## 4 通知（C1 实现；D2 使用）

### 4.1 State 新增 `notices`

```ts
// web/src/domain/types.ts（C1）
export interface Notice {
  id: ID
  thingId: ID
  title: string        // 事项标题
  reason: string       // 触发器 description
  dueAt: string
  createdAt: string
  dismissedAt?: string
}
// State 增加：notices: Notice[]   // 最近 100 条，未关闭的排在前面
```

Go 端 `workspace.State` 增加 `Notices []Notice`，JSON 字段名为 `notices`。
迁移 017 给 `workspace_notices` 增加 `id uuid NOT NULL DEFAULT gen_random_uuid()`（唯一）、`dismissed_at timestamptz`、`delivered jsonb NOT NULL DEFAULT '{}'`。

### 4.2 接口（`internal/httpapi/notify.go`，C1）

| 方法与路径 | 请求 | 响应 |
|---|---|---|
| `GET /v1/notify/config` | | `{ "webPush": { "publicKey": "...", "subscriptions": 2 }, "telegram": { "configured": true, "chatId": "123" } }` |
| `POST /v1/notify/push-subscriptions` | `{ "endpoint": "...", "keys": { "p256dh": "...", "auth": "..." } }` | 204 |
| `DELETE /v1/notify/push-subscriptions` | `{ "endpoint": "..." }` | 204 |
| `PUT /v1/notify/telegram` | `{ "botToken": "...", "chatId": "..." }`；`botToken` 为空表示移除；`chatId` 为空时，服务端用 `getUpdates` 取最近一个私聊的 chat ID（用户需要先给 bot 发一句话） | 200 `{ "configured": true }`；保存前先发一条测试消息，失败返回 400 `invalid_input` |
| `POST /v1/notify/test` | | 200 `{ "sent": ["webpush", "telegram"] }` |
| `POST /v1/notify/notices/{id}/dismiss` | | 200，返回完整 State |

事项完成或取消后，它的提醒在 State 里仍然保留，由前端过滤掉，不再置顶。

---

## 5 副手结果自动采纳（D1 实现；D2 使用）

- run 完成且 `staleContext == false` 时，worker 在同一个事务里自动采纳，去向判断规则照搬前端现有的 `adoptAs`（`web/src/pages/ThingPage.tsx`）和 `parseChecklist`（`web/src/domain/agent.ts`）：
  - 输出里有 `- [ ] …` 这样的清单行：任务加成子任务，想法或项目建成待办；
  - `kind == "summary"`：写进进度；
  - 其他情况：存成文档。
- 采纳记录为一个 `action_log` 动作，`source = "worker"`。
- `Adoption` 增加两个字段：`actionId`（string）和 `auto`（bool）。Go 端由 D1 加，前端 `Run.adopted` 的类型由 D2 加。
- 撤销用 `undoAction { id: run.adopted.actionId }`。
- **训练样本跟着撤销**（2026-09-30 补充）：`training_samples` 也纳入动作记录（迁移 018 给它挂上同一个收集触发器；撤销的白名单加上这张表）。撤销采纳时，删除由这次采纳产生的 `adopted-result` 样本；撤销后再手动采纳，只留一条样本。撤销后 run 回到「已完成、未采纳」状态，因为 `agent_runs` 的写入也被记录了。
- 采纳时 `Doc.by = "ai"`，`summary` 写成「副手结果：<去向>」。

---

## 5.1 修改历史里的"谁"（B1 补充实现；D1、D2 使用）

`Item.history[].by`（`workspace.Revision.By`）取值固定为：

| 值 | 含义 |
|---|---|
| `user` | 用户通过界面操作，包括撤销 |
| `secretary` | 秘书执行的动作（desk turn，含 Telegram） |
| `assistant` | 副手结果的自动采纳（D1） |
| `system` | 提醒、唤醒等后台规则 |

实现方式：`withActor(ctx, "secretary")` 这类 context 值，由 `saveAction` 读取；不设置时默认为 `user`。
D2 的「动态」据此显示"你 / 秘书 / 副手"。

## 6 前端共享接口（B2 实现；D2 使用）

```ts
// useStore()（web/src/store/context.ts）新增：
dispatchUndoable(action: Action, label: string): Promise<boolean>
//   成功后弹出 toast「label」，附【撤销】，8 秒后消失；点撤销时调用 undo(requestId)。
undo(actionId: string): Promise<boolean>
//   发送 undoAction；失败时按 api.ts 的 messages 提示。

// useToast()（web/src/store/toast.ts）改为：
show(text: string, options?: { link?: { to: string; label: string }; undo?: () => Promise<unknown> }): void

// 秘书组件（web/src/components/Secretary.tsx）
<Secretary thingId?: string />
// 首页用法：<Secretary />；事项页用法：<Secretary thingId={thing.id} />
// 草稿通过 useShell().draft / setDraft 保存，key 为 thingId，首页用 'desk'

tryUndo(actionId: string): Promise<{ ok: true } | { ok: false; error: string }>
//   和 undo 相同，但把失败原因返回给调用方，不弹全局提示（B2 已实现，回执行内显示原因用）。

// 秘书组件的显示模式（2026-09-30 补充）
<Secretary thingId?: string variant?: 'full' | 'latest' />
//   'full'（默认）：完整对话。
//   'latest'：只显示最近一轮（回执、追问），上方一行「展开对话（N 轮）」，点开后临时切到完整对话。
//   D2 用法：事项页用 'latest'；首页手机宽度用 'latest'；首页桌面用 'full'。

// useShell()（web/src/store/shell.ts）新增：
prefill(key: string, text: string): void
//   把 text 写进该 key 的秘书草稿，并聚焦对应的秘书输入框。
//   D2 用它实现「点事项页的信息行 = 叫秘书改」。
onPrefill(listener: (key: string, text: string) => void): () => void
//   秘书输入框用它监听 prefill 并聚焦自己（B2 已实现）。
```
