# 任务 B1：撤销基础设施 + 秘书后端

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者：**6.1 Sol** · 分支：`phase1/B1-secretary-backend` · 依赖：A 已合并
开工前读：[README.md](README.md)、[contracts.md](contracts.md) 第 1、2、3 节、[白皮书](../../whitepaper.md) 第 3、10 章。

B2（前端）会按 contracts.md 和你并行开发。**接口必须和契约一字不差**；需要改契约，先在 PR 里提出。

建议分三次提交，按顺序做，每一步都可以单独测试：**① 撤销 → ② 提醒约定 → ③ 秘书接口**。

---

## ① 动作记录与撤销

实现 contracts.md 第 1 节。

1. **迁移** `internal/postgres/migrations/016_action_log.sql`：
   - 新建 `action_log` 表，列定义见契约 1.1；加索引 `(owner_id, created_at DESC)`。
   - 给 `desk_turns` 表加列：`conversation_id uuid`、`thing_id uuid`、`request_id uuid`、`request_hash bytea`、`response jsonb`、`created_at timestamptz DEFAULT now()`（如果已有 `created_at` 就不用加）。
   - 在 `(owner_id, request_id)` 上建唯一索引，条件为 `request_id IS NOT NULL`；在 `(owner_id, conversation_id, created_at)` 上建普通索引。
   - 已有的行保持原样，新列允许为空。
2. **收集器**：新文件 `internal/postgres/actions_log.go`。
   - 用 `context` 挂一个收集器：`withActionLog(ctx, id, source, turnID, summary)`。
   - 在 `saveItem`、`saveDoc`、删除 `work_documents` 的位置、写入 `agent_runs.document` 的位置各加一个钩子：写入前读出旧 document；同一行只保留第一次的 before；写入后计算新 document 的 sha256。
   - 事务结束前调用 `flushActionLog(ctx, tx)` 写入一行。如果一次动作没有产生任何改动，就不写。
   - 没有挂收集器的写入（后台任务等）保持原样，不记录。
3. **接入 Execute**：`internal/postgres/workspace.go` 的 `Execute` 中，对契约 1.3 列出的命令，在调用 `commandTx` 前挂上收集器，`id = in.RequestID`，`source = "command"`。`summary` 用一个小函数生成，例如「完成：<标题>」「改了截止时间：<标题>」，写不出更具体的就用「修改：<标题>」。
4. **`undoAction` 命令**：在 `commandTx` 里加一个 case，按契约 1.4 实现，包括三种新错误和 404。另外导出 `Store.Undo`，见契约 1.4.1，内部复用同一套逻辑。
   - 删除一个新建的 `work_items` 行时：`project_id` 自引用、`agent_runs`（没有级联删除）以及各种 ON DELETE CASCADE 的表都要处理对；删除前先检查 queued runs 并释放它们的预留费用（照 `runs.go` 里现有的释放写法）。
   - 删除后要确认快照里不再出现它，也不能留下孤儿行。
5. **错误映射**：`internal/workspace/model.go` 加三个 sentinel error；`internal/httpapi/server.go` 的 `fail()` 加上映射，都是 409，code 见契约。

## ② 提醒约定

实现 contracts.md 第 3 节。

- `workspace.Trigger` 加字段 `Offset string \`json:"offset,omitempty"\``。
- 写一个函数 `applyDueReminder(item *workspace.Item, remind string, loc *time.Location)`：按截止时间和 `remind` 值创建、更新或删除 ID 为 `due-reminder` 的触发器。`remind` 的取值是 `"-30m"`、`"-2h"`、`"at"`、`"HH:MM"`、`"none"`，或者空字符串（表示沿用原有的 offset）。
- `updateTask` 的 patch 修改了 `due` 时：已有 `due-reminder` 的，按它的 offset 重算 `nextAt`；`due` 被清空的，删除这个触发器。
- 时区取 `workspace.Settings.Timezone`，加载失败就用 UTC。

## ③ 秘书接口

实现 contracts.md 第 2 节：`POST /v1/desk/turn` 和 `GET /v1/desk/turns`。

