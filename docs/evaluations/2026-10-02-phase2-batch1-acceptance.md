# 第 2 阶段第 1 批：38a8956 裁定后统一全量验收

**本轮按契约验收通过。** 38 条序列全部通过，44 个编号 Go 主测试全部通过；全部浏览器 **126/126 通过**（118 个既有 + 本批 8 个）。`make check` 通过，未出现 DATA RACE。稳定前缀、四项日期测试、I-A3 长资料、N12 的 20 个固定种子以及 Brief 跨日时区对照均通过。

协调者已裁定上一轮 F-T1/F-T2 为夹具前提不完整。本轮仅按批准补齐候选采纳和查询相关性；全部既有断言、冻结文件及产品代码保持原样，未开启「包含推测」。结果来自补齐后同一个提交的一次统一全量，没有拼接上一轮结果、定向复跑或浏览器重试。

上一轮红色结果保留在 [12abe94 报告历史版本](https://github.com/soaringjerry/PCAS/blob/65c68660fd086cdbab3e5093625744ef6a3f3645/docs/evaluations/2026-10-02-phase2-batch1-acceptance.md)，当前报告仅列本轮实际结果。

## 被测提交与运行边界

- 集成分支提交：`38a8956da2f9dbd02f53344a69bd09a18630e6ec`（38a8956；含 A #60/#68/#72、B #58、C #59、C2 #65、D #74）。
- 集成 38a8956 相对 12abe94 只多裁定和待办文档，产品代码无变化。
- 含 T 验收测试的实际被测提交：`d283a8064dda811b86817431a729a021fddf582d`（d283a80）。Go、全部浏览器、前端静态检查和构建都来自此提交；运行期间工作树保持干净。报告更新后没有修改测试或产品代码。
- 证据元数据记录区间：`2026-10-02T12:32:15.771630+00:00` 至 `2026-10-02T12:39:01.600965+00:00`。这是一次统一全量，未拼接 12abe94、8a4ac50 或此前定向结果。已清除 Go 测试缓存；浏览器每个用例只执行一次、`--retries=0`，黄金路径没有 repeat-each。
- 冻结文件 [b1-gold.json](../../testdata/phase2/b1-gold.json) 与前次交付 254b0c0 逐字一致，SHA256：`cbddbb479212d7a0dd48850c708262497732167ef7aef5ae6ee21ef8e6fa67ec`。
- Draft PR：[T 独立验收 #64](https://github.com/soaringjerry/PCAS/pull/64)，base 为 `phase2/batch1`。

仅在 `/root/PCAS-wt/b1-T` 执行。两套临时 PostgreSQL 均使用 `pgvector/pgvector:0.8.2-pg16-bookworm`、tmpfs、随机 loopback 端口。浏览器 API 也使用随机本机端口；订阅协议用例使用测试 runner 的固定回调端口 14559，启动前已检查空闲。资料、输出、模型 HTTP、Telegram、Web Push 及协议服务全部为合成或本地假服务。没有读取线上 `.env`/`config/`、连接线上数据库、调用真实模型或发送真实通知。仅按记录的容器 ID 和子进程 PID 清理；运行后已确认两个容器移除、四个服务进程退出。

## 38 条逐条结果

| 序列 | 本轮结果 | 对应测试名及结果 | 补充 |
|---|---|---|---|
| P1 | 通过 | `TestPhase2B1_P1_DeskTurnRawSourceAndDeletionControl`（通过） |  |
| P2 | 通过 | `TestPhase2B1_P2_ActualDeputyRequestAndSourceDependencies`（通过） |  |
| P3 | 通过 | `TestPhase2B1_P3_ManualBriefAndDeletionControl`（通过）<br>`TestPhase2B1_P3_BriefDateUsesWorkspaceTimezoneAcrossUTCDayBoundary`（通过） | 含上海/UTC 跨日：表达日期、记录日期，manual Brief 和副手实际请求。 |
| P4 | 通过 | `TestPhase2B1_P4_ClaimOnlyEmptySourceSection`（通过） |  |
| P5 | 通过 | `TestPhase2B1_P5_SystemGeneratedSourcesExcluded`（通过） |  |
| P6 | 通过 | `TestPhase2B1_P6_BudgetsTruncateAndComplete`（通过）<br>`TestPhase2B1_P6_MatchedTailExcerptBeforeAndAfterChunking`（通过） |  |
| P7 | 通过 | `TestPhase2B1_P7_HistoricalQuestionsNotRepeatedAsSources`（通过） |  |
| P8 | 通过 | `TestPhase2B1_P8_DuplicateTitlesDoNotMergeIdentities`（通过） |  |
| P9 | 通过 | `TestPhase2B1_P9_PublicReadsCannotForgeInternalAccess`（通过） |  |
| P10 | 通过 | `TestPhase2B1_P10_DeputyClaimFiltersAndSharedRawSources`（通过）<br>`TestPhase2B1_P10_ExclusionsInferenceAndProjectFiltersStayOnClaimsOnly`（通过） |  |
| P11 | 通过 | `TestPhase2B1_P11_CurrentVersionOnly`（通过） |  |
| P12 | 通过 | `TestPhase2B1_P12_MediaRequiresReadableTranscriptOrOCR`（通过） |  |
| P13 | 通过 | `TestPhase2B1_P13_AdoptedSourceContentSurvivesForUserButExpiresForModels`（通过） |  |
| P14 | 通过 | `TestPhase2B1_P14_CapturedMemoryVisibilityClosesAndReopensOriginal`（通过）<br>`TestPhase2B1_P14_OneRestrictedActiveClaimClosesWholeSource`（通过） | 速记采纳及多陈述原话关闭、重新打开、删除受限陈述对照全部通过。 |
| P15 | 通过 | `TestPhase2B1_P15_ItemExclusionAffectsOnlyThatItemAcrossAllEntrances`（通过）<br>`TestPhase2B1_P15_SourceDependentAdoptionObeysVisibilityAndItemExclusion`（通过） | 多陈述原话事项隔离、三入口、source 依赖采纳和恢复全部通过。 |
| M1 | 通过 | `TestPhase2B1_M1_ClaimCorrectionKeepsVisibleExchange`（通过） |  |
| M2 | 通过 | `TestPhase2B1_M2_ModelHistoryReplacesOnlyOldAnswer`（通过）<br>`TestPhase2B1_M2_CurrentDestinationUnavailablePreservesQuestionAndReceipt`（通过） | 秘书历史回执行及显式 deskTurnIds 副手历史对照通过。 |
| M3 | 通过 | `TestPhase2B1_M3_SourceVersionMarksOutdatedWithoutErasure`（通过） |  |
| M4 | 通过 | `TestPhase2B1_M4_SourceDeletionScrubsDependentTurnRunAndDoc`（通过） |  |
| M5 | 通过 | `TestPhase2B1_M5_OutdatedReceiptCanUndoAndSurvivesReload`（通过） |  |
| M6 | 通过 | `TestPhase2B1_M6_ReplayAndTelegramDuplicateKeepOldAnswer`（通过） |  |
| M7 | 通过 | `TestPhase2B1_M7_CorrectedDeputyResultRemainsButStale`（通过） |  |
| M8 | 通过 | `TestPhase2B1_M8_UnchangedTurnOmitsOutdatedField`（通过） |  |
| N1 | 通过 | `TestPhase2B1_N1_UndoDeletesPlanKeepsOriginalAndHistory`（通过） |  |
| N2 | 通过 | `TestPhase2B1_N2_ExtractionAfterFullUndoDoesNotRecreatePlan`（通过） |  |
| N3 | 通过 | `TestPhase2B1_N3_PartialUndoKeepsMemoryAndRawSupply`（通过） |  |
| N4 | 通过 | `TestPhase2B1_N4_RememberKeepsPreferenceButDeletesPlan`（通过） |  |
| N5 | 通过 | `TestPhase2B1_N5_ConfirmedClaimSurvivesFullUndo`（通过） |  |
| N6 | 通过 | `TestPhase2B1_N6_IndependentEvidenceProtectsClaim`（通过） |  |
| N7 | 通过 | `TestPhase2B1_N7_RefusedUndoIsAtomic`（通过） |  |
| N8 | 通过 | `TestPhase2B1_N8_TelegramCallbackUsesRealUndo`（通过） |  |
| N9 | 通过 | `TestPhase2B1_N9_UndoCommandReplayHasNoSecondDeletion`（通过） |  |
| N10 | 通过 | `TestPhase2B1_N10_ManualUndoDoesNotCascadeSecretaryMemory`（通过） |  |
| N11 | 通过 | `TestPhase2B1_N11_UndoUpdateThenCreationDeletesOnlyCompleteTurn`（通过） |  |
| N12 | 通过 | `TestPhase2B1_N12_RandomTwentyActionsInTwentySeededGroups`（通过） | 20 个固定种子、每组 20 个动作逆序撤销均通过。 |
| G1 | 通过 | `TestPhase2B1_G1_FreshDatabaseMigratesWithoutLegacyObjects`（通过） |  |
| G2 | 通过 | `TestPhase2B1_G2_LegacyCleanupPreservesEveryBusinessRow`（通过） |  |
| G3 | 通过 | `TestPhase2B1_G3_SecondStartupMakesNoFurtherChanges`（通过） |  |

N12 的种子为 20261001–20261020，共 400 个秘书动作及 400 次逆序撤销。每轮最后一个动作撤销后，该轮计划记忆删除、其他轮计划保留；最后严格比较业务字段，动作/撤销来源条目及操作版本按裁定排除，真实原话与对话记录保留。20/20 组全部通过。

## 浏览器结果

| 文件 | 本批用例名 | 结果 |
|---|---|---|
| `phase2-batch1-backend.spec.ts` | P1/P8 页面原话→新对话→真实请求与来源卡片，删除后不再供给 | 通过 |
| `phase2-batch1-backend.spec.ts` | P6/R8a 带换行的长资料：实际中段摘录能标记并定位在可见区域 | 通过 |
| `phase2-batch1-backend.spec.ts` | P6/R8a 带换行的长资料唯一中段查询对照：实际中段摘录能标记并定位在可见区域 | 通过 |
| `phase2-batch1.spec.ts` | P1/R8a 单击依据直接看到全文、定位标记，面板无版本状态摘要 | 通过 |
| `phase2-batch1.spec.ts` | M1/M3/M5 依据已更新保留回答卡片回执，撤销刷新后仍在 | 通过 |
| `phase2-batch1.spec.ts` | M8/P1 正常轮无更新标记，原话不混入时间轴，390px不溢出 | 通过 |
| `phase2-batch1.spec.ts` | P1/R8a 摘录不在原文时全文仍自动可见，没有伪标记 | 通过 |
| `phase2-batch1.spec.ts` | P1/R8a 资料库打开同一原话保留版本、处理状态、摘要及展开原文 | 通过 |

真实长资料保留换行、超过 600 字符。宽泛提问和唯一中段词对照都检查假模型实际请求、中段细节、后端摘录、全文逐字一致、唯一 mark 及可见区域；两条均通过，I-A3 已在本轮验证通过。短资料摘录等于全文时，全文直接可见且无 mark；删除后的实际模型请求对照也完整执行并通过。模拟用例验证时间轴与依据分离、outdated 回执撤销、刷新、资料库入口不变及 390px 不溢出。

全部浏览器按文件统计如下（118 个既有 + 8 个本批 = 126）：

| 文件 | 用例数 | 结果 |
|---|---|---|
| `backend.spec.ts` | 1 | 全部通过 |
| `buttons.spec.ts` | 9 | 全部通过 |
| `chatgpt-direct.spec.ts` | 1 | 全部通过 |
| `continuity.spec.ts` | 1 | 全部通过 |
| `fixes.spec.ts` | 12 | 全部通过 |
| `golden.spec.ts` | 12 | 全部通过 |
| `model-api.spec.ts` | 1 | 全部通过 |
| `notify.spec.ts` | 5 | 全部通过 |
| `phase2-batch1-backend.spec.ts` | 3 | 全部通过 |
| `phase2-batch1.spec.ts` | 5 | 全部通过 |
| `secretary.spec.ts` | 18 | 全部通过 |
| `settings-things-ux.spec.ts` | 19 | 全部通过 |
| `timezone-backend.spec.ts` | 1 | 全部通过 |
| `timezone.spec.ts` | 18 | 全部通过 |
| `usability-acceptance.spec.ts` | 20 | 全部通过 |

首次登录时区测试先在空工作区执行，其余 125 个用例在同一套临时真实后端执行；模拟文件使用自己的浏览器路由夹具。Playwright JSON 显示全部用例只有一个 `passed` 结果，没有重试或 flaky 结果。

## 发现清单与上一轮裁定闭环

本轮没有未解决的本批发现，所有原断言均通过。上一轮两项已裁定为夹具问题，闭环如下：

| 编号 | 批准的夹具补齐 | 本轮结果 |
|---|---|---|
| F-T1 | P14/P15 的两条记忆先从同一 manual 原话抽为待审候选，再逐条使用真实 `workspaceCommand(acceptCandidate, kind=memory)` 采纳。来源关联保留，两条记忆引用从采纳后快照取；不调整 agent 的 IncludeInferred，不修改任何原供给、依赖、关闭、恢复或删除断言。 | `TestPhase2B1_P14_OneRestrictedActiveClaimClosesWholeSource`、`TestPhase2B1_P15_ItemExclusionAffectsOnlyThatItemAcrossAllEntrances` 全部通过；同一原话多条记忆及未受限陈述的正向供给现已完整验证。 |
| F-T2 | 三个子用例共享同一个交接请求 `Continue the discussion about C-8420`，其中 C-8420 来自资料，检索前提一致。仅项目/排除状态不同。请求不包含完整陈述标记，避免本次请求正文干扰原话节的否定断言。 | `TestDeskHistoryCannotBypassDestinationItemScope` 的 exclude、other-project、delegate-other-project 全部通过；前者原话不在，后两者原话在，旧回答/陈述隔离、提问和替换语断言全部通过。 |

授权来源为集成 38a8956 的 T-acceptance.md「协调者对 12abe94 统一全量一轮的裁定」及用户本轮指令。补齐先提交，再变基到 38a8956，之后固定 d283a80 统一执行。对两份改动文件做了字节归一比对：去掉新增的采纳夹具 helper、还原两个 helper 调用及一个请求字符串后，与上一交付 65c6866 完全一致，原断言一个字未动。

## 用户指定复验项与历史裁定

| 项目 | 本轮结果 |
|---|---|
| `TestSecretaryStablePrefixAndVisibility` | 原文件一个字未改；通过 |
| `TestCodexSecretaryAndLegacyFormats` | 通过 |
| `TestSecretaryHistoryOneEntryPerAction` | 通过 |
| `TestSecretarySchedulesAndUpdatesRecentTask` | 通过 |
| `TestDueReminderOffsets` | 通过 |
| I-A1 秘书历史回执行（#68） | M2 秘书断言通过；副手回执断言已按裁定去掉 |
| I-A2 秘书 delegate 历史 | 按裁定去掉批外预期，显式 deskTurnIds 的问答替换、依赖和实际请求通过 |
| I-N1 N12 操作来源比较 | 按裁定只排除操作记录；20 个种子全部通过 |
| I-C2-1 全文摘录无标记 | 真实短资料全文可见、mark=0，通过 |
| I-A3 摘录窗口 | 宽泛问题、长资料及断言原样保留，本轮通过 |
| Brief 跨 UTC 日界时区 | 表达时间/记录时间均以 2026-09-12T17:30Z 对照：上海 9 月 13 日、UTC 9 月 12 日；manual 与副手实际请求通过 |

四项固定日期旧问题由 D #74 修复，本轮均通过，不计本批发现。T 的 N11 使用上海当前日期后七天的 15/16 时，未写死必须在未来的日期；本轮没有采用新 DateFromToday helper 或为此改动测试。

## 获批的旧测试与冻结预期变更

这些变更均在本轮运行前交付，本轮没有改动任何断言或 gold 值：

1. `desk_dependency_growth_test.go::TestSecretaryConversationDependenciesStayASet`：批准把依赖改为供给陈述 + 原话的精确去重集合，保留对话增长和旧结构读取保护；本轮通过。
2. `ux_regressions_test.go::TestDeskHistoryCannotBypassDestinationItemScope`：批准把提问/陈述/旧回答拆为不同标记，检查记忆部分与 ContextMemoryIDs，保留问题和替换语；按 R2a 增加 exclude 原话关闭、两个仅项目不同的原话开放对照。本轮三子用例全部通过；本轮按裁定补齐同一个相关查询词，断言未动，见 F-T2 闭环。
3. `TestSecretaryStablePrefixAndVisibility`：从未修改，本轮通过。
4. 协调者在 079151e 批准的三个 gold 旧字符串修正：R7 两条回执行描述收窄到秘书；R8a 增加全文例外。`coordinator_adjudication_079151e.authorized_corrections` 保存路径、前值、新值和授权。其他旧值保留；P14/P15、跨日日期预期均先冻结提交，再写测试。

## 检查命令与证据

- `GOFLAGS=-v bash testdata/phase2/run-go.sh make check`：**通过，退出 0**。fmt-check、go vet、`go test -race ./cmd/... ./internal/...`、`go build -trimpath -o bin/pcas ./cmd/pcas` 全部完成。未出现 DATA RACE。
- Go 主测试日志汇总：342 通过、0 失败、3 个既有真实服务 opt-in 未执行。44 个 T 主测试全部执行、零 skip；38 个编号都存在。
- 前端 `npm run lint && npm run type-check && npm run build`：全部通过（Node 22.23.3）。
- `bash web/tests/support/real-backend.sh bash /tmp/pcas-b1-T-final-38a8956-browser.sh`：通过，先 timezone-backend，再其他全部文件，`--retries=0`，一次运行。
- 冻结文件字节比对、测试编号集合和 git diff whitespace 检查通过；本轮结束时仅验收报告有后续编辑，没有产品或测试代码修改。

本地证据路径只含合成资料；不提交大日志或临时 token：

- `/tmp/pcas-b1-T-final-38a8956-meta.json`：统一提交、冻结 SHA256、运行区间、自己起的资源与清理确认。
- `/tmp/pcas-b1-T-final-38a8956-go.log`、`...-go-results.json`：完整 make check 输出及主测试结果；`...-frontend-build.log`：前端构建输出，lint/type-check 输出也在本轮工具记录中。
- `/tmp/pcas-b1-T-final-38a8956-browser.log`、`...-timezone.json`、`...-browser.json`、`...-browser-results.json`：浏览器输出、原始 JSON 及逐例状态。
- `/tmp/pcas-b1-T-final-38a8956-browser/`、`...-timezone/`：截图及长资料摘录附件；`...-services/`：本轮临时服务日志。

既有三个真实服务 opt-in：`TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`，按原有机制未执行。runner 清除了真实服务 opt-in 配置；T 没有新增 skip，也不把这三项算作真实模型验收通过。

## 规则与入口对应

| 规则 | 验收入口 / 定位 |
|---|---|
| R1/R2/R3 | DeskTurn 实际模型请求、requestRun + runAgentOnce 实际请求、manual Brief；retrieval.go |
| R2a | P14/P15 三入口、历史依赖、source 依赖采纳对照；teamSourceVisibleSQL、verifyRunForItemTx、sanitizeItemTx |
| R4/R5 | 实际提示词、Brief、desk_turns.dependencies、run.contextVersions 和 run_dependencies |
| R6 | P13 采纳及原话新版本、M4 删除传播；artifacts.go 与运行依赖校验 |
| R7 | M1–M8、显式 deskTurnIds、重放与本地 Telegram 假回调；desk_turn.go、run_context.go |
| R8/R8a | 两个浏览器文件、实际请求捕获、长资料标记可见；SecretaryCards / SourceSheet |
| R9 | Store.Undo、undoAction、ProcessExtraction、N12 固定种子；undone_turns.go、actions_log.go |
| R10 | 空库、遗留 2.0 对象及无关业务行、再次 Migrate；迁移 026 与 G1–G3 |

## 未覆盖

38 条序列、两组浏览器和全部既有回归在本轮均已执行；这是契约限定范围内的通过，外部真实能力仍有下列边界。

假模型证明系统实际把哪些内容交给模型，以及处理固定引用/动作的行为；不能证明真实模型会如何作答。没有验证真实向量语义质量、真实 OCR/转录准确率、大规模性能、线上默认 Codex 通道、真实提醒到手机、部署或线上冒烟。浏览器通知权限为 denied 时黄金用例走既有合成订阅回退，只证明临时后端向假端点投递。未合并、部署或操作线上实例。
