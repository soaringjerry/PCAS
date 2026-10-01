# 第 2 阶段第 1 批独立验收：首交付记录（待集成验收）

这是测试交付及基线发现清单，**不是通过验收的报告**。协调者尚未通知“集成分支就绪”；A、C2 完成后再变基并跑全套，届时更新本文件的逐条结果。

- 后端基线被测提交：`95f73e0b49cd159d917ee0a80bad0cb58ed4f783`；产品基线：`8cde14d4a18b67a252bcc5fa305650242b2c80e8`。
- 当前测试交付提交：`0120baad150ba3e9b119be7c60ff414d8ffab4ad`（之后的报告提交仅增加文档）。
- 最近读取补充契约的集成分支提交：`8040f673a2c401b17bfa7f5403eee03fc24a7481`；该提交不是本次已测产品。
- 独立工作区 `/root/PCAS-wt/b1-T`，分支 `phase2/b1-T-acceptance`，PR base `phase2/batch1`。

## 预期冻结与接线

先完成指定四份文档的顺序阅读，再提交原始预期（`4e35213`，变基后为 `a0e5bb2`），之后才读取基线接口和测试接线。原 JSON 的全部值仍逐项保留；追加 P13、N12、R3/R1 的对照及 R8/R8a 补充。没有读取 A、B、C 的实现来决定预期，没有修改产品文件或已有测试的预期，没有 skip、重试或失败降级。

秘书走 `DeskTurn` 并捕获假服务收到的完整 HTTP 正文；副手走 `requestRun` 后 `runAgentOnce` 真正发送请求；manual 检查真实 Brief。撤销走命令或 Store.Undo，Telegram 使用生产 poller 和回调验证，仅把 HTTP 传输转向本地假服务。抽取走 `ProcessExtraction` 与任务租约，假输出写死。P13 使用副手实际输出的自动采纳，再比较后续秘书/另一个副手请求和用户可见事项。

临时 PostgreSQL 使用 `pgvector/pgvector:0.8.2-pg16-bookworm`、tmpfs、随机 loopback 端口；Go 入口还验证临时库名和测试用户名。独立 schema 清理后，runner 只移除自己的 container ID，浏览器 runner 只终止自己记录的 PID。没有连接线上数据库、读取线上 `.env`/`config/`、调用真实模型或发真实通知。

## 后端逐条基线结果

最终基线命令：`bash testdata/phase2/run-go.sh go test -race -json ./internal/postgres -run '^TestPhase2B1_' -count=1 -timeout=12m`。38 个主测试：4 通过，34 失败；20 个 N12 固定种子子测试全部执行；无 DATA RACE。下面 36 个序列按每个编号所属全部主测试汇总（4 通过，32 失败），均待集成复验。

