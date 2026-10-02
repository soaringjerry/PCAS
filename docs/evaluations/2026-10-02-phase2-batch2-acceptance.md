# 第 2 批独立验收：测试准备报告

日期：2026-10-02。任务：T2。阶段：先冻结预期并编写测试；实现合入后等待协调者通知，再在同一个集成提交上统一执行。

## 当前状态

- 起点：`origin/main` 的 `66b1a43`。编写期间没有读取 S0/E1/E2/M/U2 的实现分支。起初没有远端 `phase2/batch2`，按任务包先从 main 建 `phase2/b2-T2-acceptance`，工作区 `/root/PCAS-wt/b2-T2`；随后变基到 `origin/phase2/batch2` 的 `1cec28b`；Draft PR 的 base 为 `phase2/batch2`。
- 被测集成提交号：未指定；尚未进行集成验收。下面的“未运行”不代表通过。
- 第 1 批测试文件及断言：零修改。M6 原样调用现有 P/M/N/G 序列，包括 N12 的 20 组固定种子。
- 不改产品代码，不连接线上数据库、不读线上 `.env` 或 `config/`、不调用真实模型和通知通道。

## 冻结预期与协调修订

1. `825845d`（变基前 `f38f622`） 先提交 `testdata/phase2/b2-gold.json`，随后开始写测试。时间锚点使用 `DateFromToday`，下周一和周五的日期偏移表由验收者独立给出，事件按当地日历构造左闭右开区间；覆盖上海和悉尼。
2. `56369aa`（变基前 `866c662`） 追加 `supplement_5865411`：协调者明确 M6 是第 1 批全部 P/M/N/G 序列不改断言；冻结 `BackfillExtractions(ctx, now)`、`nextBudgetDay(now, loc)`、10 分钟错开上限；F5 改为同一小时排入最多 30 个，处理完各次排队任务后检查并向后拨一小时。原字段 `F5.max_started_hour` 是旧契约记录，执行预期以补充为准。
3. `7952aed`（变基前 `296316d`） 追加 `supplement_32ccb70`（main 合并 `e2175b0`）：X14 每段 12000、步长 11000、重叠 1000；18000 字符的两段资料中，共用句从第 11500 个字符开始，各段独有句各一条，最终 `items = 3`。补充覆盖原 X14 的笼统总数描述。
4. 原 `fixtures.outdated_replacement` 写的是替换语说明，不是第 1 批 R7 的逐字替换语；测试使用后追加的 `supplement_5865411.history_replacement`，原字段不修改。此项属于冻结文件中的未使用描述纠偏，没有改变已有验收预期。

5. `9cc1930` 先追加 `supplement_0f8e759`：协调者冻结“显示事件区间最后一天为 `eventTo` 前一天”。U1 追加 `[2025-06-12, 2025-06-15)` 显示到 14 日的浏览器断言。

## 静态验证

- `make fmt-check` 与 `git diff --check`：通过。编译 overlay 下的 `go vet ./internal/postgres`：通过。
- 后端测试编译：使用仓库外 `/tmp` overlay 为尚未合入的两个 E2 入口提供仅供编译的签名，执行 `go test -c`；未执行编译产物，不作为产品验收结果。临时占位没有提交。
- 前端：`npm run lint && npm run type-check && npm run build` 通过。另对新增 spec 执行 TypeScript 类型检查（本机 Node 类型声明目录）和 ESLint。
- Playwright `--list` 能发现 U1–U4 共 5 个用例；没有启动浏览器执行这些用例。
- `make check` 与真实数据库/浏览器统一验收：按用户要求等待实现合入通知，未运行。

## 覆盖清单

