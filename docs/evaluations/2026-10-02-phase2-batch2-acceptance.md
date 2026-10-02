# 第 2 批独立验收：最终统一轮报告

2026-10-02。任务 T2。**本轮未通过：后端第 2 批 43/43 个入口通过，浏览器第 2 批 0/5 通过。** 浏览器失败均定位为 T2 测试使用了错误的资料库标签参数；本轮不修改导航或断言，不重跑。

## 最终轮基线与范围

- 被测集成分支提交：`02e7a2773031926a70419bccd68954d4096093b7`（02e7a27，E1 / E2 / M / M2 / U2 / U2b 全部已合入）。
- 全部执行固定在验收分支提交：`96f6fd434327312c1c029ea8101266f02b1c8c79`（96f6fd4）。后端、前端检查和全部浏览器用例运行期间，HEAD 和所有受版本控制的文件保持不变；结果写完后只提交本报告。
- 第 10 节批准取消 U5 和第 8 节表中项目时间轴那一行。先以 e61394c 追加冻结取消记录，后以 96f6fd4 删除 U5 用例；原 gold 保留。本轮 U5 是取消范围，不计为通过或跳过。
- 其余断言没有调整；没有修改产品代码、第 1 批测试或已有断言。对照变基后的原验收提交 756dbcf，浏览器 spec 只删除 U5 尾段，gold 只新增 supplement_02e7a27。
- Go 1.26.8、Node 22.23.3；独立临时 PostgreSQL 16 / pgvector 0.8.2、UTF8，合成数据与本地假模型。真实后端浏览器启动实际 serve 与 worker，不拦截 PCAS API。不使用线上配置、数据库、真实模型和通知通道。

## 统一运行结果

| 检查 | 结果 |
|---|---|
| `make check` | 通过，退出码 0，总计 469.81 秒；fmt-check / lint / test / build 按原 Makefile 执行 |
| 全部 Go 后端序列（含上述 check） | 12 个有测试的包通过，3 个包无测试；PostgreSQL 包 455.35 秒；第 2 批 40 条序列、43 个入口 43 通过 / 0 失败 |
| M6，第 1 批全部 P/M/N/G 原样执行 | 通过，含 N12 的 20 组固定种子；N12 独立执行也通过 |
| M14，副手身份下记忆的时间 / 人 / 地点 | 通过；首次预跑的字段缺口在 M2 合入后已消失 |
| 前端 `npm run lint`、`npm run type-check`、`npm run build` | 均通过 |
| 模拟后端浏览器，CI 的 9 个 spec 文件 | 106 通过 / 4 失败；4 个失败均为第 2 批，现有 106 个回归用例全部通过 |
| 第 2 批真实后端浏览器 U4 | 0 通过 / 1 失败 |
| 现有真实后端浏览器（timezone / golden / legacy） | 20 通过 / 0 失败；PCAS_REAL_BACKEND_ROUND=1，每条仅一次 |
| 全部浏览器汇总，仓库 17 个 spec 文件 | 126 通过 / 5 失败；没有重试、flaky 或跳过 |

`make check` 的 Go test 阶段即为本轮完整后端运行，没有另跑一份相同后端。仓库外临时 go 包装器只给 `go test` 加 `-json -count=1`，保留 Makefile 的 `-race -timeout 30m`；其他 go 命令直接转发，Makefile 未修改。JSON 用于逐条取结果，count=1 避免缓存冒充本次运行。所有 Go 顶层用例为 393 通过、0 失败、3 个原有真实模型 guard 跳过；没有 DATA RACE 报告。原有离线 guard 跳过的真实模型用例：`TestInstalledCodexHandshake`, `TestLiveCodexSecretaryAndLegacyFormats`, `TestLiveContinuityReplay`；第 2 批后端没有跳过。

两个显式浏览器命令指定 `--retries=0`，现有 runner 默认也是 0，golden 套件只选 round=1，没有 repeat-each 重复。模拟用例由构建后的静态站点加测试路由供给；真实后端用既有隔离 runner。

主要执行命令（数据库、服务地址及证据目录通过临时环境传入）：

```sh
make check
npm run lint
npm run type-check
npm run build
npx playwright test tests/secretary.spec.ts tests/fixes.spec.ts tests/notify.spec.ts tests/buttons.spec.ts tests/timezone.spec.ts tests/settings-things-ux.spec.ts tests/usability-acceptance.spec.ts tests/phase2-batch1.spec.ts tests/phase2-batch2.spec.ts --retries=0 --reporter=list,json
bash web/tests/support/real-backend.sh npx playwright test tests/phase2-batch2-backend.spec.ts --retries=0 --reporter=list,json
PCAS_REAL_BACKEND_ROUND=1 bash web/tests/support/real-backend.sh
```

## 本轮逐序列结果

