# 第 4b 批 T2 独立验收（2026-10-03）

集成基线是 `origin/phase2/batch4b` 的 `da7d4e9`，包含 E4 #132 和第 4 批修复 #134。测试分支 `phase2/b4b-T2-conversation-acceptance`，PR base 为 `phase2/batch4b`。

契约按协调者指定的 PR #129 `6a2dc94c1ca133f35b1c97326ae3e054f9229839` 中 `docs/tasks/phase2/batch4/README.md` §12、§12.1、§13.6 读取；C11 最新裁定另按 PR #129 的 `8a159a8e8467042c4362c312619af0acefb22b9e` §13.7 执行。当前集成分支的同名文件没有完整的 §12.1 和 C10–C12，测试没有用旧版本替代裁定。用户要求 E4 合入后开始，故本次测试在实现合入后编写；先单独提交契约预期 `a9488b2`，再提交测试，失败后才读实现定位。

## 全量检查

执行提交 `258eb1c18b2a0e0bbf989f30083e8ccc9669e94e`。所有新序列、批准迁移的三个旧测试、F6 与既有后端回归都在本次 `make check` 的同一执行中运行。结果：顶层测试 **430 通过、0 失败、3 个原有 live guard 跳过**；含子用例共 974 个通过项、0 个失败项、3 个跳过项。耗时 512.53 秒，`make check` 退出码为 0，`fmt-check`、`go vet`、`go test -race -count=1` 和 `go build` 全部通过。C1–C12 全部通过。

数据库是临时 PostgreSQL 16 / pgvector 0.8.2、UTF8、仅监听本机随机端口，每个测试沿用 `testStore` 独立 schema。模型均为本地 `httptest` HTTP 假模型，归档、身份和时间数据均为合成。没有读取生产 `.env` 或数据库，没有调用真实模型或通知。测试器只删除自己记录的容器 ID。

命令：

```sh
python3 /tmp/pcas-t2-isolated-go-check-20261003.py \
  /root/PCAS-wt/b4b-T2 make-check pcas-t2-4b-c11-final-
```

该环境辅助脚本为 `make check` 注入临时 `PCAS_TEST_DATABASE_URL`，通过独立 PATH 包装 Go 测试调用增加 `-json -count=1`；保留 Makefile 的 `-race -timeout 30m`，不用缓存，不修改 Makefile。常规复现入口是为自己的一次性测试库配置 `PCAS_TEST_DATABASE_URL` 后执行 `make check`。证据目录 `/tmp/pcas-t2-4b-c11-final-6h39fz3n` 的 `manifest.json` 记录执行 HEAD 和容器，`go-test.json` / `summary.json` / `make-check.log` 记录测试和命令结果，`cleanup.json` 记录容器清理。

原有三个 live guard 是 `TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`；本次没有新增跳过或禁用用例。容器清理结果 `remove_exit=0`、`exists=false`。`summary.json` SHA256：`596fe310fca1fdf397dd2fc539648c263567079088c0b6b0507cdb47ba85d3cd`。

## 序列

| 序列 | 测试名 | 核对内容 | 结果 |
|---|---|---|---|
| C1 | `TestPhase2B4b_C1_WholeConversationHasOrderedSpeakersTimesAndOneCall` | 12 条消息整段、说话人与时间、一次调用、逐消息处理记录及用量 | 通过 |
| C2 | `TestPhase2B4b_C2_FinalChoiceAndAgreementKeepUserEvidence` | 最后选择；同意长 AI 方案时只以用户同意原话为证据，地点、人可来自上文 | 通过 |
| C3 | `TestPhase2B4b_C3_AIMessageNeverBecomesEvidence` | 丢弃 AI 消息依据；混合合法用户项仍保留 | 通过 |
| C4 | `TestPhase2B4b_C4_QuoteMustBelongToItsNumberedMessage` | 丢弃别句引用与不存在的全局编号；合法项保留 | 通过 |
| C5 | `TestPhase2B4b_C5_MessageBoundarySegmentsGlobalNumbersAndOverlap` | 12000 字消息边界、全局编号、两条各 1200 字上文、上文不重复生成记忆 | 通过 |
| C6 | `TestPhase2B4b_C6_ReplacesOnlyUntouchedSystemMemoriesAndMarksHistory` | 替换未确认系统记忆；确认、用户改过的保留；真实秘书依赖回答只标 outdated，原话及回答不清空；重放无重复 | 通过 |
| C7 | `TestPhase2B4b_C7_KilledWorkerResumesWithoutReplayingCompletedSegment` | 真实子进程在第二段 HTTP 中被杀；只令该租约过期；已完成第一段不重调；恢复后无重复，进度等于 4 | 通过 |
| C8 | `TestPhase2B4b_C8_HoldPauseIncompleteBatchAndFreshInputPriority` | 先存着、暂停、未存完均不领取；完成并继续后优先级 10；新输入先领取 | 通过 |
| C9 | `TestPhase2B4b_C9_ConversationCallsAtMostOneFifthOfMessageCalls` | 同样 200 条合成消息真实逐条 HTTP 基线与整段 HTTP 对照，整段不超过 40 次 | 通过 |
| C10 | `TestPhase2B4b_C10_CrossSegmentWithdrawalCarriesOnlyThisRunsMemories` | 跨段 earlier_memories 只带本次记忆；撤回已有 ref，忽略未知 ref；确认和改过的记忆不进入清单 | 通过 |
| C11 | `TestPhase2B4b_C11_VisibleLimitsRejectHiddenQuoteAndPreserveOriginals` | AI 前 1200 字、超长用户独自正文前 12000 字、截取外引用丢弃、原话不变、两个 extractor=3 记录；只调用一次，messages 仅用户正文，context_messages 仅 AI 上文 | 通过 |
| C12 | `TestPhase2B4b_C12_OverlapCannotBeEvidenceForNewSegment` | 上文不能作为本段依据；合法本段项仍保留 | 通过 |

