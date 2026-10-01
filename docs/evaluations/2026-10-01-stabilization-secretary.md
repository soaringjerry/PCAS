# T3：秘书、Telegram 与删除传播独立测试

2026-10-01。执行者 `t3_secretary_tests`；分支 `stabilization/T3-secretary-tests`；候选基线 F7 `e63ee5eb5430ea8b8ebc5cefa766ae806c8fc451`，包含 main `c94b496`。PR base 为 `fix/undo-chain`，依赖 PR #20；以下结果不是 main 验收，也不是生产实测。

19 个序列编号均有可单独运行的测试。发现 8 项契约不满足，先真实失败，再暂存 8 个局部 finding skip。阶段**未通过**；有 skip 的绿色命令不能作为稳定化通过证据。D3 的快照删除传播本身通过，错误码问题与 T1-U12 为同一修复来源。

## 范围与 oracle

依据协调 worktree 当日有效的 `docs/tasks/stabilization/T3-secretary-tests.md`、`dispatch.md`、`README.md` §2.3–2.5，`docs/tasks/phase1/contracts.md` §1.4/2，以及阶段环境和任务流程规则。模型内部 JSON 与别名含义来自 B1 任务文档；渠道协议来自 C2 任务文档。只消费公开类型、schema 和既有测试夹具；没有读取被测算法来反推预期，没有改产品、共享 helpers、旧测试或配置。

只新增以下文件：

- `internal/postgres/stabilization_secretary_test.go`
- `internal/postgres/stabilization_deletion_test.go`
- `internal/telegram/stabilization_secretary_test.go`
- 本报告

复用已有 `testStore`、`workspaceCommand`、`secretaryModel`、`turnRequest`、`mustTurn`、`secretaryLogs`、Telegram `fixture`/`integrationStore` 等夹具。新增 helper 均使用 `stabilizationSecretary` 或 `stabilizationDeletion` 前缀。T1-U15 持有跨渠道撤销覆盖；这里的 Telegram T1 只测同一 update/同一 callback 的去重，没有另建跨渠道撤销夹具。

## 覆盖矩阵

下表测试名在各自包内唯一。主编号 S/D 使用 `./internal/postgres`；Telegram T 使用 `./internal/telegram`。S6 同时有后端续接和 Telegram 选项按钮用例。可用 `go test -race -count=1 <包> -run '^<测试名>$' -v` 单独运行；子用例使用 `-run '^<测试名>$/^<子名>$'`。

