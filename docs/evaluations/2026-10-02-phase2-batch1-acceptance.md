# 第 2 阶段第 1 批：12abe94 统一全量验收

**本轮未达到全绿，不能签署验收通过。** 按编号下任一主测试失败即失败汇总，38 条序列为 **36 通过、2 失败（P14、P15）**；44 个编号 Go 主测试为 **42 通过、2 失败**。另外一组获批修改的旧测试失败，`make check` 退出码为 2。全部浏览器 **126/126 通过**，包括本批 8 个用例及 I-A3 长资料用例。

失败证据集中在 T 的夹具对照：两项新增测试期待 manual 抽取的候选陈述被供给；旧测试期待与当前查询无相关词的资料进入跨项目交接。原话本身的 R2a 隐藏、事项排除、恢复断言均通过。静态定位提示夹具前提不完整，详见发现清单；本轮没有将这些失败认定为已证实产品缺陷，也没有删断言、改预期或重跑。

## 被测提交与运行边界

- 集成分支提交：`12abe94e7541713d164e476a4dffecfc85a27992`（12abe94；含 A #60/#68/#72、B #58、C #59、C2 #65、D #74）。
- 含 T 验收测试的实际被测提交：`60bde6d25466a2ac4997bae5e422c0385d3b432e`（60bde6d）。Go、全部浏览器、前端静态检查和构建都来自此提交；运行期间工作树保持干净。报告更新后没有修改测试或产品代码。
- 证据元数据记录区间：`2026-10-02T10:48:41.180406+00:00` 至 `2026-10-02T10:55:19.915484+00:00`。这是一次统一全量，未拼接 8a4ac50 或此前定向结果。已清除 Go 测试缓存；浏览器每个用例只执行一次、`--retries=0`，黄金路径没有 repeat-each。
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
| P14 | 失败 | `TestPhase2B1_P14_CapturedMemoryVisibilityClosesAndReopensOriginal`（通过）<br>`TestPhase2B1_P14_OneRestrictedActiveClaimClosesWholeSource`（失败） | 速记采纳主路径通过；多陈述原话控制因陈述供给断言失败，见 F-T1。 |
| P15 | 失败 | `TestPhase2B1_P15_ItemExclusionAffectsOnlyThatItemAcrossAllEntrances`（失败）<br>`TestPhase2B1_P15_SourceDependentAdoptionObeysVisibilityAndItemExclusion`（通过） | 原话事项隔离和 source 依赖采纳控制通过；多陈述供给断言失败，见 F-T1。 |
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

## 发现清单

| 编号 | 现象与冻结预期 | 定位及建议归属 |
|---|---|---|
| F-T1 | `P15_ItemExclusionAffectsOnlyThatItemAcrossAllEntrances` 和 `P14_OneRestrictedActiveClaimClosesWholeSource` 期待未受限的两条陈述出现在 Brief/实际模型正文及依赖中；实际没有陈述，相关 claim 依赖计数为 0。原话隐藏、排除、恢复及 source 依赖检查没有失败。 | **T 的夹具前提问题，交协调者核定。** 夹具从 manual 来源抽取，正文与 quote 分别使用记忆标记和原话；未执行采纳/确认，也未开启副手包含推测。静态定位：`processing.go::extractionConfirmation` 对此返回 candidate；`claims.go::memoriesTx` 将其映射为 inferred；秘书/副手的现有过滤排除 inferred。R1/R2a 明确保留陈述过滤规则。可补齐符合原冻结供给前提的夹具状态，断言和原话标记保持；本轮未调整、未复跑。不能据这两项认定 R2a 产品缺陷。 |
| F-T2 | 获批旧测试 `TestDeskHistoryCannotBypassDestinationItemScope` 的 other-project、delegate-other-project 两子用例期待「相关原话」含原件；实际原话节为空。exclude 子用例、提问保留、旧回答替换和陈述排除均通过。 | **T 的检索对照夹具问题，交协调者核定。** 新增断言的资料为 `Scope claim marker C-8420`，交接请求为 `Continue the discussion`，事项为 Destination/New work，假服务未配置 embedding。静态定位：`prepareRunContext`/`requestRun` 检索以当前请求和事项为查询，不把历史提问加入查询；本对照缺相关词，不能区分「按项目过滤」与「没有检索命中」。建议补齐相关查询的对照前提；本轮保留失败，不改断言、不重跑。 |

