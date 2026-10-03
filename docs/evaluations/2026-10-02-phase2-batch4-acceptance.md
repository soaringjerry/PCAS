# 第 4 批独立验收：测试预先交付（2026-10-02）

本页为原轮预先交付时的历史记录；2026-10-03 原轮已按协调者通知开始统一运行，另补第 11 节 I15–I20、W5。最新实际结果与待确认项见 [2026-10-03 报告](2026-10-03-phase2-batch4-acceptance.md)，不能据此宣布第 4 批通过。

任务：T4。分支 `phase2/b4-T4-acceptance`，工作区 `/root/PCAS-wt/b4-T4`。最初基于 `origin/main` 的 `66b1a43`；当时 `origin/phase2/batch4` 尚不存在；现已按最新通知变基到该集成分支的 `4ac30e6`。Draft PR 的 base 为 `phase2/batch4`，不合 main、不部署。

被测集成提交：**尚未指定**。收到实现合入通知后，在同一个明确提交上统一跑全量；届时追加实际结果、发现及修复归属。

依据：`parallel.md`、第 4 批契约和 T4 任务包；补充接缝依据协调者确认的 `5865411`、`1cec28b` 第 6 节，以及 `b9d26f0` 的 R12、L8/L9 和 `2e2b8f9` 第 7 节、`96c6cb1` 第 8 节。未读取 I/L/U4 的新实现，也未修改产品代码或已有测试预期。

下文冻结提交号是变基前已推送的原始提交，用于记录预期在测试之前冻结的顺序；变基重放未改变原条目。

冻结预期：[b4-gold.json](../../testdata/phase2/b4-gold.json)。`01fc058` 在测试文件创建前提交全部语义预期；`213c4af` 只追加协调者确认的接缝和 I9 修订，原条目不改。`4540d30` 在接上批次里的归档目标之前追加冻结 `message` 和 `archiveId/archiveVersion` 的第二次确认。I9 以补充契约的“取消正在处理的小批、租约过期后重新领取”为准，无需杀进程。

## 序列与测试

下列“待运行”表示只有测试代码，尚无产品验收结论。