| 序列 | 测试名 | 文件 | 结果 |
|---|---|---|---|
| X1 | `TestPhase2B2_X1_SelfMentionsExpressionAndEvent` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X1 | `TestPhase2B2_X1_ExpressionFallbackIsLimitedToHandEnteredSources` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X2 | `TestPhase2B2_X2_EntitiesSharedAcrossSourcesAndOwnersIsolated` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X3 | `TestPhase2B2_X3_ExactAliasTrimCaseAndType` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X4 | `TestPhase2B2_X4_HallucinatedNameDroppedWithoutDroppingMemory` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X5 | `TestPhase2B2_X5_UnknownExpressionDoesNotResolveRelativeDate` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X6 | `TestPhase2B2_X6_InvalidWhenKeepsClaim` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X7 | `TestPhase2B2_X7_ConfirmationBoundary` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X8 | `TestPhase2B2_X8_QuestionAndOperationProduceNoMemory` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X9 | `TestPhase2B2_X9_SecretaryTaskAlsoRetainsPlan` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X10 | `TestPhase2B2_X10_ReprocessingEnrichesWithoutRevisionOrOutdated` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X11 | `TestPhase2B2_X11_UserEditedOrConfirmedMemoryNotEnriched` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X12 | `TestPhase2B2_X12_FullUndoBlocksPlanPartialUndoDoesNot` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X13 | `TestPhase2B2_X13_ImportedHistoricalTimeAndNoTodayTask` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X14 | `TestPhase2B2_X14_AllLongSourceSegmentsAndOverlapDeduplicated` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X15 | `TestPhase2B2_X15_EmptyExtractionStillSuppliesRawText` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X16 | `TestPhase2B2_X16_DeleteOrReplaceDuringActualModelCall` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X17 | `TestPhase2B2_X17_MentionLimitsRetainFirstEightValid` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| X18 | `TestPhase2B2_X18_ArchiveAssistantSkippedButAvailableAsNeighborAndRaw` | `internal/postgres/phase2_b2_extraction_test.go` | 未运行 |
| F1 | `TestPhase2B2_F1_NewJobsPrecedeBackfill` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| F2 | `TestPhase2B2_F2_SubscriptionRetriesUnavailableAndStopsAtFive` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| F3 | `TestPhase2B2_F3_NotConfiguredStopsAndManualRetrySucceeds` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| F4 | `TestPhase2B2_F4_BudgetDefersWithoutAttemptAndCalendarHandlesDST` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| F5 | `TestPhase2B2_F5_ConcurrentBackfillCapAndHourlyRecovery` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| F6 | `TestPhase2B2_F6_PausedArchiveSkippedOrdinaryProcessedResumeWorks` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| F7 | `TestPhase2B2_F7_KilledWorkerRecoversLeaseAndFencesOldCommit` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| F8 | `TestPhase2B2_F8_MeteredCallFailureDoesNotRetry` | `internal/postgres/phase2_b2_queue_test.go` | 未运行 |
| M1 | `TestPhase2B2_M1_FiltersCursorIsolationAndOwnership` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M1 | `TestPhase2B2_M1_ReadBatchQueryCountDoesNotGrowWithRows` | `internal/postgres/phase2_b2_read_count_test.go` | 未运行 |
| M2 | `TestPhase2B2_M2_SnapshotCapDoesNotCapSecretaryOrDeputy` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M3 | `TestPhase2B2_M3_FacetCountsDeletionAndFiftyLimit` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M3 | `TestPhase2B2_M3_FacetsTakeFiftyMostFrequentPerRole` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M4 | `TestPhase2B2_M4_UndoPreservesAnswerAndDeputyReplacesModelHistory` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M5 | `TestPhase2B2_M5_ExplicitDeleteStillClearsDependents` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M6 | `TestPhase2B2_M6_AllBatch1SequencesUnchanged` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M7 | `TestPhase2B2_M7_NewDeputyVisibilityInitializesOnlyOnce` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M8 | `TestPhase2B2_M8_DeleteCleansOrphansButKeepsSharedAndSelf` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M9 | `TestPhase2B2_M9_CorrectionCarriesMentionsAndEventToNewVersion` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M10 | `TestPhase2B2_M10_DistinctActionableHumanJobMessages` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| M11 | `TestPhase2B2_M11_ExportContainsNewTablesAndEventValues` | `internal/postgres/phase2_b2_memory_test.go` | 未运行 |
| U1 | `U1 记忆卡片显示人地点说话和事件日期，没有内容的卡片无空位及内部说法` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U2 | `U2 点人后仅保留提到他的记忆，清掉筛选后恢复；地点和性质筛选传给接口` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U3 | `U3 300条记忆从接口翻至末尾不重复，390px无横向溢出` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U1 | `U1 事件区间卡片显示实际最后一天` | `web/tests/phase2-batch2.spec.ts` | 未运行 |
| U4 | `U4 真实秘书原话经后台变成带成都老王日期的记忆，点老王可筛出` | `web/tests/phase2-batch2-backend.spec.ts` | 未运行 |

## 验收方法与接线

- 假模型的抽取 JSON 都由测试定义。涉及原话、时间锚点、相邻消息、最旧记忆和失效历史的用例检查实际 HTTP 请求；U4 使用既有模型请求捕获代理。
- X16 等待假服务实际收到请求后，再删除或替换资料；F5 使用两个独立 Store/连接池及启动屏障；F7 对测试记录的独立子进程发送 kill，租约过期后由另一执行者领取，验证原令牌不能提交。
- M1 造 130 条结构化数据，按人、地点、性质、时间、文字及组合条件筛选，分页中插入新记忆；另通过 pgx query tracer 验证读 1 条和 100 条的查询数相同。M2 造 250 条验证快照限量与秘书/副手完整召回。M3 覆盖删除后分面计数及每类最多 50 个。
- M4/M5 同时验证秘书回答和副手结果的保留/清空区别；M7 验证第一次启用与再次启用；M11 为新增四张表构造非空数据并检查导出事件值。
- 新增浏览器用例已接入 `.github/workflows/browser-regression.yml`：模拟后端列表加一个 spec；真实后端复用已有隔离 runner，以明确 spec 参数运行 U4，不改 runner。

## 发现与待配合

- 当前没有产品运行结果，因此没有产品失败清单；实施后的失败按 X→E1、F→E2、M→M、U→U2 归属记录，不跳过、不放宽、失败后不重复跑碰运气。
- 集成分支已建立并已变基；需要协调者合入实现并通知验收提交号，再在指定提交上一次执行全量。
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