| 序列 | 测试名 | 文件 | 结果 | 耗时 |
|---|---|---|---|---|
| X1 | `TestPhase2B2_X1_SelfMentionsExpressionAndEvent` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.73 秒 |
| X1 | `TestPhase2B2_X1_ExpressionFallbackIsLimitedToHandEnteredSources` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 1.56 秒 |
| X2 | `TestPhase2B2_X2_EntitiesSharedAcrossSourcesAndOwnersIsolated` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.44 秒 |
| X3 | `TestPhase2B2_X3_ExactAliasTrimCaseAndType` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.41 秒 |
| X4 | `TestPhase2B2_X4_HallucinatedNameDroppedWithoutDroppingMemory` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.25 秒 |
| X5 | `TestPhase2B2_X5_UnknownExpressionDoesNotResolveRelativeDate` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.58 秒 |
| X6 | `TestPhase2B2_X6_InvalidWhenKeepsClaim` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 1.04 秒 |
| X7 | `TestPhase2B2_X7_ConfirmationBoundary` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 1.88 秒 |
| X8 | `TestPhase2B2_X8_QuestionAndOperationProduceNoMemory` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.50 秒 |
| X9 | `TestPhase2B2_X9_SecretaryTaskAlsoRetainsPlan` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.45 秒 |
| X10 | `TestPhase2B2_X10_ReprocessingEnrichesWithoutRevisionOrOutdated` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.43 秒 |
| X11 | `TestPhase2B2_X11_UserEditedOrConfirmedMemoryNotEnriched` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.72 秒 |
| X12 | `TestPhase2B2_X12_FullUndoBlocksPlanPartialUndoDoesNot` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 1.03 秒 |
| X13 | `TestPhase2B2_X13_ImportedHistoricalTimeAndNoTodayTask` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.31 秒 |
| X14 | `TestPhase2B2_X14_AllLongSourceSegmentsAndOverlapDeduplicated` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.33 秒 |
| X15 | `TestPhase2B2_X15_EmptyExtractionStillSuppliesRawText` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.33 秒 |
| X16 | `TestPhase2B2_X16_DeleteOrReplaceDuringActualModelCall` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.51 秒 |
| X17 | `TestPhase2B2_X17_MentionLimitsRetainFirstEightValid` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.29 秒 |
| X18 | `TestPhase2B2_X18_ArchiveAssistantSkippedButAvailableAsNeighborAndRaw` | `internal/postgres/phase2_b2_extraction_test.go` | 通过 | 0.38 秒 |
| F1 | `TestPhase2B2_F1_NewJobsPrecedeBackfill` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 0.22 秒 |
| F2 | `TestPhase2B2_F2_SubscriptionRetriesUnavailableAndStopsAtFive` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 0.63 秒 |
| F3 | `TestPhase2B2_F3_NotConfiguredStopsAndManualRetrySucceeds` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 0.32 秒 |
| F4 | `TestPhase2B2_F4_BudgetDefersWithoutAttemptAndCalendarHandlesDST` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 0.65 秒 |
| F5 | `TestPhase2B2_F5_ConcurrentBackfillCapAndHourlyRecovery` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 1.45 秒 |
| F6 | `TestPhase2B2_F6_PausedArchiveSkippedOrdinaryProcessedResumeWorks` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 0.27 秒 |
| F7 | `TestPhase2B2_F7_KilledWorkerRecoversLeaseAndFencesOldCommit` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 0.32 秒 |
| F8 | `TestPhase2B2_F8_MeteredCallFailureDoesNotRetry` | `internal/postgres/phase2_b2_queue_test.go` | 通过 | 0.23 秒 |
| M1 | `TestPhase2B2_M1_FiltersCursorIsolationAndOwnership` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 10.80 秒 |
| M1 | `TestPhase2B2_M1_ReadBatchQueryCountDoesNotGrowWithRows` | `internal/postgres/phase2_b2_read_count_test.go` | 通过 | 10.05 秒 |
| M2 | `TestPhase2B2_M2_SnapshotCapDoesNotCapSecretaryOrDeputy` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 26.54 秒 |
| M3 | `TestPhase2B2_M3_FacetCountsDeletionAndFiftyLimit` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.41 秒 |
| M3 | `TestPhase2B2_M3_FacetsTakeFiftyMostFrequentPerRole` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 9.37 秒 |
| M4 | `TestPhase2B2_M4_UndoPreservesAnswerAndDeputyReplacesModelHistory` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.81 秒 |
| M5 | `TestPhase2B2_M5_ExplicitDeleteStillClearsDependents` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.68 秒 |
| M6 | `TestPhase2B2_M6_AllBatch1SequencesUnchanged` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 88.54 秒 |
| M7 | `TestPhase2B2_M7_NewDeputyVisibilityInitializesOnlyOnce` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.55 秒 |
| M8 | `TestPhase2B2_M8_DeleteCleansOrphansButKeepsSharedAndSelf` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.39 秒 |
| M9 | `TestPhase2B2_M9_CorrectionCarriesMentionsAndEventToNewVersion` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.36 秒 |
| M10 | `TestPhase2B2_M10_DistinctActionableHumanJobMessages` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.23 秒 |
| M11 | `TestPhase2B2_M11_ExportContainsNewTablesAndEventValues` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.30 秒 |
| U1 | `U1 记忆卡片显示人地点说话和事件日期，没有内容的卡片无空位及内部说法` | `web/tests/phase2-batch2.spec.ts` | 失败 | 5.27 秒 |
| U2 | `U2 点人后仅保留提到他的记忆，清掉筛选后恢复；地点和性质筛选传给接口` | `web/tests/phase2-batch2.spec.ts` | 失败 | 5.45 秒 |
| U3 | `U3 300条记忆从接口翻至末尾不重复，390px无横向溢出` | `web/tests/phase2-batch2.spec.ts` | 失败 | 5.54 秒 |
| U1 | `U1 事件区间卡片显示实际最后一天` | `web/tests/phase2-batch2.spec.ts` | 失败 | 5.63 秒 |
| U4 | `U4 真实秘书原话经后台变成带成都老王日期的记忆，点老王可筛出` | `web/tests/phase2-batch2-backend.spec.ts` | 失败 | 7.71 秒 |
| M12 | `TestPhase2B2_M12_ProjectEpistemicAgentAndCombinedFilters` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 1.23 秒 |
| M13 | `TestPhase2B2_M13_DetailIsDirectMemoryAndMissingIsNotFound` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.33 秒 |
| M14 | `TestPhase2B2_M14_ModelFacingMemoryCarriesTimePeoplePlaces` | `internal/postgres/phase2_b2_memory_test.go` | 通过 | 0.33 秒 |
| U5 | 项目时间轴决定不丢失 | 已按协调者第 10 节取消 | 不在本轮范围 | — |