以上定位发生在冻结预期、统一运行之后，只用于解释失败，没有据产品实现改写预期。失败仍计入本轮结果；这是一份红色验收报告，不把静态解释当作通过证据。未发现其他已证实的本批产品缺陷。

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
2. `ux_regressions_test.go::TestDeskHistoryCannotBypassDestinationItemScope`：批准把提问/陈述/旧回答拆为不同标记，检查记忆部分与 ContextMemoryIDs，保留问题和替换语；按 R2a 增加 exclude 原话关闭、两个仅项目不同的原话开放对照。本轮 exclude 通过，另两子用例失败，见 F-T2。
3. `TestSecretaryStablePrefixAndVisibility`：从未修改，本轮通过。
4. 协调者在 079151e 批准的三个 gold 旧字符串修正：R7 两条回执行描述收窄到秘书；R8a 增加全文例外。`coordinator_adjudication_079151e.authorized_corrections` 保存路径、前值、新值和授权。其他旧值保留；P14/P15、跨日日期预期均先冻结提交，再写测试。

## 检查命令与证据

- `GOFLAGS=-v bash testdata/phase2/run-go.sh make check`：**失败，退出 2**。fmt-check 和 go vet 通过；`go test -race ./cmd/... ./internal/...` 有三个主测试失败（F-T1 两个、F-T2 一个），其余完成，未出现 DATA RACE。原有 build 目标因 test 失败未执行；浏览器 runner 从同一提交成功构建真实后端二进制，不把它等同于 make check 通过。
- Go 主测试日志汇总：339 通过、3 失败、3 个既有真实服务 opt-in 未执行。44 个 T 主测试全部执行、零 skip；38 个编号都存在。
- 前端 `npm run lint && npm run type-check && npm run build`：全部通过（Node 22.23.3）。
- `bash web/tests/support/real-backend.sh bash /tmp/pcas-b1-T-final-12abe94-browser.sh`：通过，先 timezone-backend，再其他全部文件，`--retries=0`，一次运行。
- 冻结文件字节比对、测试编号集合和 git diff whitespace 检查通过；本轮结束时仅验收报告有后续编辑，没有产品或测试代码修改。

本地证据路径只含合成资料；不提交大日志或临时 token：

- `/tmp/pcas-b1-T-final-12abe94-meta.json`：统一提交、冻结 SHA256、运行区间、自己起的资源与清理确认。
- `/tmp/pcas-b1-T-final-12abe94-go.log`、`...-go-results.json`：完整 make check 输出及主测试结果。
- `/tmp/pcas-b1-T-final-12abe94-browser.log`、`...-timezone.json`、`...-browser.json`、`...-browser-results.json`：浏览器输出、原始 JSON 及逐例状态。
- `/tmp/pcas-b1-T-final-12abe94-browser/`、`...-timezone/`：截图及长资料摘录附件；`...-services/`：本轮临时服务日志。

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

本轮两个多陈述夹具的陈述供给正向对照未通过，不能据本轮签署这些对照通过；旧跨项目原话开放对照也未通过。它们的原话关闭/恢复结果与其他通过测试仍分别列出，不抵销失败。

假模型证明系统实际把哪些内容交给模型，以及处理固定引用/动作的行为；不能证明真实模型会如何作答。没有验证真实向量语义质量、真实 OCR/转录准确率、大规模性能、线上默认 Codex 通道、真实提醒到手机、部署或线上冒烟。浏览器通知权限为 denied 时黄金用例走既有合成订阅回退，只证明临时后端向假端点投递。未合并、部署或操作线上实例。