| 编号 | 测试名（均以 `TestStabilization` 开头） | 实际状态与覆盖边界 |
|---|---|---|
| S1 | `S1_ConcurrentConversationSeesPreviousObject` | **失败→skip T3-S1**。首轮模型保持在 barrier 后，第二轮模型先进入并返回 update R1，首轮随后释放；查最终持久化事项。真正覆盖第二模型先返回的调度，不靠短 sleep。单进程、同 owner/同 conversation；未测多实例或网络乱序。 |
| S2 | `S2_NoThisDoesNotCompleteArbitraryTask` | 通过。无 THIS 时两个既有事项均保持 todo，动作跳过且给原因；数据库事项数量不变。固定模型引用 THIS，不评价真实模型能否识别自然语言歧义。 |
| S3 | `S3_InvalidReferencesDoNotBlockLegalAction` | 通过。不存在的别名、不存在的 UUID、真实 UUID 当 ref 均跳过；独立合法 create 正常执行；原事项标题不变，数据库只有两个事项。 |
| S4 | `S4_IdempotencyAndFreshRequest` | 通过。同 requestId 不重调模型、不重建事项；换正文冲突；新 requestId 相同正文创建第二事项；验证 desk_turns/action_log 各两条。 |
| S5 | `S5_AskAndActionsBothSurvive` | 通过。ask/question/options 与 create 回执一起存在，数据库动作执行。 |
| S6 | `S6_AnswerOptionContinuesPreviousObject`；Telegram `S6_TelegramOptionContinuesSameObject` | 通过。直接传选项及实际 a:0 callback 都修改上一轮同一事项，R1 可用；同提示再次点 a:1 不执行。未测网页 DOM 点击或真手机。 |
| S7 | `S7_ItemPageUsesOnlyCurrentThing` | 通过。ThingID/THIS 只完成当前事项，另一个保持 todo；数据库数量不变。未测网页事项页组件。 |
| S8 | `S8_FailureCategoriesKeepOriginalAndPrivateLogs` | 部分通过；`budget`/`500`/`plain` 通过，`timeout` **失败→skip T3-S8**。分别检查用户文案、日志 stage/error_type、原话 source_versions 正文、无模型密钥/原始错误进入响应或日志，以及重放不调模型。超时使用 HTTP client deadline；未测真实模型账户。 |
| S9 | `S9_LongReplyTruncatesWithoutLosingActions` | **失败→skip T3-S9**。2500 个汉字未截断、无提示；真实失败执行时事项和 action 回执仍成功，缓存不带 state。允许截断正文后附提示，不规定具体分隔符。 |
| Telegram T1 | `TelegramT1_DuplicateUpdateExecutesOnce`；`TelegramT1_DuplicateUndoCallbackExecutesOnce` | 通过。重复文字 update、重新加载持久进度后的重复投递都只执行一次；合法回执的重复 undo callback 只撤一次、只答一次；查真实数据库及模型调用次数。与 T1-U15 的网页→Telegram 已撤销提示不同。 |
| Telegram T2 | `TelegramT2_NewBotResetsProgressAndConversation` | 部分通过。`lower_update_id` 通过：offset/会话重置，旧 writer 被拒，新会话处理低 ID；`colliding_message_id` **失败→skip T3-T2**：重置本身通过，但新 bot 与旧 bot 的 chat/message ID 相同时新消息漏执行。 |
| Telegram T3 | `TelegramT3_UnauthorizedAndForgedCallbacksHaveNoEffects` | 部分通过。其他 chat、group、伪造 From.ID、非法 UUID callback 均无业务影响；`forged_receipt` **失败→skip T3-T3**。用正确 chat/From 携带合法 actionId，但 messageId=999 从未发送回执，实际 Undo 生效。缺少可验证的 message/receipt→action 绑定；合法回执 Undo 在 Telegram T1 通过，跨渠道边界由 T1-U15 测。 |
| Telegram T4 | `TelegramT4_LongReplyFitsBotLimit` | 通过。Bot HTTP 假服务主动拒绝超过 4096 字的请求；9000 字 reply 能成功发送。使用 fakeStore，只测渠道格式/HTTP 长度和一次请求，数据库动作去重在 T1/S4 覆盖。未穷举极长 ask、全部 cards 或补充平面 Unicode。 |
| Telegram T5 | `TelegramT5_UnconfiguredTranscriptionPreservesAudio` | 通过。无转录配置时提示配置，原始 audio/ogg 附件写入真实数据库/临时 blob 文件；重投不新增来源，不创建虚假事项。音频是本地 fixture 字节，未测真实录音格式或真转录服务。 |
| Telegram T6 | `TelegramT6_RestartAfterCommittedTurnBeforeDelivery` | 部分通过。`text` 通过：已提交而送达失败的文字，丢弃 transient state 后恢复旧回执，不重调模型，并能处理下一条；`voice` **失败→skip T3-T6**：重转录两次，不同文本导致请求冲突，原回执不再送达。模拟新 poller 丢弃全部 transient cache，未实际杀进程或测机器崩溃。 |
| D1 | `D1_ReferencedMemoryDeletionClearsWholeTurn` | **失败→skip T3-D1**。删除已授权且回答引用的 claim（IncludeSources=false）后，claim 从 State 消失，但 question/answer/response 和历史仍保留该轮正文；重放未仅保留回执骨架。测试不把独立 source 里依法保留的原文视作泄漏。 |
| D2 | `D2_OriginalSourceDeletionClearsWholeTurn` | 通过。删除 desk 原话来源后 question/answer/cache 清理，history 不含原话/追问，cache 不带 state。 |
| D3 | `D3_DeletedSourceExpiresRelatedUndoSnapshot` | 部分通过。来源关联的事项被修改后，删除来源确实令 changes=[]、expired_at 非空；`expired_code` **失败→skip T3-D3/T1-U12**：HTTP 返回 409 changed_since，契约要求 expired。只使用一条最小来源关联事项，不复制 T1 撤销序列。 |
| D4 | `D4_ReplayCannotRestoreDeletedOriginal` | 通过。成功 desk 与失败 capture 两条路径都验证：删除来源后相同 requestId 返回旧 ID 的清理后轮次、不重调模型、不重复事项、不让被删原话进入完整响应/日志；再次检查原始 desk_turns cache。 |

## 真实失败与修复入口

以下是修正夹具后、未设置 skip 时的观察，之后用诊断开关保留同一组断言再次复现。最早的夹具错误（把 task kind 从 document 取、模型默认 agent 未配置、未显式设置 S1 时区）已修正，不列为产品发现。