## 发现、归属及未完成的覆盖

| 用例 | 首个失败位置 | 现象 | 归属 |
|---|---|---|---|
| `U1 记忆卡片显示人地点说话和事件日期，没有内容的卡片无空位及内部说法` | `phase2-batch2.spec.ts:55` | 记忆文字在 5 秒内不可见；尚未到达后续界面断言 | T2 导航入口 |
| `U2 点人后仅保留提到他的记忆，清掉筛选后恢复；地点和性质筛选传给接口` | `phase2-batch2.spec.ts:89` | 记忆文字在 5 秒内不可见；尚未到达后续界面断言 | T2 导航入口 |
| `U3 300条记忆从接口翻至末尾不重复，390px无横向溢出` | `phase2-batch2.spec.ts:113` | 记忆文字在 5 秒内不可见；尚未到达后续界面断言 | T2 导航入口 |
| `U1 事件区间卡片显示实际最后一天` | `phase2-batch2.spec.ts:55` | 记忆文字在 5 秒内不可见；尚未到达后续界面断言 | T2 导航入口 |
| `U4 真实秘书原话经后台变成带成都老王日期的记忆，点老王可筛出` | `phase2-batch2-backend.spec.ts:47` | 记忆文字在 5 秒内不可见；尚未到达后续界面断言 | T2 导航入口 |

这五个用例都访问 `/library?tab=memories`。实际页面 `web/src/pages/LibraryPage.tsx:603` 的标签取值是 `memory`，649 行只在这个取值下挂载 MemoryTab；无 tab 时也默认 memory。失败 trace 和 error-context 显示页面有“资料库”标题及记忆 / 来源 / 训练数据三个标签，但没有记忆列表。U4 的页面导航返回 200，抽取记忆的 API 查询也返回 200。依据这些证据，本轮首个失败归为验收测试入口写错，不能据此判定 U2 的卡片或筛选实现有错。

U4 到导航前已经通过：真实秘书原话经 worker 抽出计划；confirmation=adopted；说话时刻存在；上海事件区间端点准确；人物地点为老王、成都、春熙路；实际模型 HTTP 请求包含用户原话。之后的卡片显示、点老王筛选和手机宽度断言没有执行。U1 的卡片内容 / 内部说法 / 区间最后一天、U2 筛选、U3 300 条分页与 390px 检查均被入口失败挡住，不能标为通过。

按本轮要求不改断言、导航或产品，不重跑失败用例。后续若协调者安排修复，应由 T2 只修导航到现有记忆标签，保留全部业务断言；再固定新的执行提交做统一一轮。本报告保留这次失败，不作全绿验收结论。

## 超过一分钟的用例

| 用例 | 本轮耗时 |
|---|---|
| `TestPhase2B1_N12_RandomTwentyActionsInTwentySeededGroups` | 69.97 秒 |
| `TestPhase2B2_M6_AllBatch1SequencesUnchanged/N12_RandomTwentyActionsInTwentySeededGroups` | 62.32 秒 |
| `TestPhase2B2_M6_AllBatch1SequencesUnchanged` | 88.54 秒 |
| 浏览器 `G6 提醒送达：真实等待一分钟、首页、Web Push、Telegram` | 106.34 秒 |
| 浏览器 `G9 不丢话：模型真实超时后收到保存回执并能查原话` | 91.61 秒 |

## 证据与清理

证据目录：`/tmp/pcas-t2-final-z8nvyphp`。`run-manifest.json` 记录基线与执行提交；`make-check.log`、`go-test.json`、`make-check.exit` 和 `final-summary.json` 保留完整后端结果；`web-*.log` 保留前端检查；`browser-mocked.json` / log、`browser-b2-real.json` / log、三个 `browser-legacy-real-*.json` 及对应日志保留全部浏览器结果。失败 trace、error-context、U4 截图与真实服务日志也已保留。每个浏览器测试结果都只有一次执行、retry=0。

本轮自建数据库容器和服务进程均已清理；只清理记录的本轮容器与 PID。没有部署或合入 main，验收 PR 仍为 Draft。

---

## 历史记录：准备与首次预跑

下面保留之前各轮的原始结论、失败记录与批准修订。下列“当前状态”“等待通知”等表述均属于历史记录；本次最终轮以以上结果为准。

日期：2026-10-02。任务：T2。已完成 dda8ef5 上首次后端预跑；正式最终一轮仍等 M2 和 U2 合入后协调者通知。

## 当前状态