C2、C3、C4、C12 都包括正反用例。模型输入断言针对实际 HTTP 请求；依据同时核对消息对应 source、version、说话时间以及数据库证据定位的逐字原文。公开 excerpt 可带上下文，未把它误当作纯 quote。

## 批准迁移的旧夹具

仅改协调者点名的三个旧序列。F6 所在的 `internal/postgres/phase2_b2_queue_test.go`、第 1/2 批预期 JSON、第 1 批测试和浏览器测试均无 diff。

| 测试 | 本次迁移 | 保留的业务断言 | 全量结果 |
|---|---|---|---|
| `TestImportedHistoryNeverBecomesCurrentAction/uploaded_ChatGPT_archive` | 完成真实归档解析、开始整理、运行整段队列；假输出改成带 `message_index=1` 的计划记忆；不再向整段协议发送实时输入的 `signals` 字段 | 仍可审阅且待确认；今天的任务为 0；旧想法仍 shelved、没有 wake。原来待办建议的 `state=pending` 用整段记忆对象的 `confirmation=candidate` 检查同一待确认要求。present-day 对照的 task、signals 和断言保留 | 通过 |
| `TestPhase2B2_X13_ImportedHistoricalTimeAndNoTodayTask` | 假项加 `message_index=1`，等待批次完成后运行整段任务 | 说话时间、历史事件时间、待确认、没有当前任务和待办/想法候选的既有断言不改 | 通过 |
| `TestPhase2B2_X18_ArchiveAssistantSkippedButAvailableAsNeighborAndRaw` | 开始整理后消息入口只排整段任务；用户项 index=1，增加非法 AI 项 index=2；整理完成核对 AI 的 extractor=3 空处理记录 | AI 入口不会单独调用模型；AI 文本作为对话上下文送达；只留下用户记忆；换秘书对话仍查得到 AI 原话 | 通过 |
| `TestPhase2B2_F6_PausedArchiveSkippedOrdinaryProcessedResumeWorks` | 文件不改 | 既有断言不改 | 通过 |

## 发现与夹具修正记录

前几轮失败的原始证据保留，不把修正夹具后的结果追记为原轮通过：

- `/tmp/pcas-t2-4b-probe-c2puicn7`：导入夹具清掉尚未完成的分词任务，未排入抽取；C11 无请求时退出不充分导致越界。
- `/tmp/pcas-t2-4b-fixture-probe-9aw6c6fr`：补足真实分词准备后，发现公共 excerpt 包含上下文、老记忆模型配置准备缺失，以及 C11 的额外次数预期。
- `/tmp/pcas-t2-4b-evidence-probe-a6sty4xt`：改为核对数据库证据原文后，剩下旧归档假输出 `signals` 不属于整段格式、C6 未开启秘书使用候选计划、C7 子进程把随后产生的非抽取任务也当抽取执行。
- `/tmp/pcas-t2-4b-final-fixtures-rzocfsil`：`7e01f6d` 针对旧归档、C6、C7 的定向检查一次通过，9.34 秒。旧归档的 present-day 对照也通过。

以上改动均在测试夹具或检查证据入口，产品代码未改，原有业务要求未放宽。

C11 裁定已经落实：协调者按 PR #129 §13.7 明确纯 AI 段不调用模型，直接写处理记录；其前 1200 字符作为用户段的上文。因此本例只调用一次，唯一请求的 `messages` 只含全局 index=2 的用户前 12000 字符，`context_messages` 只含全局 index=1 的 AI 前 1200 字符，两条消息均有 extractor=3 处理记录。隐藏引用丢弃、有效记忆、逐字证据、原始 5000/15000 字文本保留的其余 C11 断言不变。

按冻结文件只增不改的约定，先提交 `4fb0da7`，追加 `ruling_8a159a8` 保存上述裁定；原 `C11.calls=2` 保留为历史冻结值，现行测试读取新增裁定的 `calls=1`，随后在 `258eb1c` 修改请求结构断言。本次只修改 C11 和冻结预期的追加项，没有产品代码或其他测试变更。

上一轮统一全量 `7abd178` 的原始证据仍在 `/tmp/pcas-t2-4b-full-3lpn5qwl`：429 个顶层通过、1 个失败、3 个原有跳过，466.22 秒，退出码 2；唯一失败为当时未获裁定的额外次数断言。没有将旧轮追记为通过。

## 范围与限制

本次是 4b 的 C1–C12 后端独立验收和三处旧夹具迁移，没有新的浏览器序列。浏览器用例未重跑，已有界面断言未改。测试只检查固定模型输出的处理、请求和队列行为，不衡量真实模型对两百条对话的语义质量或线上吞吐。
