# 第 1 阶段上线后修复

问题与根因见 [上线后问题复盘](../../evaluations/2026-10-01-phase1-postlaunch.md)。共同规则、环境和禁止事项沿用 [README](README.md)，包括 2026-10-01 新增的「验收与部署」要求。

## 任务

| 任务 | 内容 | 执行者 | 状态 |
|---|---|---|---|
| F3 | Telegram 保存失败时区分四种错误码，给出中文提示；补上 `mobile-web-app-capable` meta | 6.1 Sol | ✅ #16 已合并，待部署 |
| F4 | 秘书解析放宽、纯文字作为回复显示、失败按阶段给出准确文案并写日志 | 6.1 Sol | ✅ #17 已合并，待部署 |
| F5 | Codex 通道输出 JSON（见下） | 6.1 Sol | 进行中 |
| F6 | 时区可设置，自动检测，前后端统一（见下） | 6.1 Sol | 进行中 |
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

### F6 · 时区

- **根因**：`internal/postgres/workspace.go` 把默认时区写死为 `Asia/Shanghai`，设置页没有入口；前端按浏览器时区显示。
- **要求**：
  1. 设置页加「时区」（IANA 时区，可搜索，常用的放前面），保存走 `updateSettings`；后端用 `time.LoadLocation` 校验，失败时返回明确的错误码。
  2. 新工作区的默认时区取第一次登录时浏览器的 `Intl.DateTimeFormat().resolvedOptions().timeZone`。已有工作区如果设置和浏览器时区不一致，首页显示一条可关闭的提示「你的时区好像是 X，要改成 X 吗？」，点一下就改；关闭后同一浏览器不再提示。
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