- 起点：`origin/main` 的 `66b1a43`。编写期间没有读取 S0/E1/E2/M/U2 的实现分支。起初没有远端 `phase2/batch2`，按任务包先从 main 建 `phase2/b2-T2-acceptance`，工作区 `/root/PCAS-wt/b2-T2`；随后变基到 `origin/phase2/batch2` 的 `1cec28b`，第二轮补充变基到 `2e2b8f9`，本次第三轮变基到 `dda8ef5`；Draft PR 的 base 为 `phase2/batch2`。
- 被测集成提交：`dda8ef5a466840cfd63a8b2f7f11167e7cfaedd5`；带验收测试的执行提交：`b104e89f8009e69bd694df3e03e69c2b18c7c04e`。下表为本次预跑结果，浏览器仍未运行；不作最终验收结论。
- 第 1 批测试文件及断言：零修改。M6 原样调用现有 P/M/N/G 序列，包括 N12 的 20 组固定种子。
- 不改产品代码，不连接线上数据库、不读线上 `.env` 或 `config/`、不调用真实模型和通知通道。

## 冻结预期与协调修订

1. `b487718`（变基前 `f38f622`） 先提交 `testdata/phase2/b2-gold.json`，随后开始写测试。时间锚点使用 `DateFromToday`，下周一和周五的日期偏移表由验收者独立给出，事件按当地日历构造左闭右开区间；覆盖上海和悉尼。
2. `020e6dc`（变基前 `866c662`） 追加 `supplement_5865411`：协调者明确 M6 是第 1 批全部 P/M/N/G 序列不改断言；冻结 `BackfillExtractions(ctx, now)`、`nextBudgetDay(now, loc)`、10 分钟错开上限；F5 改为同一小时排入最多 30 个，处理完各次排队任务后检查并向后拨一小时。原字段 `F5.max_started_hour` 是旧契约记录，执行预期以补充为准。
3. `35bc55f`（变基前 `296316d`） 追加 `supplement_32ccb70`（main 合并 `e2175b0`）：X14 每段 12000、步长 11000、重叠 1000；18000 字符的两段资料中，共用句从第 11500 个字符开始，各段独有句各一条，最终 `items = 3`。补充覆盖原 X14 的笼统总数描述。
4. 原 `fixtures.outdated_replacement` 写的是替换语说明，不是第 1 批 R7 的逐字替换语；测试使用后追加的 `supplement_5865411.history_replacement`，原字段不修改。此项属于冻结文件中的未使用描述纠偏，没有改变已有验收预期。

5. `97b622e` 先追加 `supplement_0f8e759`：协调者冻结“显示事件区间最后一天为 `eventTo` 前一天”。U1 追加 `[2025-06-12, 2025-06-15)` 显示到 14 日的浏览器断言。

## 准备阶段的静态验证（历史）

- `make fmt-check` 与 `git diff --check`：通过。编译 overlay 下的 `go vet ./internal/postgres`：通过。
- 后端测试编译：使用仓库外 `/tmp` overlay 为尚未合入的两个 E2 入口提供仅供编译的签名，执行 `go test -c`；未执行编译产物，不作为产品验收结果。临时占位没有提交。
- 前端：`npm run lint && npm run type-check && npm run build` 通过。另对新增 spec 执行 TypeScript 类型检查（本机 Node 类型声明目录）和 ESLint。
- Playwright `--list` 能发现 U1–U4 共 5 个用例；没有启动浏览器执行这些用例。
- `make check` 与真实数据库/浏览器统一验收：按用户要求等待实现合入通知，未运行。

## 覆盖清单