### 文件
- `internal/postgres/desk_turn.go`：`DeskTurn(ctx, scope, req)` 和 `DeskTurns(ctx, scope, conversationID)`。它们也会被 C2 在服务端直接调用（契约 2.1.1），不要把逻辑写在 HTTP 处理函数里。`agentId` 为空时使用默认 agent。
- `internal/postgres/desk_actions.go`：动作的校验、执行和回执文案。
- `internal/workspace/desk.go`：请求和响应的类型，JSON 结构照抄契约。
- `internal/httpapi/workspace.go`：注册两个路由。和 `/v1/desk/answer` 一样，把写超时放宽到 2 分钟。

### 上下文（复用现有代码，不要重写）
从 `internal/postgres/desk.go` 的 `AnswerDesk` 里抽出共用函数，例如 `deskContextTx`。**`AnswerDesk` 改为调用它，行为和现有测试保持不变。**

复用的部分：
- 现在时间和星期、时区、城市这几行；
- `memoriesTx` 的可见性过滤、`Recall`、`sanitizeItemTx`；
- 调用模型前后两次 `verifyRunTx` 和 `checkContext`。

新增的部分（给模型看的全部用短别名，服务端保存别名到真实 ID 的映射）：

| 别名 | 内容 | 上限 |
|---|---|---|
| `T1…` | 未完成任务：标题、状态、截止时间、所属项目 | 40 条 |
| `P1…` | 活跃项目：名称、未完成数 | 全部，最多 50 |
| `I1…` | 最近的想法：标题、状态 | 20 条 |
| `R1…` | 本对话里秘书建过或改过的事项（从 `action_log.turn_id` 查） | 20 条 |
| `THIS` | `thingId` 对应的事项：标题、状态、截止、说明、子任务 | 1 条 |

另外还要给模型：本对话最近 6 轮（原话、回答、回执文案）和召回的记忆（沿用现有格式）。

模型永远看不到真实 UUID，也不能写入真实 UUID。

### 提示词缓存（省钱）

秘书每句话都要调用一次模型，前缀缓存能不能命中，直接决定成本。这个做法借鉴自 Hermes，见 [研究笔记](../../research/hermes-agent.md)。

- 系统指令（`assistantInstructions` 加 `secretaryInstructions`）必须是**固定字符串**：不能拼入时间、别名、用户数据。
- user prompt 按「越稳定越靠前」的顺序排列：城市和时区 → 项目列表 → 未完成任务 → 想法 → 本对话历史 → 召回的记忆 → `THIS` → 现在时间 → 这句话。
- 同一个列表在两轮之间要保持稳定的排序（按创建时间，不按更新时间），这样才能共享前缀。
- 如果 provider 返回了缓存命中的 token 数，把它记进现有的用量记录里（看 `internal/ai` 有没有现成字段；没有就先不做，在 PR 里说明）。

### 模型输出（服务端内部格式，前端看不到）
用 `s.models.GenerateWithSearch`，要求模型只输出如下 JSON：

```json
{ "reply": "简短回答，纯文本，没有要回答的就给空字符串",
  "used": ["记录ID"], "links": ["https://..."], "show": ["T1"],
  "remember": false,
  "actions": [
    {"op":"create_task","title":"…","due":"2026-10-03T15:00 或 2026-10-03 或 null","remind":"-30m|-2h|at|HH:MM|none|null","project":"P1|new:名称|null","notes":null,"owedTo":null,"waitingFor":null},
    {"op":"update","ref":"T3|I2|P1|R1|THIS","set":{"title":"…","due":"…或空字符串表示去掉","remind":"…","project":"P1|none","status":"todo|doing|waiting|done|cancelled","notesAppend":"…"}},
    {"op":"create_idea","title":"…","condition":"…|null","conditionDue":"…|null","project":"P1|null"},
    {"op":"create_project","name":"…"},
    {"op":"add_steps","ref":"T3|THIS|R1","steps":["…"]},
    {"op":"delegate","ref":"T3|THIS|R1|new","title":"ref 为 new 时必填","kind":"plan|draft|breakdown|summary|ask","prompt":"…"}
  ],
  "ask": {"question":"…","options":["…"]} }
```

