# F12：Telegram 身份、回调绑定与语音恢复

2026-10-01；执行者 `f12_telegram_repairs`，不是 T3 独立测试作者。工作区 `/root/PCAS-wt/F12`，修复分支 `stabilization/F12-telegram-repairs`；PR base 为 `stabilization/telegram-candidate`。候选以 F10 / #28 `8d2a70df721dd433d45d2232b0405280f0156ca7` 加 T3 / #27 `071e87dc3dd40f08c834797900e68842180d41f9` 无冲突合并，已推送 `e9e5edb0cc1658dfaf04a80d6265a0dd77cbef24`。它已含 F9、T1、T2、F7；未并入 F11。没有合并 main、部署或发送真实 Telegram 消息。

三项原 finding 均先真实复现，修复后删除对应 skip 并正常运行通过。该结论仅覆盖 F12 及相关渠道回归，**不代表稳定化阶段完成**。

## 真实复现与修复结果

使用原 T3 断言及 `PCAS_STABILIZATION_RUN_FINDINGS=1`，修复前日志 `/tmp/pcas-test-F12-before.log`：

| 原 finding | 实际失败 | 修复后的相同断言 |
|---|---|---|
| T3-T2 | 两 bot 同 chat=123、message_id=1，新 bot offset/会话已重置，但任务仅 1 件，预期 2 件。 | 2 件任务、2 次模型调用；原 `lower_update_id` 进度重置和旧 writer 拒绝仍通过。 |
| T3-T3 | 已发送 message=101 的合法 actionId 被放进从未发送的 message=999，任务从 1 件变 0 件。 | 保持原 1 件任务；其他 chat/group/spoofed sender/malformed callback 均继续无业务影响。 |
| T3-T6 | voice 事务已提交但 sendMessage 500；fresh state 重启重转录，调用数=2，回执变成“已处理在首页查看”，原回执丢失。 | 转录仅 1 次、模型仅 1 次、任务仅 1 件；恢复已保存的原回执并处理下一条；原文字重启恢复断言仍通过。 |

## 最小实现与存储边界