| 序列 | 测试名 | 文件 | 首次后端预跑 |
|---|---|---|---|
| X1 | `TestPhase2B2_X1_SelfMentionsExpressionAndEvent` | `internal/postgres/phase2_b2_extraction_test.go` | 失败（预跑） |
| X1 | `TestPhase2B2_X1_ExpressionFallbackIsLimitedToHandEnteredSources` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X2 | `TestPhase2B2_X2_EntitiesSharedAcrossSourcesAndOwnersIsolated` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X3 | `TestPhase2B2_X3_ExactAliasTrimCaseAndType` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X4 | `TestPhase2B2_X4_HallucinatedNameDroppedWithoutDroppingMemory` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X5 | `TestPhase2B2_X5_UnknownExpressionDoesNotResolveRelativeDate` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X6 | `TestPhase2B2_X6_InvalidWhenKeepsClaim` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X7 | `TestPhase2B2_X7_ConfirmationBoundary` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X8 | `TestPhase2B2_X8_QuestionAndOperationProduceNoMemory` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X9 | `TestPhase2B2_X9_SecretaryTaskAlsoRetainsPlan` | `internal/postgres/phase2_b2_extraction_test.go` | 失败（预跑） |
| X10 | `TestPhase2B2_X10_ReprocessingEnrichesWithoutRevisionOrOutdated` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X11 | `TestPhase2B2_X11_UserEditedOrConfirmedMemoryNotEnriched` | `internal/postgres/phase2_b2_extraction_test.go` | 失败（预跑） |
| X12 | `TestPhase2B2_X12_FullUndoBlocksPlanPartialUndoDoesNot` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X13 | `TestPhase2B2_X13_ImportedHistoricalTimeAndNoTodayTask` | `internal/postgres/phase2_b2_extraction_test.go` | 失败（预跑） |
| X14 | `TestPhase2B2_X14_AllLongSourceSegmentsAndOverlapDeduplicated` | `internal/postgres/phase2_b2_extraction_test.go` | 失败（预跑） |
| X15 | `TestPhase2B2_X15_EmptyExtractionStillSuppliesRawText` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X16 | `TestPhase2B2_X16_DeleteOrReplaceDuringActualModelCall` | `internal/postgres/phase2_b2_extraction_test.go` | 失败（预跑） |
| X17 | `TestPhase2B2_X17_MentionLimitsRetainFirstEightValid` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| X18 | `TestPhase2B2_X18_ArchiveAssistantSkippedButAvailableAsNeighborAndRaw` | `internal/postgres/phase2_b2_extraction_test.go` | 通过（预跑） |
| F1 | `TestPhase2B2_F1_NewJobsPrecedeBackfill` | `internal/postgres/phase2_b2_queue_test.go` | 通过（预跑） |
| F2 | `TestPhase2B2_F2_SubscriptionRetriesUnavailableAndStopsAtFive` | `internal/postgres/phase2_b2_queue_test.go` | 通过（预跑） |
| F3 | `TestPhase2B2_F3_NotConfiguredStopsAndManualRetrySucceeds` | `internal/postgres/phase2_b2_queue_test.go` | 通过（预跑） |
| F4 | `TestPhase2B2_F4_BudgetDefersWithoutAttemptAndCalendarHandlesDST` | `internal/postgres/phase2_b2_queue_test.go` | 通过（预跑） |
| F5 | `TestPhase2B2_F5_ConcurrentBackfillCapAndHourlyRecovery` | `internal/postgres/phase2_b2_queue_test.go` | 通过（预跑） |
| F6 | `TestPhase2B2_F6_PausedArchiveSkippedOrdinaryProcessedResumeWorks` | `internal/postgres/phase2_b2_queue_test.go` | 失败（预跑） |
| F7 | `TestPhase2B2_F7_KilledWorkerRecoversLeaseAndFencesOldCommit` | `internal/postgres/phase2_b2_queue_test.go` | 通过（预跑） |
| F8 | `TestPhase2B2_F8_MeteredCallFailureDoesNotRetry` | `internal/postgres/phase2_b2_queue_test.go` | 通过（预跑） |
| M1 | `TestPhase2B2_M1_FiltersCursorIsolationAndOwnership` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M1 | `TestPhase2B2_M1_ReadBatchQueryCountDoesNotGrowWithRows` | `internal/postgres/phase2_b2_read_count_test.go` | 失败（预跑） |
| M2 | `TestPhase2B2_M2_SnapshotCapDoesNotCapSecretaryOrDeputy` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M3 | `TestPhase2B2_M3_FacetCountsDeletionAndFiftyLimit` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M3 | `TestPhase2B2_M3_FacetsTakeFiftyMostFrequentPerRole` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M4 | `TestPhase2B2_M4_UndoPreservesAnswerAndDeputyReplacesModelHistory` | `internal/postgres/phase2_b2_memory_test.go` | 通过（预跑） |
| M5 | `TestPhase2B2_M5_ExplicitDeleteStillClearsDependents` | `internal/postgres/phase2_b2_memory_test.go` | 通过（预跑） |
| M6 | `TestPhase2B2_M6_AllBatch1SequencesUnchanged` | `internal/postgres/phase2_b2_memory_test.go` | 通过（预跑） |
| M7 | `TestPhase2B2_M7_NewDeputyVisibilityInitializesOnlyOnce` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M8 | `TestPhase2B2_M8_DeleteCleansOrphansButKeepsSharedAndSelf` | `internal/postgres/phase2_b2_memory_test.go` | 通过（预跑） |
| M9 | `TestPhase2B2_M9_CorrectionCarriesMentionsAndEventToNewVersion` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M10 | `TestPhase2B2_M10_DistinctActionableHumanJobMessages` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M11 | `TestPhase2B2_M11_ExportContainsNewTablesAndEventValues` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| U1 | `U1 记忆卡片显示人地点说话和事件日期，没有内容的卡片无空位及内部说法` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U2 | `U2 点人后仅保留提到他的记忆，清掉筛选后恢复；地点和性质筛选传给接口` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U3 | `U3 300条记忆从接口翻至末尾不重复，390px无横向溢出` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U1 | `U1 事件区间卡片显示实际最后一天` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U4 | `U4 真实秘书原话经后台变成带成都老王日期的记忆，点老王可筛出` | `web/tests/phase2-batch2-backend.spec.ts` | 未运行 |
| M12 | `TestPhase2B2_M12_ProjectEpistemicAgentAndCombinedFilters` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| M13 | `TestPhase2B2_M13_DetailIsDirectMemoryAndMissingIsNotFound` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |
| U5 | `U5 项目决定超出200条快照后仍全部出现在时间轴` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| M14 | `TestPhase2B2_M14_ModelFacingMemoryCarriesTimePeoplePlaces` | `internal/postgres/phase2_b2_memory_test.go` | 失败（预跑） |

## 验收方法与接线

