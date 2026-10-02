# 第 2 阶段第 1 批：8a4ac50 集成复验（待后续最终一轮）

本轮**未通过验收**。36 条序列按编号汇总为 **34 通过、2 失败（M2、N12）**；39 个 Go 主测试汇总为 36 通过、3 失败。新浏览器 8 个逻辑用例：6 通过、2 失败。现有浏览器回归 118 个全部通过。`make check` 失败仍保留，不能据此上线。

这是协调者要求的当前一轮，不是甲的后续 PR 合入后的最终结果。最终一轮收到通知后再 fetch、变基、完整复验。

## 被测版本和证据

- 被测产品/集成分支：`8a4ac5047940f2e149fce43f29b0a593accbddf1`。本轮所有执行使用同一产品版本。
- 初次 `make check` 的测试提交：`08158f4`；修正测试接线后的定向复验：`e43f820`、`834743d`、`2ce6542`。当前交付提交：`31e63f363e7a5e93435d6129b40eaeb027029980`（之后仅报告和 PR 说明更新）。
- 长资料最终浏览器运行使用 `e43f820`；已有浏览器启动时使用 `fb56243`。期间未修改产品代码。
- 报告汇总是“一次全量 + 有明确接线修正原因的定向复验”，**不是当前测试提交一次全量全绿**。下次最终一轮必须统一重跑。
- Draft PR：[T 独立验收 #64](https://github.com/soaringjerry/PCAS/pull/64)。[首交付基线](2026-10-01-phase2-batch1-acceptance.md)仅是历史记录。

全部资料和输出均为合成。临时 PostgreSQL 使用指定 `pgvector/pgvector:0.8.2-pg16-bookworm`、tmpfs、随机 loopback 端口。模型正文由本地假 HTTP 服务捕获；副手结果由假服务固定。真实浏览器使用临时 serve/worker，Telegram、Web Push 和订阅协议外部依赖也是假服务。未读取线上 `.env`/`config/`，未连接线上库、真实模型或真实通知通道。容器仅按记录的 ID 移除，进程仅按自己记录的 PID 清理。

## 36 条逐条结果

| 序列 | 本轮结果 | 测试名 | 说明 |
|---|---|---|---|
| P1 | 通过 | `TestPhase2B1_P1_DeskTurnRawSourceAndDeletionControl` |  |
| P2 | 通过 | `TestPhase2B1_P2_ActualDeputyRequestAndSourceDependencies` |  |
| P3 | 通过 | `TestPhase2B1_P3_ManualBriefAndDeletionControl` |  |
| P4 | 通过 | `TestPhase2B1_P4_ClaimOnlyEmptySourceSection` |  |
| P5 | 通过 | `TestPhase2B1_P5_SystemGeneratedSourcesExcluded` |  |
| P6 | 通过 | `TestPhase2B1_P6_BudgetsTruncateAndComplete`<br>`TestPhase2B1_P6_MatchedTailExcerptBeforeAndAfterChunking` |  |
| P7 | 通过 | `TestPhase2B1_P7_HistoricalQuestionsNotRepeatedAsSources` |  |
| P8 | 通过 | `TestPhase2B1_P8_DuplicateTitlesDoNotMergeIdentities` |  |
| P9 | 通过 | `TestPhase2B1_P9_PublicReadsCannotForgeInternalAccess` |  |
| P10 | 通过 | `TestPhase2B1_P10_DeputyClaimFiltersAndSharedRawSources`<br>`TestPhase2B1_P10_ExclusionsInferenceAndProjectFiltersStayOnClaimsOnly` |  |
| P11 | 通过 | `TestPhase2B1_P11_CurrentVersionOnly` |  |
| P12 | 通过 | `TestPhase2B1_P12_MediaRequiresReadableTranscriptOrOCR` |  |
| P13 | 通过 | `TestPhase2B1_P13_AdoptedSourceContentSurvivesForUserButExpiresForModels` |  |
| M1 | 通过 | `TestPhase2B1_M1_ClaimCorrectionKeepsVisibleExchange` |  |
| M2 | 失败 | `TestPhase2B1_M2_ModelHistoryReplacesOnlyOldAnswer`<br>`TestPhase2B1_M2_CurrentDestinationUnavailablePreservesQuestionAndReceipt` | 模型历史缺回执行；秘书 delegate 缺讨论历史。 |
| M3 | 通过 | `TestPhase2B1_M3_SourceVersionMarksOutdatedWithoutErasure` |  |
| M4 | 通过 | `TestPhase2B1_M4_SourceDeletionScrubsDependentTurnRunAndDoc` |  |
| M5 | 通过 | `TestPhase2B1_M5_OutdatedReceiptCanUndoAndSurvivesReload` |  |
| M6 | 通过 | `TestPhase2B1_M6_ReplayAndTelegramDuplicateKeepOldAnswer` |  |
| M7 | 通过 | `TestPhase2B1_M7_CorrectedDeputyResultRemainsButStale` |  |
| M8 | 通过 | `TestPhase2B1_M8_UnchangedTurnOmitsOutdatedField` |  |
| N1 | 通过 | `TestPhase2B1_N1_UndoDeletesPlanKeepsOriginalAndHistory` |  |
| N2 | 通过 | `TestPhase2B1_N2_ExtractionAfterFullUndoDoesNotRecreatePlan` |  |
| N3 | 通过 | `TestPhase2B1_N3_PartialUndoKeepsMemoryAndRawSupply` |  |
| N4 | 通过 | `TestPhase2B1_N4_RememberKeepsPreferenceButDeletesPlan` |  |
| N5 | 通过 | `TestPhase2B1_N5_ConfirmedClaimSurvivesFullUndo` |  |
| N6 | 通过 | `TestPhase2B1_N6_IndependentEvidenceProtectsClaim` |  |
| N7 | 通过 | `TestPhase2B1_N7_RefusedUndoIsAtomic` |  |
| N8 | 通过 | `TestPhase2B1_N8_TelegramCallbackUsesRealUndo` |  |
| N9 | 通过 | `TestPhase2B1_N9_UndoCommandReplayHasNoSecondDeletion` |  |
| N10 | 通过 | `TestPhase2B1_N10_ManualUndoDoesNotCascadeSecretaryMemory` |  |
| N11 | 通过 | `TestPhase2B1_N11_UndoUpdateThenCreationDeletesOnlyCompleteTurn` |  |
| N12 | 失败 | `TestPhase2B1_N12_RandomTwentyActionsInTwentySeededGroups` | 20/20 组最后的 sources 与初始值不同；撤销、各轮计划删除及资料保留断言通过。 |
| G1 | 通过 | `TestPhase2B1_G1_FreshDatabaseMigratesWithoutLegacyObjects` |  |
| G2 | 通过 | `TestPhase2B1_G2_LegacyCleanupPreservesEveryBusinessRow` |  |
| G3 | 通过 | `TestPhase2B1_G3_SecondStartupMakesNoFurtherChanges` |  |

N12 使用 20261001–20261020 固定种子，每组 20 个跨轮秘书动作并逆序撤销。20 组都执行了全部动作、撤销和计划断言：每轮最后一个动作撤销后，该轮计划消失，其他轮计划仍在；所有原话和对话保留。20 组都在最后的事项比较失败。增加差异诊断后只检查 seed 20261001：标题、状态、步骤等一致，但 `sources` 比初始值多了 `label:"撤销：加了 1 步"` 的条目，版本也不同。历史、更新时间、事项 revision 已排除，`sources` 仍严格比较。该条究竟应算业务差异还是允许保留的审计来源，需要协调者明确；当前保持失败，不放宽预期。

M2 的秘书实际请求保留提问并使用替换语，旧陈述依赖不再带入，用户内容也保留。失败包括：秘书历史漏回执行；秘书的 `delegate` 动作产生的 Brief 没有“导办台之前的讨论”及替换语，提问仅被再次作为原话召回；显式 `requestRun` 在排除/不可见两种当前场合对照中有正确的提问和替换语，但漏回执行。实际副手 HTTP 请求也验证了这些缺失。

## 浏览器结果

| 文件 / 用例 | 结果 | 观察 |
|---|---|---|
| 模拟 P1/R8a：单击直接全文、定位标记、无版本状态摘要 | 通过 | 精确摘录在长原文中标记且可见 |
| 模拟 M1/M3/M5：更新标记、内容保留、撤销及刷新 | 通过 | 回执撤销后刷新仍在 |
| 模拟 M8/P1：无标记、独立依据列表、390px | 通过 | 原话没有并入时间轴，无横向溢出 |
| 模拟 P1/R8a：摘录不存在 | 通过 | 全文自动显示，没有伪标记 |
| 模拟 P1/R8a：资料库同一资料 | 通过 | 版本、状态、摘要、手动展开保持原交互 |
| 真实 P1/P8：12 份同名资料、新对话、请求、原文及删除对照 | 失败 | 实际请求含原话、零陈述、卡片和全文正常；全文等于摘录时没有 mark。此处中止，所以浏览器删除对照未执行；Go P1 的删除对照通过 |
| 真实 P6/R8a：多行长资料、宽泛提问 | 失败 | 请求和卡片只给前言，缺中段暗号；前言能匹配、能标记且可见。中段问题未拿到中段资料 |
| 真实 P6/R8a：同资料、唯一中段检索词对照 | 通过 | 请求和卡片有中段暗号；直接全文、mark、可见区域断言全通过；换行没有被改成空格 |

真实长原文共 61 个换行，超过 600 字；中段两行是“成都资料中段：交付暗号是青色灯塔7319。”和“见面地点是锦江桥东侧，带上蓝色档案袋。”。宽泛提问是“成都长资料中段的交付暗号是什么”，唯一中段对照是“蓝色档案袋”。后端摘录保留换行，去掉截断提示用的首尾省略号后可在原文中精确找到；本轮未观察到“换行被压为空格导致无标记”的问题。宽泛提问失败来自选段位置。

现有浏览器回归单次运行且 `--retries=0`：`timezone-backend` 1 个，加上其余既有文件 117 个，共 118 个全部通过，包含黄金路径、订阅协议、真实副手、通知及模拟界面。黄金路径没有 repeat-each。新增用例之外未改旧浏览器预期。

## 发现清单

| 编号 | 现象 / 契约预期 | 建议归属 |
|---|---|---|
| I-A1 | outdated 的模型历史漏动作回执行。R7 补充明确要求保留。秘书和显式副手入口均失败 | A；协调者提到的后续 PR 部分范围 |
| I-A2 | 秘书 delegate 的 Brief 不带本段讨论历史、替换语和回执行。原提问被作为相关原话再次送出，不能替代 R7 历史处理 | A |
| I-N1 | N12 最终 `sources` 新增撤销来源，导致 20 组严格恢复断言失败；其他可逆业务字段、计划和资料断言通过 | B / 协调者：确认 `sources` 的恢复边界，再决定实现或预期处理 |
| I-C2-1 | 短原话本身就是完整摘录时，全文直接显示但没有 mark。R8a 当前没有全文等于摘录的例外 | C2 / 协调者 |
| I-A3 | 宽泛中段提问与前言共享词语时，摘录选在前言，中段细节没有进模型；唯一词对照通过 | A / 协调者：判断检索选段质量的处理范围 |
| I-PENDING | `TestSecretaryStablePrefixAndVisibility` 的旧隐藏断言失败，当前原话仍供给 | R2 待用户决定；按协调者指示保持失败 |
| I-OLD-DATE | 四组既有测试失败：`TestCodexSecretaryAndLegacyFormats`、`TestSecretaryHistoryOneEntryPerAction`、`TestSecretarySchedulesAndUpdatesRecentTask`、`TestDueReminderOffsets`。夹具固定 2026-10-02 15:00 上海（或该时刻的提醒）；执行时已约 17:48，提醒被视为过去，导致提醒/历史预期失败 | 协调者：日期夹具修复另行授权，本轮未改 |

## 获批的既有测试变更

严格按 T-acceptance 的批准范围修改两组：

1. `desk_dependency_growth_test.go::TestSecretaryConversationDependenciesStayASet`：每轮检查捕获原话 + 采纳陈述的精确集合，`kind`、版本、各一次；保留 9 轮增长、600 项旧重复结构读取及后继集合不膨胀的检查。原话取 capture 候选的源，采纳时生成的 `memory-input` 记录不算原话。定向复验通过。
2. `ux_regressions_test.go::TestDeskHistoryCannotBypassDestinationItemScope`：提问、陈述、模型回答使用三个不同合成标记。三个子用例均检查旧回答不在、替换语和提问在、相关记忆段无受限陈述、ContextMemoryIDs 无其 id。没有判断 exclude 对应原话是否供给。全量中三个子用例全部通过。

`TestSecretaryStablePrefixAndVisibility` 文件未改，继续失败。没有修改其他旧预期。冻结 JSON 的原有值全部保留，只追加 R7、长资料和唯一中段对照。

## 检查、测试接线与证据目录

- `GOFLAGS=-v bash testdata/phase2/run-go.sh make check`：失败。fmt-check、vet 先通过，test 阶段失败后 make 停止；随后 `make fmt-check lint build` 通过。全量及所有定向运行均没有 DATA RACE。
- 前端 `npm run lint && npm run type-check && npm run build` 通过；浏览器接线修正后再次 lint/type-check 通过。
- 39 个编号主测试覆盖全部 36 序列；冻结 JSON 只增不改审计、shell 语法、diff whitespace 检查通过。相对被测 8a4ac50 的改动没有产品文件，旧测试仅上述两份。
- 接线修正及定向执行都有具体原因：JSONB/HTTP 对象键排序导致字节比较误报，改为保留所有值的语义比较；P1 补齐冻结夹具已指定的表达时间元数据；旧依赖增长测试指向 capture 原件；长浏览器进入来源页签并按实际依据按钮定位。未改 gold 的旧值，也未放宽任何内容、依赖、预算、撤销或标记断言。
- 长资料用 `expect.soft` 汇集多个失败：失败仍令用例失败，继续点击以保存原文、摘录和 mark 证据。不是失败降级。没有同代码重跑碰运气。

本机证据均为合成内容，未把大日志或 token 提交到仓库：

- `/tmp/pcas-b1-T-integration-8a4ac50-go.log`：一次全量及 20 个种子。
- `...-targeted-go.jsonl`、`...-targeted-final.jsonl`、`...-p1-final.jsonl`：修正后的定向结果和 N12 精确字段差异。
- `...-browser-first/`：新用例首次运行截图/trace；`...-long-final/`：长资料最后的截图/trace/摘录附件。
- `...-existing-browser.log`、`/tmp/pcas-b1-T-existing-{timezone,browser}/`：118 个现有用例及截图。

既有三个真实服务 opt-in 用例按原有机制未执行：`TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`。T 没有添加 skip；runner 明确清除真实服务 opt-in，符合本轮只用假模型的要求。上述三项不算真实模型验收通过。

## 规则对应位置（用于复查，不代表逐行实现审计）

| 规则 | 被测代码位置 / 入口 |
|---|---|
| R1 | `internal/memory/contracts.go` Scope.Team；`internal/postgres/retrieval.go`；P9 外部 HTTP 对照 |
| R2/R3 | `retrieval.go` 的 `teamSourceExcerptsTx`、`sourceExcerpt`；秘书、实际副手、manual 三入口 |
| R4/R5 | `desk_turn.go::secretaryPrompt`、`runs.go::runCommandTx`；实际提示词、Brief 和依赖持久化 |
| R6 | `runs.go` 运行依赖校验、`artifacts.go::sanitizeItemTx`；P13、M3/M4 |
| R7 | `desk_turn.go::deskTurnsTx` / `secretaryPrompt`、`run_context.go`、`runs.go`、Telegram 重投；M1–M8 与当前场合对照 |
| R8/R8a | `desk_turn.go::secretaryCardsTx`、前端 SecretaryCards/SourceSheet；独立依据、mark 和资料库对照 |
| R9 | `undone_turns.go`、`actions_log.go::Undo` / `deleteUndoneTurnMemoriesTx`、`processing.go`；N1–N12 |
| R10 | `migrations/026_drop_phase2_0_leftovers.sql`；新库、遗留库及第二次启动 |

## 未覆盖和最终一轮边界

没有验证真实模型能否回答、真实向量语义质量、真实 OCR/转录准确率、大规模性能、线上冒烟或部署。外部调用全为假服务。模拟卡片不能替代真实请求证据，真实请求已另行捕获。

本轮未加入 Brief 跨 UTC 日期边界的时区对照；现有原话日期样本在两时区是同一天，因此不能据本轮通过判断甲后续时区修复正确。最终一轮应补齐该边界（预期须继续来自契约）。R2 隐藏/排除陈述对应原话的决定待用户；不据本轮实现自行定预期。

当前报告的代码并非一次统一全量测量；最终必须在甲后续及协调者处理本轮发现后重新变基运行全量。没有合并、部署或线上操作。