| finding | 复现与实际结果 | 契约预期 / 已通过的邻近边界 |
|---|---|---|
| T3-S1 | 首轮模型阻塞；第二模型在首轮释放前进入；回执为 skipped「找不到要改的那件事」；第二轮 state tasks=0；首轮最终写入一件 due=15:00 的事项。 | 同 conversation 串行，后一轮用 R1 把同一事项改为 16:00；没有重复事项。原先仅让第二 goroutine 就绪即释放首轮的弱调度曾通过，不能证明串行，已替换。 |
| T3-S8 | HTTP client 200ms deadline 导致超时；日志 `stage=model error_type=model_error`；用户回执「已记下原话；模型没有响应，稍后会自动整理」。 | 超时应与 500 分别分类、分别给文案。原话保存、模型错误正文/密钥脱敏、无重复执行已通过。 |
| T3-S9 | `reply` rune 数=2500，`containsNotice=false`。 | 超过 2000 字截断并提示「回答太长，已截断」；动作仍执行（实际已通过）。 |
| T3-T2 | 两个不同 bot token，同 chat=123、message_id=1；新 offset/会话已重置并更新到 2，但 persisted task count=1，模型只处理旧消息。 | 新 bot 消息独立执行，应该两件事项；不碰撞的低 update ID 已通过。无法从此测试推断真实 Telegram 为不同 bot 分配 ID 的概率。 |
| T3-T3 | 已发送回执 message=101 的合法 actionId，放入伪造 callback message=999；chat/From 均=123；事项数量从 1 变成 0。 | 忽略未与真实回执 message/receipt 绑定的 action；chat 和 From 授权过滤已通过。未规定保存绑定的实现方式。 |
| T3-T6 | 第一次语音转录「明天下午三点开会」，事务提交并创建一件事；sendMessage 500，offset=0；fresh state 重启再次转录成「不一样的转写」，转录调用数=2；最后回执为「这条消息已经处理过，请在首页查看回执」。 | 重启恢复已提交的原始输入/回执，转录只需一次；模型仍只调用一次、数据库没有重复事项、后续文字能处理，但恢复送达失败。 |
| T3-D1 | claim 已删；raw desk_turns 检查 `questionEmpty=false answerEmpty=false cachedSecret=true`；同 requestId replay 和 DeskTurns 都还含本轮正文。 | 清掉依赖已删除 claim 的整轮内容，回执只留骨架；IncludeSources=false 不是保留依赖 claim 的生成正文的许可。独立 source 原文是否继续保存不是本 finding。 |
| T3-D3/T1-U12 | 已确认 changes=[]、expired_at 非空，HTTP undo 返回 `409 changed_since`。 | 返回 `409 expired`。这是 T1-U12 同一错误码问题，不登记两个独立修复。`newer_action`/普通 `changed_since` 歧义没有参与此预期。 |

协调者已收到以上发现；本任务没有顺手修复，也没有改 F9 持有的产品文件。修复者应删除对应 skip 再跑诊断断言和相关专项；是否通过必须以零 skip 为准。

## 命令与证据

使用自有 `pcas-test-T3-secretary`，镜像 `pgvector/pgvector:0.8.2-pg16-bookworm`；PG data 在 1GiB tmpfs，max/min WAL 为 128/32MB，随机 localhost 端口 33250。owner、schema、临时通知设置和 blob 目录由夹具隔离。只连接该测试库，所有模型、Bot API、转录返回值均为本地假服务，没有生产连接或真实账号调用。

```sh
# 每个执行环境须自行替换为自建 disposable PG DSN。
export PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33250/postgres?sslmode=disable'

# 所有编号专项：保留局部 finding skip；退出 0 不代表阶段通过。
go test -race -p=1 -count=1 ./internal/postgres ./internal/telegram -run '^TestStabilization' -v

# 原样复现 8 项失败；预期退出 1，不删除/改弱断言。
PCAS_STABILIZATION_RUN_FINDINGS=1 go test -race -p=1 -count=1 \
  ./internal/postgres ./internal/telegram \
  -run '^TestStabilization(S1|S8|S9|D1|D3|TelegramT[236])' -v

# 普通检查不运行数据库；数据库验收另跑自己的 PG。
env -u PCAS_TEST_DATABASE_URL -u PCAS_STABILIZATION_RUN_FINDINGS make check
make test-integration
```

专项包使用 `-p=1`，避免首次同时创建 PostgreSQL extension 的环境竞争。失败、skip 与结果分别来自本地日志：`/tmp/pcas-test-T3-final-failure.log`、`/tmp/pcas-test-T3-evidence-private.log`、`/tmp/pcas-test-T3-evidence-claim.log`、`/tmp/pcas-test-T3-evidence-propagation.log`、`/tmp/pcas-test-T3-reproduce-findings.log`、`/tmp/pcas-test-T3-with-findings-skipped.log`。日志是当前机器的复现辅助，永久证据为上面的最小复现、实际结果和提交中的断言。

最终实测结果：

- `make check` 退出 0：fmt-check、go vet、Go race 单测和 build 通过；数据库未由这个命令验收。
- 自有数据库上 `make test-integration` 退出 0，PostgreSQL 全包 race 测试用时 63.921s；已包含局部 finding skip，所以不代表零 skip 验收。
- 最终编号专项退出 0，明确报告 **8 个 finding skip**，其余可执行分支通过；日志 `/tmp/pcas-test-T3-final-targeted.log`。
- `PCAS_STABILIZATION_RUN_FINDINGS=1` 诊断专项退出 1，8 个发现均保留真实失败；最后增加响应脱敏断言后 S8 专项仍仅 timeout 分支失败，预算/500/纯文本通过。
- 自有 `pcas-test-T3-secretary` 已执行 `docker rm -f -v`，确认容器不存在；所有 httptest 服务和临时通知/blob 文件由测试 cleanup 释放，没有持久后台进程。没有清理或停止其他任务/生产资源。少量 `/tmp/pcas-test-T3-*.log` 留作当前协调者查看失败证据，不是大体积缓存。

真实 Telegram/手机语音、默认 Codex 账户、生产配置、真机通知、网页交互、线上 11 项与一天试用均未验收。没有部署、没有合并、没有 main 推送。