- 假模型的抽取 JSON 都由测试定义。涉及原话、时间锚点、相邻消息、最旧记忆和失效历史的用例检查实际 HTTP 请求；U4 使用既有模型请求捕获代理。
- X16 等待假服务实际收到请求后，再删除或替换资料；F5 使用两个独立 Store/连接池及启动屏障；F7 对测试记录的独立子进程发送 kill，租约过期后由另一执行者领取，验证原令牌不能提交。
- M1 造 130 条结构化数据，按人、地点、性质、时间、文字及组合条件筛选，分页中插入新记忆；另通过 pgx query tracer 验证读 1 条和 100 条的查询数相同。M2 造 250 条验证快照限量与秘书/副手完整召回。M3 覆盖删除后分面计数及每类最多 50 个。
- M4/M5 同时验证秘书回答和副手结果的保留/清空区别；M7 验证第一次启用与再次启用；M11 为新增四张表构造非空数据并检查导出事件值。
- 新增浏览器用例已接入 `.github/workflows/browser-regression.yml`：模拟后端列表加一个 spec；真实后端复用已有隔离 runner，以明确 spec 参数运行 U4，不改 runner。

## 发现与待配合

- 本次后端预跑结果和发现见末尾；失败按 X→E1、F→E2、M→M、U→U2 归属记录，不跳过、不放宽、不反复重跑碰运气。
- 已在指定集成基线完成一次后端预跑；最终统一全量等待 M2 和 U2 合入后的通知。
- 本次覆盖不评判真实模型的抽取准确率；真实默认通道验证和上线仍由协调者负责。
- 如运行发现测试夹具或接口语义问题，先记录具体原因并与协调者确认；冻结预期只追加协调者批准的修订。


## 附：旧测试偶发失败定位（待办第 25 项）

2026-10-02，已 fetch，并将本分支变基到 `origin/phase2/batch2` 的 `6dc8719474952febf42793d57f2ea85c5beb87f9`。诊断在该提交的独立、干净工作区执行；Go `1.26.8 linux/amd64`，独立临时 PostgreSQL 16 / pgvector 0.8.2，只用合成数据。没有改旧测试、产品代码或第 1 批断言，没有运行正式 T2 验收。

**结论：本次复现是 `timeout` 子用例的计时与阶段假设过紧，建议修测试，不建议据此改产品取消逻辑。** 100ms 调用者 deadline 在假模型与任务夹具准备之前就启动，测试却无条件要求已经进入模型阶段、捕获原话成功。

复现命令（`PCAS_TEST_DATABASE_URL` 指向临时库；两组样本均完整保留失败）：

```sh
go test -race -count=20 -json -run '^TestSecretaryRejectsStaleRowsAndKeepsOriginalOnCancellation$' ./internal/postgres
go test -race -count=100 -json -run '^TestSecretaryRejectsStaleRowsAndKeepsOriginalOnCancellation$/^timeout$' ./internal/postgres
```

| 样本 | 结果 |
|---|---|
| 正常连接，完整旧测试 20 次 | stale / cancel / timeout 各 20 次通过，0 次失败 |
| 正常连接，仅 timeout 100 次 | 99 次通过，1 次失败；本组观察失败比例 1%，不是稳定概率估计 |
| 外部代理仅延迟夹具 `INSERT INTO work_items` 200ms，timeout 1 次 | 1 次失败；是受控因果验证，不计入正常失败比例 |

自然复现与受控实验均在 `internal/postgres/desk_turn_test.go:521` 报 `context deadline exceeded`，返回的 `ConversationID` 和 `Turn` 为空；不是旧回答被错误更新，也不是捕获后丢失。三组均没有 `DATA RACE` 报告。自然失败不能仅凭返回值分辨是哪个受理/排队检查先看到 deadline，但可以确定尚未进入初始化 `Turn` 的工作回调。

原因链：`desk_turn_test.go:500` 启动 100ms 时限，随后才执行 `secretaryModel`（503）与 `workspaceCommand(addTask)`（516）。后者使用 `context.Background()`，不会随这 100ms 中止，因此可能在进入 `DeskTurn` 前耗尽调用者时间。`DeskTurn` 的初始 Snapshot 使用独立持久化上下文（`desk_turn.go:368–372`），随后受理与排队检查调用者取消（`desk_turn_order.go:59、95、167、216`），到期可直接返回空响应；工作回调才会初始化 Turn（`desk_turn.go:412–413`）并进入模型失败捕获。受控实验实际延迟 200.41ms，代理未观察到 `INSERT INTO desk_turn_order`，与受理前到期路径一致。已有 `TestSecretaryOrderCanceledBeforeAdmission`（`desk_turn_order_test.go:416`）明确要求受理前取消不创建票据，因此不应为了通过本测试而让已过期输入无条件被受理。

建议协调者安排修改 `desk_turn_test.go` 的此子用例：先完成全部夹具准备；以通道确认假模型实际收到请求，验证真正的模型阶段超时；若仍采用真实 deadline，留足入队和数据库准备余量，并让假模型阻塞到取消，不能仅放大 100ms 而保留 1 秒后成功回复的竞速。保留原话、禁止过时更新、WARN/model/timeout 的原断言；受理前取消继续由已有专门用例覆盖。本次没有证明所有取消路径均无竞态，但已定位此次偶发失败的测试原因。

原始 JSON 和代理日志留在诊断环境 `/tmp/pcas-t2-flaky-fb1y3a4u/`（`baseline20.json`、`timeout100.json`、`latency1.json`、`proxy.log`）；容器与代理在诊断后清理。正式 X/F/M/U 验收仍等协调者通知。


