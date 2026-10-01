# 任务 A：原话供给、依赖校验、依据标记（后端）

执行者 6.1 Sol / high。分支 `phase2/b1-A-raw-text`，工作区 `/root/PCAS-wt/b1-A`，PR base `phase2/batch1`。

先读 [本批总览与契约](README.md)。本任务实现其中的 R1–R8。规则、数据形状、文件归属以总览为准；这里只说做法和要注意的地方。

## 要交付什么

秘书、副手、手动转交在组装上下文时，把检索命中的原话摘录写进提示词，记进依赖，并让依据卡片能指回原话。同时把「依据变了」从「替换掉回答」改成「保留回答并标记」。

## 你独占的文件

`internal/memory/contracts.go`（只加 Scope 的内部标记）、`internal/postgres/retrieval.go`、`desk_turn.go`、`run_context.go`、`runs.go`、`telegram_turn.go`、`artifacts.go`（只改 `sanitizeItemTx` 里校验运行依赖的那条查询）、`internal/workspace/desk.go`；R7 需要时可以动 `internal/telegram/`。

不要动：`actions_log.go`、`processing.go`、`undone_turns.go`、迁移文件（B 的）；`web/`（C 的）；任何测试文件（T 的）。

## 做法

### 1 让检索返回原话（R1、R2）

- `memory.Scope` 加一个内部标记（建议名 `Team bool`，标签 `json:"-"`）。它不能从任何请求里解码出来。
- `recallTx` 里现在的授权条件是「本人，或者有 `record_grants`」。加上：标记为真且记录是原话（`kind='source'`）时也允许。陈述仍然只看原来的条件。`linked`、`pending`、`evidenceTx` 里同类的条件按同样的方式处理原话。
- 三个入口调用 `Recall` 时设置这个标记：`secretaryPrompt`（`desk_turn.go`）、`prepareRunContext`（`run_context.go`）。`AnswerDesk`（`desk.go`，旧接口）不动。HTTP 层的 `recall`、`expand`、`getSource` 不设置。
- R2 的排除条件放在挑选摘录的地方：来源类型、有没有可读文本、是不是当前对话已出现的提问、是不是「整轮都被撤销」那一轮的原话（调用 B 提供的 `deskTurnFullyUndoneTx`，参数是这份原话的 `external_id`；只对 `desk` 类型的原话需要查）。

### 2 取摘录（R3）

`Recall` 现在把每条命中的文本拼进 `Summary` 一个大字符串，调用方拿不到「哪条记录对应哪段文字」。给 `RecallResult` 加一个按记录给出的摘录列表（建议 `Excerpts []RecallExcerpt`，含记录引用、摘录文本、标题、来源类型、表达时间、记录时间、角色），在 `recallTx` 里组装 `Summary` 的同一个循环里填。不要让调用方去解析 `Summary`，也不要为每条命中再查一遍库取全文。

截断规则按 R3。字符数按 Unicode 字符算（`[]rune`），不按字节。

### 3 写进提示词，记进依赖（R4、R5）

- 秘书：在 `secretaryPrompt` 里现有「召回的记忆」一节之后写「相关原话」一节，别名 `S1`…。把写进去的每条原话引用加进 `c.Dependencies`。`secretaryInstructions` 里相应说明 `S*`。
- 副手：在 `requestRun` 里现有记忆行之后写「相关原话：」一节，把引用加进 `run.ContextVersions` 和 `run.ContextMemoryIDs`，并写 `run_dependencies`。注意现有的 30000 字节上限判断是「超过就跳过这一条」，原话摘录也走同样的跳过逻辑，不新增任何会让请求失败的上限。
- 现有的输出结构约束（`desk_schema.go`）里 `used` 是字符串数组，不用改。

### 4 校验依赖（R6）

`verifyRunForItemTx`（`runs.go`）现在把每个依赖都当陈述查。改成按 `kind` 分开：陈述走原逻辑；原话检查记录还在且当前版本等于记下的版本。`kind` 为空的旧依赖按陈述处理（历史数据都是陈述）。

删除传播不用新写：现有的删除语句按依赖里的 id 匹配，原话的 id 一样会被匹配到。请确认 `run_dependencies` 对原话 id 也能正常写入和匹配。

`artifacts.go` 的 `sanitizeItemTx` 里有一条查询，把一次运行的所有依赖都当陈述来校验，并把返回的 `kind` 写死成 `claim`。原话依赖进来之后，用到原话的副手结果会被误判为无效。**允许你改这一条查询，范围只到这里**：原话依赖只看记录还在、版本没变，不要求授权，不套用陈述的类别和项目规则，事项上的排除项照常生效；返回实际的 `kind`；陈述的判断保持原样。`artifacts.go` 的其他部分不要动。

### 5 依据变了只标记（R7）

现在有三处把回答替换成「（这条回答依据的记忆已变更）」：`deskTurnsTx`（`desk_turn.go`）、`DeskTurnByRequest`（`telegram_turn.go`），以及 `secretaryPrompt` 里靠比较这句话来判断历史轮是否可用。

- 把「被删除清空」和「依据变了」分开。被清空的轮次行为不变。
- 依据变了的轮次：给用户的返回保留原回答、卡片、回执，并设 `Outdated: true`。
- 给模型的历史：不要再用比较字符串的办法。让 `deskTurnsTx` 明确返回每一轮是否 outdated，由提示词组装处决定写替换语「（先前回答的依据已更新，请按现在的资料回答）」，并且不把这一轮的旧依赖并入新一轮。副手交接内容里的「导办台之前的讨论」同样处理。
- 同一请求重放（`DeskTurn` 里读到已存在的轮次直接返回的那条路径）现在直接返回存下来的内容，补上 outdated 的判断。

### 6 依据卡片（R8）

`secretaryCardsTx` 现在按 `answer.Used` 里的 `M*` 别名生成条目。加上 `S*`：条目形状见总览 R8。陈述条目补 `Kind: "claim"`。原话条目不要进时间轴卡片的生成逻辑。

## 容易错的地方

- **不要加任何授权判断、接收者、指纹之类的东西。** 2.0 就是栽在这里，见 [回滚记录](../../../evaluations/2026-10-01-phase2-0-rollback.md)。
- **不要加会让这一轮失败的上限。** 超预算就少给几段。
- **不要为了记录「用了什么」新建表。** 现有的依赖字段够用。
- 当前对话里刚说的这句话在这一轮结束时才入库，检索不会命中它；但前几轮的提问已经入库，会被命中，R2 最后一条就是处理这个。
- 线上有很多份标题相同的资料（十几份都叫「秘书原话」）。别名、依赖、卡片都必须按记录 id 区分，不能按标题。
- 一份原话可能命中多段。这一批每份原话只给排名最高的一段。

## 交付

Draft PR，说明里按 R1–R8 逐条写对应的代码位置；`make check` 通过（需要临时 PostgreSQL，见总览第 8 节）；列出你认为需要改预期的已有测试（不要自己改，见总览第 7 节）；列出拿不准的地方。
