# 任务 C1：提醒通道

执行者：**6.1 Sol** · 分支：`phase1/C1-notify` · 依赖：A 已合并；与 B1、B2 并行
开工前读：[README.md](README.md)、[contracts.md](contracts.md) 第 3、4 节、[白皮书](../../whitepaper.md) 第 9、10 章。

**现状**：`internal/postgres/reminders.go` 每 30 秒检查一次，把到点的触发器写进 `workspace_notices`，但之后就没有下文了，用户根本收不到。
**目标**：到点的提醒通过三个通道送到用户手上：首页置顶（D2 负责展示）、Web Push（浏览器 / PWA）、Telegram。
另外要为以后的手机 App 推送留好扩展点。

触发器由 B1 写入（契约第 3 节）。开发期间可以自己在测试里直接构造带 `due-reminder` 的任务。

---

## 1 通道抽象（新目录 `internal/notify/`）

```go
type Message struct { NoticeID, ThingID, Title, Body, URL string }
type Channel interface {
    Name() string                                  // "webpush" / "telegram"
    Send(ctx context.Context, m Message) error     // 返回 ErrGone 表示这个目标永久失效
}
```

- `webpush.go`：用 `github.com/SherClockHolmes/webpush-go`，向所有订阅发送。
  - 某个订阅返回 404 或 410 时删掉它，其他订阅照常发送。
  - VAPID subject 用 `PCAS_PUBLIC_URL`。
- `telegram.go`：直接调用 Bot API 的 `sendMessage`，不引入 SDK。文本格式为 `⏰ <标题>\n<时间>\n<链接>`。
- 以后加手机 App 推送，只需新增一个实现，不改分发逻辑。

## 2 密钥存储

- 新文件 `notify.json`，放在 `PCAS_MODEL_SETTINGS_FILE` 的同一目录；也可以用 `PCAS_NOTIFY_SETTINGS_FILE` 覆盖路径。
- 文件权限 0600，写法照 `internal/ai/settings.go` 的 `writeSettings`（临时文件 + rename，保证原子写入）。
- 内容：`{ "vapidPublic", "vapidPrivate", "telegramToken", "telegramChatId" }`。
- VAPID 密钥在第一次需要时生成并保存，之后一直沿用。
- 私钥和 token 不写日志，也不返回给前端。

## 3 迁移 `017_notify.sql`

- `workspace_notices` 增加三列：`id uuid NOT NULL DEFAULT gen_random_uuid()`（加唯一索引）、`dismissed_at timestamptz`、`delivered jsonb NOT NULL DEFAULT '{}'`。
  `delivered` 的格式为 `{"webpush": "RFC3339", "telegram": "RFC3339", "attempts": {"telegram": 2}}`。
- 新建 `push_subscriptions` 表：`owner_id`、`endpoint text`、`p256dh text`、`auth text`、`created_at`，主键为 `(owner_id, endpoint)`。

## 4 分发循环

- 新文件 `internal/postgres/notify.go` 实现 `(s *Store) RunNotify(ctx, logger, channels []notify.Channel)`，每 15 秒执行一次。
- 在 `cmd/pcas/main.go` 的 `serve` 分支，挨着 `RunReminders` 加一行启动它。
- 每一轮做这些事：
  1. 找出 24 小时内产生、未关闭（`dismissed_at IS NULL`）、并且对应事项仍未完成或取消的通知。
  2. 对每个通道，跳过已经送达的，其余调用 `Send`，成功后写入该通道的送达时间。
  3. 失败时累加 `attempts`，按 1 分钟、5 分钟、30 分钟退避重试，最多 5 次。
  4. **发送前重新读取事项状态**：已完成就不发，PRD 第 4 节要求执行前核对最新状态。
- 某个通道没有配置（没有订阅、没有 token）时直接跳过，不算失败。
- 消息内容：Title 为事项标题；Body 为「<本地时间> · <reason>」；URL 为 `PCAS_PUBLIC_URL + "/t/" + thingId`。

## 5 接口（新文件 `internal/httpapi/notify.go`）

- 按契约 4.2 实现全部 6 个接口，只允许 owner 调用。
- 在 `internal/httpapi/server.go` 里加一行注册路由。
- 用 `Options` 传入一个接口类型，写法照现有的 `Workspace`、`Models`。

