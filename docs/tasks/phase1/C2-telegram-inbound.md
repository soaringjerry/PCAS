# 任务 C2：Telegram 双向对话

执行者：**6.1 Sol** · 分支：`phase1/C2-telegram-inbound` · 依赖：**B1 和 C1 都已合并**
开工前读：[README.md](README.md)、[contracts.md](contracts.md) 第 1.4、2、4 节、[白皮书](../../whitepaper.md) 第 6、11 章。

**目标**：手机 App 还没有，Telegram 先当手机上的秘书入口。用户给 bot 发文字或语音，就等于在首页对秘书说了一句话；回执会回到 Telegram，也能在那里撤销。
这个思路借鉴自 Hermes Agent 的消息网关，见 [研究笔记](../../research/hermes-agent.md)。

## 要做什么

### 1 收消息（新包 `internal/telegram/`）

- 用长轮询 `getUpdates`（timeout 30 秒）接收消息，**不用 webhook**，这样不需要额外开放公网入口。offset 持久化在 C1 的 `notify.json` 里，字段名 `telegramOffset`，重启后不会重复处理。
- 只在 C1 的 Telegram 配置里 **token 和 chatId 都有值时**才启动轮询。C1 在 chatId 为空时会用 `getUpdates` 自动检测，那时轮询还没启动，两者不会抢消息。配置变更后，轮询要在一分钟内感知到并重启。
- **只接受配置里那个 chatId 发来的消息**，其他人的消息直接忽略，不回复，也不记录内容。
- 在 `cmd/pcas/main.go` 的 `serve` 分支加一行启动它。C1 已经在这里加过一行，这次紧挨着加。

### 2 文字 → 秘书

- 每个 chat 维持一个当前对话 ID，存进 `notify.json` 的 `telegramConversation` 字段。用户发 `/new` 就开启新对话。
- 调用 B1 实现的 `DeskTurn`，在服务端直接调用函数，不走 HTTP：
  - `requestId` 由 `chat_id` 和 `message_id` 确定性地派生（例如取 UUIDv5），这样同一条消息重复投递也只执行一次；
  - `thingId` 为 null；
  - `agentId` 用工作区的默认 agent。没有可用的 agent 时，B1 会自动退回到只保存原话。
- **回复格式**（Telegram 纯文本，简短，不写长文）：
  - `reply`，如果有；
  - 每条回执一行，前面加状态符号（✓ 表示完成，· 表示跳过）；
  - 卡片压缩成文字：`timeline` 每条一行「日期 · 内容」，`tasks` 每条一行「○ 标题 · 截止」，`sources` 只写「依据 N 条记录」，`links` 列出域名。
  - 每条可撤销的回执配一个 inline keyboard 按钮「撤销 <简短标题>」，`callback_data` 为 `u:<actionId>`。
  - 有 `ask` 时，把 options 做成 inline 按钮，`callback_data` 为 `a:<序号>`；点击就把该选项作为下一句发给秘书。
- **按钮回调**：
  - `u:` 调用 `undoAction` 的内部实现（B1），结果用 `answerCallbackQuery` 提示「已撤销」，或者提示契约 1.4 里的错误文案；
  - `a:` 按上面的规则处理。

### 3 语音 → 文字 → 秘书

- 收到 voice 或 audio 时，用 `getFile` 下载（大小上限 20 MB），交给 `internal/ai` 的 `Registry.Transcribe` 转成文字，然后按第 2 节处理。
- 回复第一行写「🎤 听到：<转写文本>」。
- 没有配置转录模型时，回复「还没有配置语音转写，先发文字吧」，原始音频按第 4 节存进资料。

### 4 图片和文件

- photo 或 document：下载后复用现有的附件导入逻辑（`POST /v1/memory/attachments` 背后调用的那个 Store 方法），存为资料。
- 回复「收到，已存进资料」。附带的说明文字（caption）不为空时，把它当成一句话交给秘书。

### 5 设置页

- C1 的 `NotifySettings.tsx` 在 Telegram 已连接时，加一句说明：「现在也可以直接给 bot 发消息或语音，等于在首页对秘书说话；发 /new 开始新对话」。只加这句说明，不做其他改动。

## 不要做

- 不要改 `DeskTurn` 和 `undoAction` 的语义；接口不够用时在 PR 里提出。
- 不要做群聊支持，也不要支持多个 chatId。

## 验收（`internal/telegram/*_test.go`，用 httptest 模拟 Bot API 和模型）

- [ ] 发一条文字：秘书被调用一次，Bot API 收到的回复里有回执；同一条 update 重复投递不会重复执行。
- [ ] 非配置 chatId 发来的消息被忽略，秘书没有被调用。
- [ ] 回调 `u:<actionId>`：撤销生效，并且调用了 `answerCallbackQuery`。
- [ ] 回调 `a:1`：第二个选项作为下一句发出，而且在同一个对话里。
- [ ] 语音：模拟转录返回「明天下午三点开会」，秘书收到这句话，回复第一行是「🎤 听到：…」。
- [ ] 没有配置转录模型时：回复提示文案，音频被存为资料。
- [ ] `/new` 之后开启新对话。
- [ ] 重启后 offset 不回退，不会重复处理旧消息。
- [ ] Telegram 配置被移除后，轮询在一分钟内停止。
- [ ] `make check` 和 `make test-integration` 通过。
- [ ] 手动验证（写进 PR）：用户提供测试 bot 后，用手机发一句带时间的话、一段语音，各点一次撤销。**token 不能提交，也不能写进 PR。**