| 序列 | 测试 | 状态 |
|---|---|---|
| I1 | [TestPhase2B4_I1_ZipPreviewCountsMediaGapsAndNeverWrites](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I2 | [TestPhase2B4_I2_PartialImportOriginalReachesNewSecretaryConversation](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I3 | [TestPhase2B4_I3_PauseStopsStorageAndExtractionButNotOrdinarySource](../../internal/postgres/phase2_b4_import_test.go)<br>[TestPhase2B4_I3_ImportStateErrorsHaveFrozenCode](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I4 | [TestPhase2B4_I4_ReimportAndExtendedExportOnlyAddUnseenMessages](../../internal/postgres/phase2_b4_import_test.go)<br>[TestPhase2B4_I4_BlockedMessageIsReportedAndSkippedWithoutFailingImport](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I5 | [TestPhase2B4_I5_HistoricalPlanRemainsCandidateWithoutPresentActions](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I6 | [TestPhase2B4_I6_SecretaryOriginalExtractionPrecedes500ImportedMessages](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I7 | [TestPhase2B4_I7_UnsupportedAndDecompressedLimitAreAtomic](../../internal/postgres/phase2_b4_import_test.go)<br>[TestPhase2B4_I7_FourUploadErrorsAreDistinctAndAtomic](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I8 | [TestPhase2B4_I8_RecordCapKeepsNewestAndReportsLeftOut](../../internal/postgres/phase2_b4_import_test.go)<br>[TestPhase2B4_I8_CapPrecedesDisjointDuplicateBlockedAndNewCounts](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I9 | [TestPhase2B4_I9_CancelAtHalfCommitThenRecoverExpiredLease](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I10 | [TestPhase2B4_I10_DeleteArchiveChildrenClaimsBatchAndRecallControl](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I11 | [TestPhase2B4_I11_RegeneratedAnswerPreservesHistoricalBranch](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I12 | [TestPhase2B4_I12_ConcurrentImportsPauseOnlyOneBatch](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I13 | [TestPhase2B4_I13_SpokenDateUsesSydneyAndShanghai](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| I14 | [TestPhase2B4_I14_NonUserMessagesRemainOriginalWithoutExtractionCalls](../../internal/postgres/phase2_b4_import_test.go) | 待运行 |
| L1 | [TestPhase2B4_L1_SecretaryRecordsExactNumbersAndDependencies](../../internal/postgres/phase2_b4_usage_test.go)<br>[TestPhase2B4_L1_CallsPaginationHasNoDuplicatesAndPreservesNumbers](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |
| L2 | [TestPhase2B4_L2_DeputyLegacyAnswerExtractionEachRecordTheirCall](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |
| L3 | [TestPhase2B4_L3_FailureAndSuccessfulRequestReplayDoNotAddRows](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |
| L4 | [TestPhase2B4_L4_LocalDaysAndSydneyDSTHaveExactSummaries](../../internal/postgres/phase2_b4_usage_test.go)<br>[TestPhase2B4_L4_SummarySeparatesPurposesAndOmitsEmptyDays](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |
| L5 | [TestPhase2B4_L5_CurrentUnicodeExcerptDeletedRefAndEveryColumnPrivacy](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |
| L6 | [TestPhase2B4_L6_UsageEndpointsEnforceOwnerAndIsolation](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |
| L7 | [TestPhase2B4_L7_UndoClearAndDeleteKeepExactUsageRows](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |
| L8 | [TestPhase2B4_L8_ReturnedButInvalidExtractionStillRecordsUsage](../../internal/postgres/phase2_b4_usage_test.go)<br>[TestPhase2B4_L8_ReturnedButInvalidSecretaryStillRecordsUsage](../../internal/postgres/phase2_b4_usage_test.go)<br>[TestPhase2B4_L8_CorrectedMemoryDuringDeputyGenerationKeepsReturnedUsage](../../internal/postgres/phase2_b4_usage_test.go)<br>[TestPhase2B4_L8_UsageIsVisibleBeforeResultTransactionAndSurvivesRollback](../../internal/postgres/phase2_b4_usage_test.go)（回滚/取消两个子用例） | 待运行 |
| L9 | [TestPhase2B4_L9_IdenticalLegacyRequestsMakeTwoCallsAndTwoUsageRows](../../internal/postgres/phase2_b4_usage_test.go) | 待运行 |

| 浏览器序列 | 文件与覆盖 | 状态 |
|---|---|---|
| W1 | [phase2-batch4.spec.ts](../../web/tests/phase2-batch4.spec.ts)：选文件、预览全部计数和日期、确认前无导入请求 | 待运行 |
| W2 | 同上：进行中/暂停/完成/失败分别验证进度、暂停/继续操作；390px 无横向溢出 | 待运行 |
| W3 | 同上：四种错误各有不同的人话和处理建议，不暴露错误码 | 待运行 |
| W4 | [phase2-batch4-backend.spec.ts](../../web/tests/phase2-batch4-backend.spec.ts)：临时真实后端，2000 条合成消息、暂停、继续、完成、资料库看原文 | 待运行 |

## 关键断言和接线

- 导出由测试构造真实 ChatGPT `mapping` 拓扑，包括空根、父子节点、`current_node`、角色、时间和再生成分支；zip 加合成图片与音频。
- 预览/导入/进度/暂停/继续/删除全部走 HTTP。I2/I10/I13/I14 和 L1 抓本地假模型实际收到的 HTTP 请求。
- I3/I9/I12 的生产解析处理函数在 goroutine 中真实执行、逐批提交；I9 真取消 context，更新自有任务租约的测试时钟，再用真实 `Claim` 重新领取。没有顺序执行冒充并发，没有故障重跑。
- I3/I6 使用生产队列 `worker.New` 与处理函数；暂停批次不交给抽取模型，普通新资料仍完成；导入队列不抢秘书新原话的抽取顺序。仅清理自有 context/goroutine，不操作线上进程。
- L8 的副手用通道门闩真实扣住假模型 HTTP 响应，确认请求正文含那条记忆后才纠正；结果失败、不采用，花费及旧版本引用仍保留。
- 独立花费事务用例在结果写入触发器中等待自有 advisory lock；另一个连接在结果提交前读到花费，随后注入真实 SQL 回滚或取消处理，记录前后完全相同。只操作临时 schema 的触发器和自有锁，清理自有 goroutine/连接。
- `blocked` 新序列 [TestPhase2B4_I4_BlockedMessageIsReportedAndSkippedWithoutFailingImport](../../internal/postgres/phase2_b4_import_test.go)：先导入两条，HTTP 删除一条并禁止再导入，再预览和导入含这两条及一条新消息的归档。`messages=3`、`alreadyImported=1`、`blocked=1`、`leftOut=0`；本次新存 1 条，但已在库里的 1 条也算存好，所以 `total=stored=2`（第 8 节）、状态 done，旧的禁止项不恢复，原来保留的消息不重复。预览只读且 gaps 有说明。
- W1 追加冻结模拟预览 `messages=17`、`alreadyImported=4`、`leftOut=2`、`blocked=3`，检查界面显示实际新存 8 条；图例不要求给禁止项单列一行，显示冻结的 gaps 说明。模拟批次的 `total=12`，已在库里的 4 条算作已存好；W4 正常归档预览明确检查 `blocked=0`。
- 第 8 节计数补充先在 `664affc` 追加冻结。I4 的重复导入完成为 `4/4`，扩展归档为 `7/7`；I8 混合序列包含超限范围内的已导入/禁止/未见消息，以及保留范围内的已导入/禁止/新消息。预览 `8=3 leftOut+2 alreadyImported+1 blocked+2 新存`，完成 `total=stored=4`；旧的已导入资料保留，禁止项不恢复，超限未见消息不新增，保留消息恰好一份。
- L5 通过 `to_jsonb(u)` 扫描 `model_usage` 每一列，包括 `memory_refs` 和 `plan`；另测 80 个 Unicode 字符的截断、读取当前文本、删除后无正文的标记。
- 悉尼/上海跨日和悉尼夏令时开始、结束使用冻结的历史时间；没有依赖“必须在未来”的日期或今天星期几。
- [browser-regression.yml](../../.github/workflows/browser-regression.yml) 的模拟浏览器列表加入 W1–W3；真实后端 round 1 增加独立 W4 命令，`PCAS_IMPORT_CHUNK_SIZE=1`，`--retries=0`。不改变既有测试预期，不修改共享 runner。

## 当前检查

| 检查 | 结果 |
|---|---|
| gofmt、diff 空白检查 | 通过 |
| JSON 解析与编号静态审计 | I1–I14、L1–L9、W1–W4 全部存在；新增测试没有 `t.Skip` |
| 前端 `npm run lint && npm run type-check && npm run build` | 通过 |
| 两份新 Playwright 文件独立 TypeScript 静态检查 | 通过；Node 类型只装在自有 `/tmp` 目录，未改变仓库依赖 |
| Go 测试仅编译，不执行 | 当前基线不能编译：`ImportChunkSize`、`connectors.MaxUploadBytes` 尚未实现；`MaxArchiveBytes`、`MaxArchiveRecords` 仍为常量。扩展编译诊断未报告其他类型错误 |
| `make check` | 按用户指示，等实现合入通知后运行 |
| 浏览器 W1–W4 | 按用户指示，等实现合入通知后运行 |

已有测试预期修改：**无**。产品代码修改：**无**。评测 R16–R19 属于 V，不在 T4 范围。

## 待协调者确认/配合

1. 集成分支现已创建且 T4 已变基。测试直接使用第 6 节的可调变量，合入 I 后才能编译；Draft PR 自动 CI 当前可能因此失败，不等同于已进行人工统一验收。
2. 指定实现全部合入后的被测提交，通知 T4 运行统一全量。运行临时 pgvector 16、tmpfs、随机本机端口，设 `PCAS_TEST_DATABASE_URL`，不读线上 `.env` 或 `config/`，不调真实模型，不发真实通知。
3. 两处接缝均由协调者确认并写入 `1cec28b`：错误说明使用 `message`，只断言非空且四类互不相同；I10 从批次接口读取 `archiveId/archiveVersion`，沿用 `POST /v1/memory/delete` 和 `include_sources: true`。这些接口字段均已确定。
4. `b9d26f0` 新增的 L8/L9 已先追加冻结（`1387dde`），没有改动 L1–L7 的预期。L8 抽取和秘书用 HTTP 200 返回非空但截断的 JSON；分别检查输出确实被拒绝、模型仅调用一次、记录恰好一行、token 和花费准确。L9 给旧问答入口传两次完全相同的参数，检查真实模型调用两次、产生两行独立记录并对应两个回答。副手样例已经由协调者在 `2e2b8f9` 第 7 节确认：自由文本不做格式不合格假设，改为生成期间纠正一条实际用到的记忆，返回后运行失败且不采用，但花费仍记一行。新增条目在 `ac1ccf2` 冻结后才编写测试，先前冻结的条目不改，副手场景以该补充为准。

正式结果追加时逐条写清现象、契约预期、应修任务及未覆盖之处；每个失败都保留，不跳过、不放宽，不重复运行碰运气。
