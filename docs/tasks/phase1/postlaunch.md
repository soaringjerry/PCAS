# 第 1 阶段上线后修复

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

问题与根因见 [上线后问题复盘](../../evaluations/2026-10-01-phase1-postlaunch.md)。共同规则、环境和禁止事项沿用 [README](README.md)，包括 2026-10-01 新增的「验收与部署」要求。

## 任务

| 任务 | 内容 | 执行者 | 状态 |
|---|---|---|---|
| F7 | 连续撤销：先撤销修改、再撤销新建会失败（见下） | 6.1 Sol | PR #20 已开，四项 CI 通过；待合并，未确认部署 |
| F3 | Telegram 保存失败时区分四种错误码，给出中文提示；补上 `mobile-web-app-capable` meta | 6.1 Sol | ✅ #16 已部署 |
| F4 | 秘书解析放宽、纯文字作为回复显示、失败按阶段给出准确文案并写日志 | 6.1 Sol | ✅ #17 已部署 |
| F5 | Codex 通道输出 JSON（见下） | 6.1 Sol | ✅ #18 已合并并部署（2026-10-01 00:37 UTC），冒烟测试 3/3 通过 |
| F6 | 时区可设置，自动检测，前后端统一（见下） | 6.1 Sol | #19 已于 2026-10-01 00:53 UTC 合并；部署待核实，显示漏项由 F8 收尾 |
| F8 | 事项详情、秘书卡片及项目列表等动态时间统一使用工作区时区 | 6.1 Sol | [任务包](../stabilization/F8-timezone-displays.md)已准备 |
| C-1 | 线上时区改为 `Australia/Melbourne` | 协调者 | 待用户确认 |
| C-2 | 配置语音转写（OpenAI `gpt-4o-transcribe`） | 协调者 | 待用户确认 |

F5 和 F6 改的文件不重叠，可以并行。全部合并后统一部署一次，然后按下面的清单做线上实测。

### F5 · Codex 通道输出 JSON

- **根因**：`internal/ai/codex.go` 的 `Generate` / `GenerateWithSearch` 固定传入 `developerInstructions: "Answer with text…"`，压过了 `baseInstructions` 里秘书要求的「只输出 JSON」。
- **要求**：
  1. `developerInstructions` 不再规定输出形式，改为要求遵守 baseInstructions 的输出格式；「不使用本地工具、不读文件、搜索词不得包含私人信息」这些限制保留。
  2. 如果 Codex app-server 的 turn 参数支持 `outputSchema`，为秘书调用传入 JSON Schema；不支持就在 PR 里说明。
  3. `secretaryPrompt` 末尾加一行格式提醒。放在最后，不破坏前缀缓存。
  4. 旧的 `/v1/desk/answer`、副手 run、后台整理在 Codex 通道下的行为不变。
  5. 用本机 Codex 通道实测（临时 tmpfs 的 Codex home、合成数据，不碰线上）：「明天下午三点给张三回邮件」返回合法 JSON，并建出带时间和提醒的事项；「今天天气怎么样」能正常回答。
- **范围**：`internal/ai/codex.go`、`internal/postgres/desk_turn.go`（只改 prompt 末尾）及对应测试。分支 `fix/codex-json`。

### F7 · 连续撤销

- **现象**（2026-10-01 部署后冒烟测试发现）：秘书先「新建」一件事，再「改到四点」。撤销「改到四点」成功；接着撤销「新建」，返回 409 `changed_since`。
- **根因**：撤销写回 before 快照时，`Version` 加 1，history 里多了一条「撤销：…」，`saveAction` 还会追加一条 `sources` 收据。于是文档的 sha256 再也不可能等于「新建」那个动作的 `afterHash`。按从后往前的顺序连续撤销，是最常见的用法。
- **要求**：
  1. `afterHash` 和撤销时的比对，都改为对**去掉簿记字段之后的文档**计算 sha256。去掉的字段：`recordVersion`、`updatedAt`、`history`、`evolution`、`sources`。触发器和 Go 端的比对，必须使用同一个规范化函数。
  2. 已有的 `action_log` 行存的是旧算法的 hash：比对时，旧 hash 或新 hash 任何一个匹配都算通过。或者写一个迁移，把还没撤销、也没过期的行按新算法重算（快照里有 before，没有 after，所以旧行只能双算法兼容）。在 PR 里说明选了哪种。
  3. 其他字段（标题、状态、截止时间、提醒、子任务、说明、项目等）只要被改过，仍然要返回 `changed_since`。
  4. 测试：新建 → 修改 → 撤销修改 → 撤销新建，全部成功，事项消失；新建 → 用户改了标题 → 撤销新建，返回 `changed_since`；撤销采纳 → 再撤销更早的动作，也能成功。
  5. 修好后，协调者会用它撤销线上冒烟测试留下的那条「给张三回邮件」（目前已手动标为取消）。
- **范围**：迁移 `019_*.sql`（替换触发器函数）、`internal/postgres/actions_log.go` 及测试。分支 `fix/undo-chain`。

### F6 · 时区