`ask` 在不需要追问时为 null。

指令（写成常量 `secretaryInstructions`，在 `assistantInstructions` 之后追加；以下是要点，措辞可以调整）：
- 你是用户的前台秘书。理解整句话：该回答的回答，该办的事直接用 actions 办掉，一句话里可以有多个动作。
- 相对时间（明天、周五、下周一）按给出的「现在」和时区换算成本地时间，格式为 `YYYY-MM-DDTHH:MM`；只说了日期就只写日期。说了时间就设提醒；没说怎么提醒时 `remind` 填 null，由系统用默认值。
- 项目按名称和意思匹配已有的 `P*`；只有用户明确说要新建项目时才用 `new:名称`。
- 用户在修改刚才的安排时（比如「改到周一」「不对，是 A 项目」），用 `update` 引用 `R*` 或 `T*`，不要新建。
- 只有真正有歧义、而且会影响结果的地方才填 `ask`；其他动作照常执行。
- `delegate` 只在用户明确要求产出东西时使用（写方案、起草、查资料、拆步骤）。
- 用户说的是事实、偏好或决定（比如「我不吃香菜」）时，把 `remember` 设为 true。
- `reply` 要简短，像当面回话，不要列 1. 2. 3.；需要展示事项清单就用 `show`，需要展示依据就用 `used`。
- 搜索词的约束沿用现有的 `deskInstructions`：不能把私人信息放进搜索词。

### 校验与执行
所有动作在**一个事务**里执行，但每个动作单独挂一个收集器，写成单独的一行 `action_log`（`source = "desk"`，`turn_id` 为本轮的 ID），这样每条回执都能单独撤销。

| 规则 | 不满足时 |
|---|---|
| 一轮最多 10 个动作 | 多出的写一条 skipped 回执：「一次太多了，只做了前 10 件」 |
| `ref` 必须在别名表里 | skipped：「找不到要改的那件事」 |
| `due` 能按用户时区解析，且不早于一年前 | 该字段忽略，回执里加注「时间没看懂」 |
| 只有日期的 `due` | 存为当地 23:59；`remind` 为 null 时默认 `09:00` |
| 带时间的 `due` 且 `remind` 为 null | 默认 `-30m` |
| `status` 必须在白名单内 | 该字段忽略 |
| `new:名称` 与已有项目同名（忽略大小写和首尾空格） | 改用已有项目 |
| `title` 为空或超过 200 字 | skipped |

动作到命令的映射（全部通过现有的 `s.commandTx` 执行，不要绕开）：

| 动作 | 命令 |
|---|---|
| `create_task` | `addTask`，之后按需 `updateTask`（due、owedTo、waitingFor），然后 `applyDueReminder` |
| `update` | `renameThing`、`updateTask` / `updateProject`、`setTaskStatus`、`moveThing`、`setNotes`（追加），按需组合 |
| `create_idea` | `addIdea`，有条件时再 `addCondition` |
| `create_project` | `addProject` |
| `add_steps` | 每步一个 `addCheck` |
| `delegate` | `ref = new` 时用 `delegateTask`；否则用 `requestRun`。受现有预算约束；超出额度时 skipped：「超过今天的额度」 |

原话以 `connector = "desk"`、`ExternalID = requestId` 通过 `ingestTx` 保存下来，不生成候选。
`internal/postgres/processing.go` 的抽取流程里，`connector == "desk"` 的来源**跳过 task 和 idea 类条目**（秘书已经处理过），记忆类条目照常处理。这是本任务对 `processing.go` 唯一的改动。

`remember == true` 时追加一条回执：`op = "remember"`，文案「记下了，会整理进记忆」，`undoable = false`。

### 回执文案（服务端生成，用户时区）
- `create_task`：`已建：周五 15:00 给张三回邮件 · A 项目 · 14:30 提醒`，没有的部分省略
- `update`：`已改：给张三回邮件 → 下周一 10:00`，或 `已完成：给张三回邮件`
- `create_idea`：`记成想法：做个记账小工具`（有条件时加「 · 等成本降下来」）
- `create_project`：`新建项目：A`
- `add_steps`：`给「…」加了 3 步`
- `delegate`：`交给 <副手名>：写方案`

