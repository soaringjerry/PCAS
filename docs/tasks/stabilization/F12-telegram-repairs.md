# F12：Telegram 身份隔离、按钮绑定与语音重试

执行者：6.1 Sol / high，`f12_telegram_repairs`，不是 T3 作者。T3 与 F10 均已停止写入，F10 已释放 `poller.go` / `poller_test.go`，可以启动。

在 `/root/PCAS-wt/F12` 从 F10 / #28 `8d2a70df721dd433d45d2232b0405280f0156ca7` 与 T3 / #27 `071e87dc3dd40f08c834797900e68842180d41f9` 无冲突集成 `stabilization/telegram-candidate`，记录并推 SHA，再创建 `stabilization/F12-telegram-repairs`，PR base 为该候选。冲突先报告；F11 在独立候选修 PostgreSQL/AI 模块，之后再做整合验收，不混写。

## 已发现的序列与要求

- T2：切换 bot 后进度和对话虽已重置，相同 message_id 仍可能命中旧 bot 的幂等记录。新 bot 消息应执行一次，旧 bot 重试仍不会重复执行。
- T3：配置 chat 内的伪造消息携带合法 actionId，仍能触发 Undo。必须验证回调与实际发出的回执/消息关联，保留合法按钮和网页撤销后 Telegram 再点击的正确提示。
- T6：语音事务已提交、发送失败后重启，再次转录得到不同文本，引发请求冲突。应从已保存结果恢复投递，不重复转录/生成/执行动作。

先确认 T3 最终证据中的精确绑定和恢复边界，再实现。不能只改提示来掩盖事务重复或漏执行。真实 Telegram 没有实测就如实标注。

## 实现与归属约束

独占 `internal/telegram/poller.go` 及 T3 移交的 `internal/telegram/stabilization_secretary_test.go`；必要的 `internal/telegram/api.go` / 私有类型也可修改，须在开工说明列出。`internal/telegram/poller_test.go` 只允许必要的新边界或旧断言与正式规则一致化，不能删除原覆盖。

需要数据库、notify 凭据存储或 workspace 公共接口时先向协调者提出具体数据与生命周期；不把转录正文或整轮 response 写进新的持久 JSON。优先复用既有 desk 结果和不含正文的关联标识，保持删除传播；不能复制建立第二套会话/记忆库。

已批准的最小扩权：新增 `internal/postgres/telegram_turn.go`，按 owner/requestId 读取既有 desk 结果，应用与历史/重放相同的授权、依赖版本和删除检查，刷新撤销及当前 State；不增加 HTTP 接口或 workspace 公共类型。`internal/notify/settings.go` 及相关测试可保存 getMe 确认的稳定 bot ID、凭据 guard、无正文发送关联和 legacyConversation 迁移锚点。关联包含 bot/chat/message/request/conversation/turn 标识与 SentAt，按 30 天裁剪；不使用 100 条硬上限让有效期内按钮失效。既有 64KiB 读取上限若不适用，只调整本机服务写入的 0600 凭据文件解码，不扩展网络输入限制。

旧 chat-only requestId 只在首次身份确认、原配置和会话未变、结果会话匹配迁移锚点时复用；无可信锚点不猜 bot 归属。真正换 bot 清关联，同 bot 换 token 确认身份后保留且旧 token 受写入 guard 限制。回调同时核验发送绑定与 action 归属，旧无可信绑定的按钮明确失效。发送已成功但关联未落盘即崩溃的窗口需要如实报告，不宣称外部消息恰好投递一次。

旧 TestChangedRetranscriptionDoesNotBlockLaterMessages fake 只有 ErrConflict、没有已保存 turn，已批准改为验证未提交冲突不确认消息、不推进 offset；重查有保存结果才恢复原回执，无结果返回错误等待重试。保留真数据库 T6 已提交语音恢复断言，报告注明夹具纠正。

幂等必须绑定正确的 bot 身份与 chat，不能把明文 token 作为日志、外部 ID 或用户可见字段。说明同一 bot 换 token 时的兼容边界，不能只改一个测试常量。重启恢复和旧消息关联兼容要有明确策略，不依赖仅内存的 map。

F10 已授予的 expired 文案映射先完成再移交本文件；本任务沿用该映射，不改撤销错误语义。秘书并发、超时、长度和记忆删除由 F11 处理，产品文件互不重叠。

唯一报告 `docs/evaluations/2026-10-01-telegram-repairs.md`；不改其他报告/任务文档/前端。

## 验收

T2/T3/T6 原始失败正常执行并移除对应 skip；非授权 chat、群聊、合法回调、重复投递、文字/语音故障恢复、410 通道隔离等相关既有回归不退步。使用本地假 bot/模型/转录和自有 tmpfs 数据库，运行 make check、相关集成与 race 测试，清理所有自有资源，交付独立 PR；不发真实消息、不合并、不部署。

## 最终浏览器 CI 补验（07:00 UTC）

PR #30 c1a0173 的 run36826478748 在 G7 三轮均未看到入站任务，其他失败尚未报告。原 F12 执行者接手调查，先确认内部 golden 假 Bot API 是否遗漏新增 getMe 协议；当前该 fixture 仅显式实现 getUpdates/sendMessage。唯一新增文件归属 internal/testsupport/golden/main.go 及自己的 F12 报告。真实身份检查不可放宽，不能为通过测试绕过 getMe 或回调绑定。若确为夹具缺口，补上符合官方协议的稳定 bot ID/is_bot 响应，保留当前回调/业务断言；若另有产品问题先报告最小范围。

web/tests/golden.spec.ts 当前由 F11 修正 G9 文案，F12 只读该文件；若 G7 确需改动，先报告，待 F11 释放后再由一个执行者接手。自有端口 18148/18149、tmpfs PG，针对 G7 三轮与相邻通知回归；完整最终验收由 A1 执行。修复在原 F12 分支补提交、更新报告/PR、清理自身资源、交最终 SHA，不合并部署。