- **根因**：`internal/postgres/workspace.go` 把默认时区写死为 `Asia/Shanghai`，设置页没有入口；前端按浏览器时区显示。
- **要求**：
  1. 设置页加「时区」（IANA 时区，可搜索，常用的放前面），保存走 `updateSettings`；后端用 `time.LoadLocation` 校验，失败时返回明确的错误码。
  2. 新工作区的默认时区取第一次登录时浏览器的 `Intl.DateTimeFormat().resolvedOptions().timeZone`。已有工作区如果设置和浏览器时区不一致，首页显示一条可关闭的时区差异提示，用户点击后切换到设备时区；关闭后同一浏览器不再提示。F6 原提示为「你的时区好像是 X，要改成 X 吗？」；U1 / PR #31 候选精简为「时间按 Y 显示，和这台设备不同」及「改成 X」按钮，Y 为当前工作区时区、X 为设备时区，行为不变。
  3. 首页「今天」的分组和时间线、各处的时间显示，统一按工作区设置的时区计算，和后端回执保持一致。
  4. 测试：时区保存与校验；提示的出现与关闭；同一个任务在设置时区下的「今天」分组正确。
- **范围**：`internal/postgres/workspace.go`（默认值与校验）、`web/src/pages/SettingsPage.tsx`、`web/src/pages/HallPage.tsx`、`web/src/domain/time.ts`、`web/src/domain/hall.ts`、`web/src/domain/lines.ts` 及对应测试。分支 `fix/timezone`。

## 线上实测清单

全部修复部署后，由协调者在线上用**真实配置**逐项检查：ChatGPT 订阅（Codex）通道、用户的时区、真实的 Telegram bot、已订阅推送的设备。结果写进本节，然后请用户实际使用一天。

| # | 检查 | 通过标准 |
|---|---|---|
| 1 | 问「今天天气怎么样」 | 一句话回答（城市正确），不出现「已记下原话」 |
| 2 | 「5 分钟后提醒我喝水」 | 回执的时间是本地时间；事项、提醒都建出来了 |
| 3 | 「改到 10 分钟后」 | 改的是同一件事，提醒时间跟着变 |
| 4 | 点回执的【撤销】 | 事项消失，刷新后显示「已撤销」 |
| 5 | 到点 | 首页「到点了」出现这件事；设备弹出通知，点击能打开事项；Telegram 收到消息 |
| 6 | 给 Telegram bot 发文字 | 回执出现在 Telegram，首页能看到这件事；在 Telegram 里点撤销有效 |
| 7 | 给 Telegram bot 发语音 | 回复首行是「🎤 听到：…」，并正确执行（需要先完成 C-2） |
| 8 | 在事项页让秘书拆步骤 | 副手完成后自动加成子任务；撤销后消失；「放回去」后恢复 |
| 9 | 首页打勾完成，再点撤销 | 事项回到原位 |
| 10 | 在资料库删除一条秘书原话 | 对话里那一轮显示「（内容已删除）」 |
| 11 | 日志 | 检查期间没有未解释的 WARN 或 ERROR；秘书的每一轮都有对应阶段的日志 |

### 2026-10-01 实测结果

在线上用真实浏览器从正式域名登录，走 ChatGPT 订阅（Codex）通道，工作区时区为用户的实际时区。首次运行的版本是 main `3402644`，第 8、10 项在 `6230b9f` 上重测。测试产生的事项、原话和提醒都已撤销或删除；只有一条因为副手已经开工而无法撤销，改成了已取消。

| # | 结果 | 说明 |
|---|---|---|
| 1 | 通过 | 一句话回答，城市正确，没有落到「已记下原话」 |
| 2 | 通过 | 回执是本地时间；提前量已过，提醒落在截止时间 |
| 3 | 通过 | 改的是同一件事，提醒时间跟着变 |
| 4 | 通过 | 先撤销修改、再撤销新建，事项消失；刷新后两条回执都显示「已撤销」 |
| 5 | 服务端通过，设备弹出待用户确认 | 首页「到点了」出现这件事；服务端记录 Web Push 和 Telegram 都已发出。手机上是否弹出、点击能否打开，要用户看一眼 |
| 6 | 未做 | 需要用户用自己的 Telegram 给 bot 发文字 |
| 7 | 未做 | 需要用户用自己的 Telegram 给 bot 发语音 |
| 8 | 首次未通过，修复后通过 | 真实模型把步骤写成编号列表，自动采纳只认「- [ ]」格式，结果被存成文档而不是子任务。修复（PR #54）：拆步骤的请求里写明输出格式。部署后重测：副手按格式返回三行，自动加成 3 个子任务，撤销后为 0，放回去后恢复为 3 |
| 9 | 通过 | 打勾后状态为完成，从提示条撤销后回到原位 |
| 10 | 首次未通过，修复后通过 | 删除原话后，对话里那一轮显示的是「（这条回答依据的记忆已变更）」。修复（PR #54）后重测，显示「（内容已删除）」 |
| 11 | 通过 | 检查期间没有 WARN 或 ERROR；6 轮秘书对话都有解析阶段的日志 |

剩下第 5 项的设备弹出、第 6 项和第 7 项要用户用自己的手机和 Telegram 完成。另外，部署后重测第 8 项时，秘书在事项页对「拆成三步」直接用「加步骤」动作加了三个子任务，没有派给副手；两条路都能得到子任务。