日期格式：今天和明天写「今天」「明天」，本周内写「周五」，更远的写「10 月 3 日」，时间一律用 24 小时制。

### 卡片
- `sources`：从 `used` 生成。只允许引用实际发给模型的记忆，写法照 `AnswerDesk` 现有的做法。
- `links`：只接受 http 或 https、不带用户信息的链接，最多 3 个，照 `AnswerDesk` 现有的做法。
- `timeline`：`used` 里带时间的记忆不少于 2 条时生成，按时间排序；`status` 取该记忆关联事项的状态，没有关联事项就填 `open`。
- `tasks`：从 `show` 生成。

### 失败回退
下列情况都保存原话（沿用现有 `capture` 命令）并返回 200，回执照契约：
- 没有可直连的模型、agent 是 manual、模型调用失败、JSON 解析失败；
- 超出每日额度（`reserveModelCost` 返回预算错误）。

原则是**任何情况下都不能丢掉用户说的话**。

### 持久化与幂等
- 每一轮写入 `desk_turns`：原有列照常写，新列写入 conversation、thing、request、hash 和完整响应。
- 同一个 `requestId`：请求体哈希相同就直接返回保存的响应；不同就返回 409。
- `GET /v1/desk/turns`：按对话 ID 查询，对每一轮的 `dependencies` 做 `verifyRunTx`，校验失败的按契约 2.2 替换掉回答内容。

---

## 不要做

- 不要删除或修改 `/v1/desk/route`、`/v1/desk/answer`、`jev.go` 的行为。
- 不要改前端。
- 不要写入 `training_samples`。
- 不要实现通知发送（C1 负责），也不要做 run 完成后的自动采纳（D1 负责）。

## 验收（集成测试，新文件 `internal/postgres/desk_turn_test.go` 和 `internal/postgres/undo_test.go`，用 httptest 假模型）

- [ ] 时区设为 Asia/Shanghai，假模型返回 `create_task`（due 为 `2026-10-02T15:00`，project 为 `P1`，remind 为 null）：任务的 due、项目都正确，`due-reminder` 的 `nextAt` 为当地 14:30 对应的 UTC 时间；回执文案正确。
- [ ] 一轮里同时有 `reply` 和 2 个动作：回答和两个任务都在，两条回执各自带不同的 `actionId`。
- [ ] 第二轮用 `R1` 发出 `update`（改 due）：更新的是同一个任务，提醒时间也跟着变。
- [ ] 伪造的别名、真实 UUID 当作 ref、11 个动作、非法 status：都被跳过或忽略，其余动作正常执行。
- [ ] 撤销：新建 → 事项消失；修改 → 恢复原值；修改后又改过 → `changed_since`；重复撤销 → `already_undone`；已经开始执行的 delegate → `work_started`。
- [ ] 普通命令（`setTaskStatus` done）走 `Execute` 之后，用 `undoAction { id: 该命令的 requestId }` 能恢复原状态。
- [ ] 同一个 `requestId` 请求两次，不会重复建任务；换了请求体则返回 409。
- [ ] 假模型返回 500 或非法 JSON：响应 200，只有一条 `capture` 回执，原话已作为资料保存。
- [ ] 被设为不可见的记忆不出现在 prompt 里（照 `desk_test.go` 现有的断言写法）。
- [ ] `connector = "desk"` 的来源经过抽取后，不产生 task 或 idea 候选。
- [ ] 通过 `updateTask` 改 due 会重算提醒；去掉 due 会删除提醒。
- [ ] `GET /v1/desk/turns` 按顺序返回；删除某轮用到的记忆后，该轮的回答被替换。
- [ ] 用同一个假模型连续调用两轮，两次的系统指令字节完全相同；两次 user prompt 从开头到「本对话历史」之前的部分也完全相同。
- [ ] 原有的 `desk_test.go`、`ux_regressions_test.go` 全部仍然通过。
- [ ] `make check` 和 `make test-integration` 通过。