## 第二轮补充：经协调者批准的调整（2e2b8f9）

先以 `f6e6767` 追加冻结 `supplement_2e2b8f9`，再改测试。原冻结字段均未修改；第 1 批 P/M/N/G 的文件与断言仍零修改，N12 不改。

- 旧测试 `TestExtractionConfirmationRequiresCurrentVerbatimCapture`：只把 unresolved subject、unresolved predicate、paraphrased assertion 三项预期改成 adopted；新增 0.79 → candidate、0.8 → adopted 边界，其余原项的预期不变。
- 旧测试 `TestExtractionWithoutModelStaysNotConfigured`：改为检查明确、不可重试的 `provider_not_configured`，实际 Worker 处理后 blocked、没有消耗重试、不会自动再领取；配置假模型后手动 retryJob，同一任务完成并留下记忆。以上两组由契约第 7 节明确批准，放在验收 PR #88；尚未运行，等实现合入。
- F5 的 20 / 30 均按资料版本计数，只认 `backfill_queued_at` 非空的 `source.extract` 根任务。造 50 份待补资料，其中一份 18000 字符、产生两段任务；另放 25 个优先级同为 10、标记为空的归档抽取根任务。两个 Store 同时检查，三轮各排 20、10、0 份，之间处理完所有根任务和分段；拨一小时后再排 20 份。按标记核对最近连续一小时的排入数，归档任务与分段不占额外名额。
- M12：12 条固定记忆分别属于两个项目、三类可信度、两位副手的不同可见范围。九组筛选（包括全部新参数与旧 entity/nature/q/from/to 组合、空交集）以 limit=2 翻到末页，每页检查筛选 total，按已知更新顺序比对全部 id。
- M13：详情直接返回记忆对象，核对列表同形字段、提及、说话和事件时间；不存在、其他用户和已删除条目均 404。
- U5：项目三条旧决定全部在 200 条快照之外，另造 250 条更新记忆；打开项目页，检查实际请求 project+nature=decision，并检查三条决定可见且不重复。加另一项目的决定和同项目的事实，确认不混入时间轴。
- U1 的禁止词仍只检查记忆卡片；不扩大到资料库原有标签页、版本按钮等页面文案。

本轮静态验证：`make fmt-check`、`git diff --check`、仓库外仅供编译的 E2 入口 overlay 下 `go test -c` 和 `go vet` 通过；前端 lint / type-check / build、新增 spec 的独立 TypeScript 检查通过；Playwright `--list` 发现 U1–U5 共 6 个用例（U1 含区间末日独立用例）。没有执行这些验收用例，也没有运行全量 `make check`。