| 序列 | 基线结果 | 对应测试名 |
|---|---|---|
| P1 | 失败 | `TestPhase2B1_P1_DeskTurnRawSourceAndDeletionControl` |
| P2 | 失败 | `TestPhase2B1_P2_ActualDeputyRequestAndSourceDependencies` |
| P3 | 失败 | `TestPhase2B1_P3_ManualBriefAndDeletionControl` |
| P4 | 失败 | `TestPhase2B1_P4_ClaimOnlyEmptySourceSection` |
| P5 | 失败 | `TestPhase2B1_P5_SystemGeneratedSourcesExcluded` |
| P6 | 失败 | `TestPhase2B1_P6_BudgetsTruncateAndComplete`<br>`TestPhase2B1_P6_MatchedTailExcerptBeforeAndAfterChunking` |
| P7 | 失败 | `TestPhase2B1_P7_HistoricalQuestionsNotRepeatedAsSources` |
| P8 | 失败 | `TestPhase2B1_P8_DuplicateTitlesDoNotMergeIdentities` |
| P9 | 失败 | `TestPhase2B1_P9_PublicReadsCannotForgeInternalAccess` |
| P10 | 失败 | `TestPhase2B1_P10_DeputyClaimFiltersAndSharedRawSources`<br>`TestPhase2B1_P10_ExclusionsInferenceAndProjectFiltersStayOnClaimsOnly` |
| P11 | 失败 | `TestPhase2B1_P11_CurrentVersionOnly` |
| P12 | 失败 | `TestPhase2B1_P12_MediaRequiresReadableTranscriptOrOCR` |
| P13 | 失败 | `TestPhase2B1_P13_AdoptedSourceContentSurvivesForUserButExpiresForModels` |
| M1 | 失败 | `TestPhase2B1_M1_ClaimCorrectionKeepsVisibleExchange` |
| M2 | 失败 | `TestPhase2B1_M2_ModelHistoryReplacesOnlyOldAnswer` |
| M3 | 失败 | `TestPhase2B1_M3_SourceVersionMarksOutdatedWithoutErasure` |
| M4 | 失败 | `TestPhase2B1_M4_SourceDeletionScrubsDependentTurnRunAndDoc` |
| M5 | 失败 | `TestPhase2B1_M5_OutdatedReceiptCanUndoAndSurvivesReload` |
| M6 | 失败 | `TestPhase2B1_M6_ReplayAndTelegramDuplicateKeepOldAnswer` |
| M7 | 通过 | `TestPhase2B1_M7_CorrectedDeputyResultRemainsButStale` |
| M8 | 通过 | `TestPhase2B1_M8_UnchangedTurnOmitsOutdatedField` |
| N1 | 失败 | `TestPhase2B1_N1_UndoDeletesPlanKeepsOriginalAndHistory` |
| N2 | 失败 | `TestPhase2B1_N2_ExtractionAfterFullUndoDoesNotRecreatePlan` |
| N3 | 失败 | `TestPhase2B1_N3_PartialUndoKeepsMemoryAndRawSupply` |
| N4 | 失败 | `TestPhase2B1_N4_RememberKeepsPreferenceButDeletesPlan` |
| N5 | 通过 | `TestPhase2B1_N5_ConfirmedClaimSurvivesFullUndo` |
| N6 | 通过 | `TestPhase2B1_N6_IndependentEvidenceProtectsClaim` |
| N7 | 失败 | `TestPhase2B1_N7_RefusedUndoIsAtomic` |
| N8 | 失败 | `TestPhase2B1_N8_TelegramCallbackUsesRealUndo` |
| N9 | 失败 | `TestPhase2B1_N9_UndoCommandReplayHasNoSecondDeletion` |
| N10 | 失败 | `TestPhase2B1_N10_ManualUndoDoesNotCascadeSecretaryMemory` |
| N11 | 失败 | `TestPhase2B1_N11_UndoUpdateThenCreationDeletesOnlyCompleteTurn` |
| N12 | 失败 | `TestPhase2B1_N12_RandomTwentyActionsInTwentySeededGroups` |
| G1 | 失败 | `TestPhase2B1_G1_FreshDatabaseMigratesWithoutLegacyObjects` |
| G2 | 失败 | `TestPhase2B1_G2_LegacyCleanupPreservesEveryBusinessRow` |
| G3 | 失败 | `TestPhase2B1_G3_SecondStartupMakesNoFurtherChanges` |


N12 种子为 20261001–20261020，每组 20 个秘书动作，分多轮执行并逆序撤销；每一步核对各轮计划的存在与否，最后核对全部事项的业务字段、原话和对话。业务比较排除记录撤销本身的 revision、历史和更新时间；标题、正文、备注、状态、步骤、来源等业务字段仍严格比较。首交付末尾修正了随机目标列表，使其按创建顺序而非随机 UUID 排序，保证种子可复现；该接线修正后只做编译检查，N12 的最终结果等集成复验。

## 浏览器结果与待跑用例

补充 R8a 之前的一次基线运行：4 个浏览器测试中，原话卡片打开原文通过；真实请求缺原话、outdated 标记缺失、原话并入时间轴三个用例失败。观察针对 C 合入前的旧基线，不能据此判断现有集成分支里的 C 仍有这些问题。

R8/R8a 的最新测试已加入，**尚未执行最新六个浏览器用例**，按协调者要求等待 A/C2 后一起验收：