`internal/telegram/poller.go`、`api.go` 使用 Bot API `getMe` 的 `User.id` 确认 bot 身份，所有 desk request、`/new` 及附件外部 ID 绑定 bot ID + chat + 事件。身份不从 token 猜测，不把明文 token 作为外部 ID、日志或用户输出。官方协议见 [getMe](https://core.telegram.org/bots/api#getme)、[User](https://core.telegram.org/bots/api#user) 和 [CallbackQuery](https://core.telegram.org/bots/api#callbackquery)。凭据轮换但 `User.id` 相同时，业务 request ID 相同；实际不同 bot 则独立处理。同 bot 换 token 和返回旧 bot 的同 message ID 回归都通过。getMe 失败或返回非 bot 时不处理、不推进 offset。

经协调者明确授权，扩展 `internal/notify/settings.go`，并新增 `internal/postgres/telegram_turn.go`；未改 F11 拥有的 `desk_turn.go`、`desk_parse.go`、`database.go`、`provider.go` 或其 T3 PostgreSQL 测试，未新增 workspace 公共类型、HTTP 接口、表或迁移。

- 既有 0600 `notify.json` 新增已确认 bot ID、token 摘要 guard、legacy conversation 锚点，以及 `messageID/requestID/conversationID/turnID/sentAt` 关联。每条关联隐含属于当前已确认 bot/chat；真正换 bot、换 chat 或移除配置清除关联，同 bot 换 token 经再次确认后保留。旧 token 不能确认身份、写关联或覆盖新配置进度。
- 关联仅在 `sendMessage` 成功返回真实 message ID 后写入；按发送时间 30 天裁剪，没有 100 条硬上限。相关测试写入并读回 400 条、文件超过旧 64KiB 上限，轮换仍保留全部活跃关联，31 天关联被裁剪。因此把本地凭据文件的原 64KiB 解码限制改为流式读取；仍保持严格字段解码、原有锁、原子替换及私有权限。存储/重写开销随最近 30 天投递量增长；本任务未做大量真实消息负载测试。
- 回调必须命中持久已发消息关联，再按 request ID 重读其 turn，核对 conversation/turn ID。Undo 还要确认 action 确实是该 turn 的可撤销回执；合法网页先撤销后 Telegram 再点击仍得到“已经撤销过了”，F10 的 expired 映射全部保留。
- 追问同样需要可信关联，选项正文只从既有 desk 结果读取；移除仅凭回执文字匹配的恢复。fresh state 下复制合法消息正文到另一 message ID 不可恢复选项，另一轮 action 也不可借真实 message ID 撤销。已提交但送达失败的选项结果可以恢复，已送达提示不能再次选择。
- `DeskTurnByRequest` 只读现有 `desk_turns.response`，限定 owner，检查 owner 权限、依赖版本/授权，沿用 DeskTurns 的 erased/依赖变更提示与卡片清理，刷新撤销状态并取当前 State。无模型、无转录、无动作写入、无新历史正文副本，也不受 DeskTurns 最近 50 轮的查询窗口限制。
- 收到文字/语音/带 caption 文件后先查既有结果。已提交的语音恢复原回执和已保存转录前缀，免去下载、转录及模型调用；`turn.Text` 被删除时不恢复转录前缀。删除原话来源后 fresh state 恢复、越 owner 读取及非 owner 拒绝都有真实数据库断言。已发送到 Telegram 的外部消息不由本补丁远程删除；本地关联没有正文，后续重放读取清理后的内容。引用记忆删除的整轮清理仍消费 F11/既有删除规则，不建立第二套记忆库。

## 兼容与不能保证的窗口

升级前 chat-only 的旧 request ID 仅能保守迁移：首次身份确认时保存原配置中已有的 conversation ID，旧结果必须属于该锚点才可恢复。原配置/会话未变的文字和语音恢复通过，语音不调用转录；不同 conversation 和换 bot 后旧 request ID 均拒绝借用。无锚点的历史请求无法安全认定 bot 归属；不把它猜成新 bot 的结果。同 bot token 轮换保留新格式幂等/关联；迁移前未完成的旧格式请求若在首次确认前换过配置，则没有安全迁移保证。

升级前没有可信关联的旧按钮返回失效，不能凭合法 actionId 或复制的正文补认。30 天后的关联失效，网页撤销仍遵循动作保留期语义。真正换 bot 清当前旧 bot 的回调关联；重新配置旧 bot 时新格式 request 仍可恢复其原结果，但更换期间已清除的旧按钮关联不凭空重建。

外部 `sendMessage` 成功、但关联落盘或 offset 更新前崩溃时，结果可能再次发送；之前没有落盘关联的按钮安全失效，重发成功后生成新的关联。该修复保证业务动作不因正常重投重复执行，并恢复已提交结果；**不声称 Telegram 消息投递 exactly-once**。事务提交前崩溃的语音没有已存 turn，仍需重新转录；无法在没有提交结果时恢复结果。

旧 `TestChangedRetranscriptionDoesNotBlockLaterMessages` 的 fake 仅返回 `ErrConflict`，没有任何已提交 turn，不代表 T6 的已提交语音。经协调者明确批准，改为 `TestUncommittedConflictDoesNotAcknowledgeInput`：冲突后再查真实结果；存在则恢复，不存在则保留 offset 返回错误，重试成功后处理下一条。删去冒称已处理的提示，没有降低 T6 真库断言。原语音 retry 单测现在要求只提交一次、只转录一次且保留原转录前缀；Undo 文案单测先发送真实回执再点击，保留全部原错误映射断言；原 ask 重启测试改为断言持久标识关联且不存正文/选项，而非要求凭据文件永远恰好 6 个字段。

## 验证与依赖

全部使用本地 fake Bot API/模型/转录和自有容器 `pcas-test-F12-telegram`：`pgvector/pgvector:0.8.2-pg16-bookworm`，PG data 为 1GiB tmpfs，WAL 128/32MB，localhost 随机端口 33256。未调用真实 Telegram、模型账户或生产数据库。永久证据为本报告的复现及提交中的断言；`/tmp/pcas-test-F12-*.log` 为本机辅助日志。

```sh
# 每个复跑环境使用自己的 disposable PostgreSQL 地址。
PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33256/postgres?sslmode=disable' \
  go test -race -p=1 -count=1 ./internal/telegram ./internal/notify -v
# 正常运行，未设置 finding 开关；Telegram/notify 全部用例零 skip。

env -u PCAS_TEST_DATABASE_URL -u PCAS_STABILIZATION_RUN_FINDINGS make check

PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33256/postgres?sslmode=disable' \
  make test-integration

PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33256/postgres?sslmode=disable' \
  go test -race -p=1 -count=1 ./internal/postgres -run '^TestStabilization' -v
```

最终 Telegram/notify 全包 race 检查通过，三项 finding 无开关、零对应 skip；包含合法 Undo、网页→Telegram U15、重复 update/callback、非授权/群聊过滤、长回复、转录未配置音频保存、配置取消/重启、文字与语音故障恢复及 notify 的 R11/410 通道隔离。新增迁移、持久绑定、选项故障恢复、身份失败及凭据轮换边界均通过。`make check` 通过 fmt/vet/race/build（该命令不带数据库，不充当数据库零 skip 验收）。完整 `make test-integration` 通过，PostgreSQL 全包 race 123.497s；该包已有 skip，不能当作阶段通过证据。

另跑 PostgreSQL 编号专项通过，66.855s，明确保留 **9 个叶 skip**：未并入 F11 的 S1/S8-timeout/S9/D1，以及需在 F11 含 F10/T3 候选确认的 D3-expired_code，共 5 个 S/D；T1 待裁定 U3/U4-user/U5/U10 共 4 个。本任务未改这些测试，未声称其通过。F11 已交付的修复须后续集成验收，本 PR 不自行混入它。

真实 Telegram/手机录音、机器崩溃/多实例、线上 11 项和用户一天试用未验收。

## 清理

自有测试容器在验证结束后执行 `docker rm -f -v pcas-test-F12-telegram` 并确认不存在；HTTP fake 服务、临时通知和 blob 目录由测试 cleanup 释放。没有持久后台进程、没有按进程名终止、没有清理其他任务或生产资源。小型 `/tmp/pcas-test-F12-*.log` 留供协调者检查。交付后停止写入。

## 远端完整浏览器 CI 补验

首次交付 `c1a0173629568c4588ed1f942af28e6010dd4f04` 后，PR #30 的 [browser regression run36826478748](https://github.com/soaringjerry/PCAS/actions/runs/36826478748) 在 G7 三轮均失败于入站任务可见断言（行 138）。其原始服务日志持续出现 `Telegram polling will retry`，三份 G7 trace 显示 Telegram 配置 PUT 和入站 fixture/control POST 均返回 200，但随后没有任务。下载远端 artifact 核对日志/trace，并直接运行原 golden fixture：`getMe` 的实际响应是 `{"ok":true,"result":true}`，与新身份协议要求的 User 对象不符；证据 `/tmp/pcas-test-F12-getMe-before.json`、协调者保存的 `/tmp/pcas-pr30-ci-failed.log`。

协调者授权 `internal/testsupport/golden/main.go` 补协议，随后在 F11 明确释放 `web/tests/golden.spec.ts` 后，仅授权修改 G7 回调 hunk。先只补 getMe 保持旧 101 回调，本地 G7 真实失败于撤销后任务消失断言（行 146），而入站、首页展示、回执文字、Undo 按钮断言均已通过。trace 的 sendMessage 事件记录真实 `messageId=102`；旧测试携带的却是 `message_id=101`，不能命中持久回执关联。证据 `/tmp/pcas-test-F12-G7-before-callback.log`、`/tmp/pcas-test-F12-G7-before-callback-evidence.jsonl`，失败 trace 在本工作区 `web/test-results/F12-g7-before/`。

补丁只涉及 golden fixture、G7 回调和本报告，不修改真实身份校验、持久绑定或秘书产品逻辑：

- getMe 返回固定 acceptance bot 的稳定 `id=123456`、`is_bot=true` 及 User 基本字段，不随事件重置或请求顺序变化；不再让该方法落入返回布尔值的通用分支。
- 在既有 sendMessage 事件中增加 `messageId: number`，值严格等于该次 API 响应的 `message_id`；沿用原发送 ID 生成方式，不把它固定成 101，不新增另一套 fixture 状态。
- G7 从已找到的真实发送事件读取该 ID，验证安全正整数后构造 callback。回执正文/Undo 按钮、首页任务消失、snapshot 无该任务、answerCallbackQuery 等断言全部保留。F11 的 G9 文案 hunk 不在此分支改动，最终候选由 A1 合并两者后验收。

补验使用自有依赖、`18148` API / `18149` callback 端口；在 `/tmp/pcas-test-F12-browser-run.sh` 临时复制既有真实后端 runner，只调整工作区、自有 `pcas-test-F12-golden-*` 容器命名、端口及 WAL 128/32MB，未修改跟踪的 runner。容器为 1GiB tmpfs PostgreSQL，localhost 随机端口；实际 API/worker 配合本地假模型、Telegram、Push。以下结果只覆盖 G6/G7 专项，不替代 A1 的整体浏览器验收或真实 Telegram。

```sh
PATH=/root/.nvm/versions/node/v22.23.3/bin:$PATH PCAS_GOLDEN_PORT=18148 \
  bash /tmp/pcas-test-F12-browser-run.sh \
  npx playwright test tests/golden.spec.ts --grep 'G[67] ' --repeat-each=3 \
  --output=test-results/F12-g67-final --reporter=list
```

最终专项 **6/6 通过、零 skip**：G6 三轮（真实一分钟等待、首页提醒、Web Push 加密投递及 Telegram）和 G7 三轮（入站任务、回执及合法绑定撤销）。完整日志 `/tmp/pcas-test-F12-G67-final.log`；本次 fixture 变更后的 `make check` 也通过，日志 `/tmp/pcas-test-F12-browser-makecheck.log`。前端自有依赖安装及 build 通过；没有重跑或宣称整个 golden/full-browser suite 已通过，最终完整 runner 等 A1 整合 F11、F12、U1 后执行。

两次自有真实后端 runner 的 cleanup 均完成：只终止其记录的 fixture/API/worker PID 并等待退出，只删除自己的 `pcas-test-F12-golden-*` 容器和卷，临时运行目录已删除，18148/18149 无监听。单独协议复现 fixture PID 已正常终止；已清理下载的远端 artifact、临时复现 binary/fixture 目录，保留小型证明日志/JSON和本工作区浏览器证据。没有连接生产、没有真实通道调用、没有部署或合并 main。本轮补提交到原 PR #30 后再次停止写入。