偶发失败的计时修复按要求另开 [PR #112](https://github.com/soaringjerry/PCAS/pull/112)，base 为 main，提交 `90bbdcf`，不在本验收 PR。只把原 WithTimeout 100ms 块移到假模型、任务、请求准备完成后，全部断言逐字保留。独立临时库 `-race -count=20` 三个子用例各 20 次通过；外部代理给准备阶段 addTask 增加 200ms 延迟，修复后 timeout 也通过。单次旧测试最长 2.28 秒，本次没有超过一分钟的单条测试。临时数据库、代理和编译占位均清理，原始验证日志保留在 `/tmp/pcas-t2-timeout-fix-rl65nk3w/`。


## 第三轮澄清及首次后端预跑（dda8ef5）

协调者批准：未配置抽取模型时 `attempts=1`（只被领取一次，随后停住，手动重试重新计数）；新增 M14。先以 `1d0fcb1` 追加冻结 `supplement_dda8ef5`，再以 `b104e89` 更新旧测试和 F3 的次数检查、增加 M14。旧冻结记录 `supplement_2e2b8f9.legacy_not_configured.attempts=0` 保留，执行预期只由新增澄清覆盖。其他冻结值、第 1 批 P/M/N/G 测试与产品代码均没有改动。

### 首次完整后端轮：原始结果保留

- 集成基线：`dda8ef5a466840cfd63a8b2f7f11167e7cfaedd5`。执行提交：`b104e89f8009e69bd694df3e03e69c2b18c7c04e`。
- 环境：Go 1.26.8 linux/amd64；独立临时 PostgreSQL 16 / pgvector 0.8.2、UTF8；每条测试使用既有 testStore 创建独立 schema。只用合成数据与假模型。
- `make fmt-check lint build`、`git diff --check` 通过。E2 已合入，本次没有编译 overlay。
- 完整运行 `PCAS_TEST_DATABASE_URL=<临时库> go test -race -timeout 30m -count=1 -json ./cmd/... ./internal/...` 一次，退出码 1。PostgreSQL 包耗时 467.194 秒；其余 11 个有测试的包通过，3 个包无测试。
- 第 2 批 40 条后端序列、43 个测试入口：24 个入口通过，19 个失败；按序列整体算为 23 条通过、17 条包含失败。覆盖表保留这一次的结果，没有把随后定向验证混算为本轮通过。
- M6 的全部第 1 批 P/M/N/G 序列原样通过；N12 单独执行和在 M6 内执行都通过。两组批准修订的旧抽取测试及旧秘书取消测试通过。没有 DATA RACE 报告，没有跳过第 2 批序列。
- 三个既有真实 Codex 用例由它们原有的离线 guard 跳过：TestInstalledCodexHandshake、TestLiveCodexSecretaryAndLegacyFormats、TestLiveContinuityReplay；没有调用真实模型。U1–U5 浏览器未运行，等 U2 合入。

### 首轮发现与测试修正

19 个失败入口逐一定位如下。所有首轮失败仍记为失败；以下修正位于测试代码，不改变冻结的业务预期。

| 涉及序列 | 首轮现象与原因 | 修正 / 归属 |
|---|---|---|
| M1（含查询计数）、M2、M3（两项）、M7、M9、M11、M12、M13、M14：11 个入口 | 共同 b2Library 夹具缺少 Commit 必需的原话证据；M2 的 250 条另超过单次 200 条图记录上限，报 invalid input，尚未进入目标检查 | T2 夹具：先建实体，逐条补真实合成原话及 Evidence，再按 200 条分批提交 |
| X1（悉尼） | 用当地日期字符串匹配 UTC 序列化时间，跨 UTC 日时误报；已写入的时间和区间检查通过 | T2：解析捕获的输入，比较表达时刻精确相等及 timezone 相等 |
| X9、X13 | 把快照里 accepted 的 memory 候选也算作待办候选，实际 task/idea 候选并未新增 | T2：按契约只计 task/idea 候选，原“0 个”预期不变 |
| X11（编辑、确认两种） | 整个对象深比较只在按读取时刻计算的 exposure 浮点值上不同；其他字段完全相同 | T2：只排除该计算值，仍深比较全部存储内容和元数据；版本、提及和事件检查保留 |
| X14 | 检查的模型输入本身是 JSON，整段原文中的换行还在 JSON 字符串内转义，直接子串匹配误报 | T2：解出捕获输入的 source 后逐字符核对两段原文；两次请求、重叠只记一条、items=3 全部保留 |
| X16（删除） | 删除原话连带删除了被租约保护的任务，返回 worker.ErrLeaseLost；测试在检查“无记忆、无实体”之前误把它当异常 | T2：只在实际调用中删除原话时接受该取消标记，保留记忆 / 实体数必须为 0 的检查 |
| F6 | 归档解包之后还没切分消息原文，source.extract 根任务尚未生成，查询 priority 报 no rows | T2 夹具：先完成归档消息和普通资料的真实 source.chunk 阶段，再检查抽取优先级及暂停 / 恢复 |
| M10 | budget_deferred 的说明和建议已经在 Detail 中；测试额外要求可选 Recovery 字段非空而误报 | T2：检查 Detail 与 Recovery 合起来包含说明和行动建议，仍要求两类提示不同、无内部错误码 |

上述测试修正提交 `45f164d`。在同一集成基线上，仅对受改动影响的 20 个入口定向执行一次（含 X1 的原通过补充用例），结果为 18 通过、M7 与 M14 失败，包耗时 76.465 秒。定向命令：

```sh
go test -race -timeout 30m -count=1 -json -run '^TestPhase2B2_(X(1_|9_|11_|13_|14_|16_)|M(1_|2_|3_|7_|9_|10_|11_|12_|13_|14_)|F6_)' ./internal/postgres
```

M7 随后定位出另一项夹具问题：从已有副手复制的对象继承了“已开过记忆”的标记，虽然 Enabled 已设 false，仍不符合“从没启用过 B”的起点。`c2c43b5` 改为构造全新副手，原 `[0,1]`、用户关甲后再启用仍为 `[1]` 的断言不变。只对改动后的 M7 定向验证一次通过（0.55 秒）。两次定向运行都由明确测试代码修正触发，没有重跑同一份代码碰运气，没有再次执行完整后端轮。

### 剩余产品缺口、慢用例和最终一轮

**M14 → M2 跟进。** 修好证据夹具后，memoriesTx 在 manual 副手身份下只返回应当可见的那条记忆，但 expressedAt、eventFrom、eventTo、eventPrecision 均未填，person/place 提及均为空（预期老王 / 成都）。与第 9 节明确的 M2 待补范围一致。当前不改产品，也不降低此断言。第 3 批合并后模型提示词那一行的验证仍由后续集成完成。

按第 7 节列出超过一分钟的测试（均通过）：

| 测试 | 首轮耗时 |
|---|---|
| TestPhase2B1_N12_RandomTwentyActionsInTwentySeededGroups | 62.88 秒 |
| TestPhase2B2_M6_AllBatch1SequencesUnchanged 内的 N12 | 78.40 秒 |
| TestPhase2B2_M6_AllBatch1SequencesUnchanged 整体 | 111.66 秒 |

修夹具后其他受影响入口的定向验证通过，包括 M1 筛选及固定查询数、M2 的 250 条 / 快照 200 条 / 模型可用最旧记忆、M12 新参数组合及 total、M13 直接对象与 404、F6 暂停和恢复。当前提交尚未重跑完整 Go 后端与浏览器，最终统一全量仍等 M2 与 U2 合入后协调者通知；不把不同测试提交的结果合并宣称全量通过。

证据目录 `/tmp/pcas-t2-preliminary-a0r_uibt/`：run-manifest.json、go-test.json / stderr / exit、summary.json（首次完整轮），targeted-head.txt 与 targeted-test.json（45f164d 定向轮），m7-head.txt 与 m7-test.json（c2c43b5 定向轮）。原始日志均保留；本次自建临时数据库容器已停止并移除。