| 文件 | 当前测试 | 当前结果 |
|---|---|---|
| `phase2-batch1.spec.ts` | P1/R8a 单击依据直接全文、定位标记、无版本状态摘要 | 待集成 |
| `phase2-batch1.spec.ts` | M1/M3/M5 outdated 保留内容、回执撤销及刷新 | 待集成 |
| `phase2-batch1.spec.ts` | M8/P1 无更新标记、原话独立于时间轴、390px | 待集成 |
| `phase2-batch1.spec.ts` | P1/R8a 摘录不存在时全文可见、无伪标记 | 待集成 |
| `phase2-batch1.spec.ts` | P1/R8a 资料库同一资料保留原面板交互 | 待集成 |
| `phase2-batch1-backend.spec.ts` | P1/P8 页面原话、新对话、完整请求捕获、原文、删除对照 | 待集成 |

真实后端使用 support 内本地捕获代理保存实际模型请求；没有把黄金路径服务中只记录当前提问的 events 当作上下文证据。日期断言不依赖今天是星期几。

## 基线发现清单

| 编号 | 现象与预期 | 归属建议 |
|---|---|---|
| F-A1 | 零陈述、同名标题资料未进入秘书/副手/manual；缺 source 依赖和来源引用卡片。应按 R1–R5/R8 供给。 | A |
| F-A2 | 陈述纠正后用户回答/卡片被替换；模型历史缺 R7 替换语；请求及 Telegram 重放缺 outdated。应保留用户内容，仅更新模型历史。 | A |
| F-A3 | 原话升版、删除未能通过 source 依赖触发过期/删除传播；P13 升版后仍把采纳内容给模型。应停止供给过时采纳内容并保留用户事项。 | A；如需超出其文件归属，由协调者安排 |
| F-B1 | 完整撤销后计划仍在，先撤销后抽取仍生成计划；N12 各轮计划未随最后动作撤除。原話和对话保留的对照已检查。 | B |
| F-B2 | 026 未执行，遗留 6 表、5 列、4 条迁移记录仍在。应清理且保留所有非遗留表逐行数据。 | B |
| F-C1 | 旧前端无依据已更新标记，原话混进时间轴；这是 C 合入前的基线观察，当前 C 待重验。 | C / 协调者 |
| 待测 | R8a 直接全文、定位、精简面板与资料库面板对照，暂不作产品结论。 | C2 |

测试接线问题已修正：混合 JSON 读取、自动采纳后重复采纳、动态快照统计逐字比较、检索排名的采样时机。没有因此改冻结预期或放宽业务断言。

## 静态检查与复验入口

`go test ./internal/postgres ./web/tests/support/phase2-capture -run '^$'` 编译检查通过；`npm run lint && npm run type-check && npm run build` 通过，R8a 测试补充后 lint/type-check 也通过。三个 runner 的 `bash -n`、`git diff --check`、36 编号覆盖、冻结 JSON 只增不改审计、四份遗留 SQL 与归档标签逐字节比较通过。`make check` 尚未作为集成验收运行，CI 的该行已接到独立临时库 runner。

- 36 序列：`bash testdata/phase2/run-go.sh`。
- 全部 Go 检查：`bash testdata/phase2/run-go.sh make check`。
- 新浏览器：先 `npm --prefix web ci && npm --prefix web run build`，再 `bash web/tests/support/phase2-backend.sh`。

## 未覆盖的地方与边界

没有在线上跑真实默认模型或 Telegram 网络，也未验证真实模型是否会按要求回答；固定假输出只证明输入交付和引用处理。未覆盖真实向量模型的语义排名、性能/大规模并发、真实 OCR/语音准确率。媒体用例覆盖已有可读解析文本的供给边界。没有上线冒烟、部署或真实通知。集成分支全套现有回归、最终浏览器与 A/C2/B/C 联合行为均待“集成分支就绪”后执行。

已有测试预期修改：无。冻结预期纠正：无，仅追加。完整原始日志、截图和 trace 保留在本机临时目录/忽略的 `web/test-results/`，未提交线上内容、密钥或大日志。文档入口由协调者按文件归属接入。