## 6 State 里的 notices

- `internal/workspace/notify.go` 定义 `Notice` 类型，`State` 加字段 `Notices []Notice`（JSON 字段名为 `notices`）。
- `internal/postgres/workspace.go` 的 `snapshotTx` 查询最近 100 条通知，未关闭的排在前面。
- 现有把通知放进 `jobs` 的逻辑保持不动。
- 前端 `web/src/domain/types.ts` 按契约 4.1 加 `Notice` 和 `State.notices`。

## 7 前端：PWA 与设置

- **PWA**：
  - 新建 `web/public/manifest.webmanifest`：name 为 PCAS，`display: standalone`，`start_url: /`，图标见下。
  - 用机器上能用的工具（`rsvg-convert` 或 ImageMagick），从 `web/public/favicon.svg` 生成 `icon-192.png`、`icon-512.png`、`apple-touch-icon.png`（180）三个文件，一并提交。
  - `web/index.html` 加 manifest 链接、`apple-touch-icon`，以及 `apple-mobile-web-app-capable`。
- **Service Worker**：`web/public/sw.js`。
  - `push` 事件：`showNotification(title, { body, tag: noticeId, data: { url } })`。
  - `notificationclick` 事件：已经打开了 PCAS 的窗口就聚焦它并跳转到 url，否则新开一个窗口。
  - **不要加 fetch 缓存**，避免用户拿到过期的前端代码。
  - 在 `web/src/main.tsx` 里注册 Service Worker。这个文件没人负责，你是唯一改它的人，只加注册这几行。
- **设置**：新建组件 `web/src/components/NotifySettings.tsx`，在 `SettingsPage.tsx` 里加一行挂载它。风格沿用设置页现有的写法。
  - 「在这台设备上接收提醒」开关：请求通知权限 → 用 VAPID 公钥订阅 → `POST` 到后端；关闭时 `DELETE`。
    浏览器不支持时显示原因。iOS 上加一句提示：「iPhone 需要先把 PCAS 添加到主屏幕」。
  - Telegram：token 输入框（密码类型）、chat ID（可选，旁边写明「留空会自动检测，请先给你的 bot 发一句话」）、保存按钮、状态显示（已连接 / 未连接）。
  - 「发一条测试提醒」按钮：调用 `POST /v1/notify/test`，显示实际发到了哪些通道。

## 不要做

- 不要改提醒的触发逻辑本身（`CheckReminders` 写通知的部分），也不要写触发器（B1 负责）。
- 不要做首页置顶的展示（D2 负责），你只需要提供数据。
- 不要改 `commands.go`。关闭通知用专门的接口，不走命令。

## 验收

Go 测试（`internal/notify/*_test.go`、`internal/postgres/notify_test.go`）：
- [ ] 有到点的通知：假通道被调用一次；下一轮不会重复发送；`delivered` 里有记录。
- [ ] 事项在发送前被完成：不发送。
- [ ] 通道失败：`attempts` 增加，退避时间到之前不会重试，满 5 次后不再重试。
- [ ] Web Push：用 httptest 当推送端点，能收到加密后的请求；端点返回 410 时对应的订阅被删除。
- [ ] Telegram：用 httptest 模拟 Bot API，`sendMessage` 的参数正确；`chatId` 为空时通过 `getUpdates` 取到 chat ID；token 无效时 `PUT` 返回 400。
- [ ] `notify.json` 的文件权限是 0600，不存在时能自动创建，并发写不会损坏文件。
- [ ] `GET /v1/workspace` 的响应里有 `notices`，关闭接口会设置 `dismissedAt`。

前端（`web/tests/notify.spec.ts`，用 mock API）：
- [ ] 设置页能看到通知设置；保存 Telegram 时发出的 `PUT` 请求体正确；测试按钮显示返回的通道列表。

手动验证（写进 PR 描述）：
- [ ] 在临时实例上，桌面 Chrome 打开开关 → 点测试 → 收到系统通知 → 点击通知能打开对应页面。
- [ ] 用户提供测试 bot 的 token 后，Telegram 能收到测试消息。**token 不能提交到仓库，也不能写进 PR。**
