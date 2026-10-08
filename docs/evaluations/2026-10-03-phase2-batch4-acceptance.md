# 第 4 批 T4：最终分支的整段协议夹具迁移通过（2026-10-03）

本轮从 `origin/phase2/final` 的 `e775cd688355ce4aef98c06dd38b95b6332073eb` 建分支 `phase2/T4-final-conversation-fixtures`。验收提交为 `602bf083d00d179ce2af79d2fd445a02383d042f`；后续报告提交只增加本轮结果，不改测试或产品代码。Draft PR 的 base 为 `phase2/final`。

依照 README §12、§12.1、§13 第 6、7 条，迁移 I5、I10、I14 的夹具入口和假模型输出。先提交追加式 golden 协议说明（`801e18e`），再提交测试；原有 golden 全部键值保持一致。真实上传、存完后通过 HTTP `organize` 解除「先不整理」，由真实 worker 领取整段任务；测试不制造对话任务或绕过领取条件。I5 的输出改为合法的记忆项并带 `message_index=1`，不再使用旧的 task/signals 输出；I10 根据整段请求中的用户消息返回相应记忆；I14 检查整段协议的 `extractor=3` 处理记录。

| 编号 | 预期（保留） | 实际 |
|---|---|---|
| I5 | 历史计划保持待确认；保留历史说话时间，不产生今天待办，不唤醒搁置想法 | 真实对话整理调用 1 次；生成指定计划，confirmation=candidate、时间一致；Tasks 为空，想法仍搁置且无 Wake；通过 |
| I10 | 删除归档后原话、抽出的记忆、批次消失，不能再次召回 | 3 段对话各整理 1 次，指定历史细节生成记忆；删除前真实秘书请求包含该细节；删除后全部消息、目标记忆、归档和批次均消失，后续秘书请求不含该细节；通过 |
| I14 | assistant/system/tool 原话、角色、时间保留，不为无用户消息的对话调用抽取；AI 原话仍可回忆 | 整理模型调用 0 次；每条 empty/items=0/extractor=3；原话、角色、时间一致；随后秘书调用 1 次并收到 AI 原话；通过 |

验证：三条定向 `go test -race -count=1 -v` 为 **3/3 通过**。同一验收提交上执行实际 **`GOFLAGS=-v make check`，退出码 0**：格式检查、`go vet`、全量 race 测试、构建均通过。顶层测试 **520 通过、0 失败、3 跳过**（包括子测试为 1370 通过、0 失败、3 跳过）；T4 **39/39**；4b 的 C1–C12 和 worker 子进程入口 **13/13**。PostgreSQL 包用时 658.794 秒。没有 race 报告，也没有新增跳过。

三个既有真实通道测试按环境条件跳过：`TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`。此次没有调用真实模型、真实通知或线上数据库。数据库为本轮自建 tmpfs PostgreSQL/pgvector、随机本机端口，模型全部为本地假服务；测试结束后已按记录的容器 ID 清理。

原始证据：`/tmp/pcas-T4-final-go-Ui8vB1/targeted.log`、`make-check.log`、`commit.txt`、`targeted.exit`、`make-check.exit`、`results.json`、`container-cleanup.log`。改动范围为三条测试、golden 的追加协议说明和本报告。本轮没有发现产品行为与上述预期不符。

---

以下保留上一轮及历史验收记录。

# 第 4 批独立验收：c093e0a 修复后通过（2026-10-03）

最新执行记录见「c093e0a 修复后的同一提交全量验收」。下文 §13 裁定轮、原轮和第一次新增序列结果均保留为历史，不替换为本轮结果。

## c093e0a 修复后的同一提交全量验收

产品基线：`c093e0a5568cad3c1e307076c70a6b41dcc569a8`，已 fetch 并变基。最终被测验收提交：`58043322d9b8bd3b520a937aa21e3d383b904e3d`，固定工作区 `/root/PCAS-wt/b4-T4-c093e0a-final-run`；Go 全仓、I1–I20/L1–L9、W1–W5 全部在这个提交运行，不拼接不同提交的通过结果。没有读取新后端实现来推导预期。

协调者已定位旧默认假模型响应的问题：它同时包含 reply/answer/used/actions/items/output，额外字段不属于抽取格式，三次解析失败时 organized=0 是正确行为。按明确指示，在 `bbe508b` 先追加冻结合法空抽取夹具，再于 `e39e68b` 把默认内容改为 `{"items":[]}`。全部旧黄金条目、organized 增长、普通/新资料优先级、L8 独立可见及 rollback/new cancel 的预期保持。I3/I6 上轮还受到无效假输出影响；历史分段优先级为 0 的产品问题由 #133 修复。上述归因来自协调者裁定，本执行者不反推实现。

初次 `e39e68b` 的浏览器为 8 通过、1 失败，真实 W4 通过。W5 now 的旧 getByText.first 定位到背景说明中的「现在就整理」，点击被预览弹窗拦截；它尚未到达确认导入和字段断言。`5804332` 只把选择限定到预览弹窗内的 radio/option，行为预期不变。为保持同一提交的统一全量，停止了尚未完成的旧 Go 运行，只终止自有 Go/test 子进程，自有临时数据库已清理；该中断不算 Go 验收结果。首轮日志/JSON/轨迹保留在 `/tmp/pcas-b4-c093e0a-go-D4clbB`、`/tmp/pcas-b4-c093e0a-browser-23Phip`、`/tmp/pcas-b4-c093e0a-w4-me2ddP` 和初次固定工作区中，没有把已知夹具失败当作产品失败或碰运气重跑。

最终 Go 命令：`make fmt-check lint`；`go test -race -count=1 -v -timeout 30m ./cmd/... ./internal/...`；`make build`，包含 make check 全部步骤。使用自有 tmpfs pgvector 16 容器和随机本机端口，模型/向量/通知均本地假服务，不调用真实模型、不读线上 .env/config。

完整 Go **退出 0**：仓库主测试 **453 通过、0 失败、3 个既有条件测试跳过**；T4 **39 个主测试全部通过、0 失败、0 跳过**。postgres 包耗时 650.296 秒。fmt-check、vet、make build 均通过，三个步骤退出码均为 0，自有数据库已清理。

| 序列 | 结果 | 对应主测试 |
|---|---|---|
| I1 | 1 通过 | `TestPhase2B4_I1_ZipPreviewCountsMediaGapsAndNeverWrites` |
| I2 | 1 通过 | `TestPhase2B4_I2_PartialImportOriginalReachesNewSecretaryConversation` |
| I3 | 2 通过 | `TestPhase2B4_I3_PauseStopsStorageAndExtractionButNotOrdinarySource`<br>`TestPhase2B4_I3_ImportStateErrorsHaveFrozenCode` |
| I4 | 2 通过 | `TestPhase2B4_I4_BlockedMessageIsReportedAndSkippedWithoutFailingImport`<br>`TestPhase2B4_I4_ReimportAndExtendedExportOnlyAddUnseenMessages` |
| I5 | 1 通过 | `TestPhase2B4_I5_HistoricalPlanRemainsCandidateWithoutPresentActions` |
| I6 | 1 通过 | `TestPhase2B4_I6_SecretaryOriginalExtractionPrecedes500ImportedMessages` |
| I7 | 2 通过 | `TestPhase2B4_I7_UnsupportedAndDecompressedLimitAreAtomic`<br>`TestPhase2B4_I7_FourUploadErrorsAreDistinctAndAtomic` |
| I8 | 2 通过 | `TestPhase2B4_I8_RecordCapKeepsNewestAndReportsLeftOut`<br>`TestPhase2B4_I8_CapPrecedesDisjointDuplicateBlockedAndNewCounts` |
| I9 | 1 通过 | `TestPhase2B4_I9_CancelAtHalfCommitThenRecoverExpiredLease` |
| I10 | 1 通过 | `TestPhase2B4_I10_DeleteArchiveChildrenClaimsBatchAndRecallControl` |
| I11 | 1 通过 | `TestPhase2B4_I11_RegeneratedAnswerPreservesHistoricalBranch` |
| I12 | 1 通过 | `TestPhase2B4_I12_ConcurrentImportsPauseOnlyOneBatch` |
| I13 | 1 通过 | `TestPhase2B4_I13_SpokenDateUsesSydneyAndShanghai` |
| I14 | 1 通过 | `TestPhase2B4_I14_NonUserMessagesRemainOriginalWithoutExtractionCalls` |
| I15 | 2 通过 | `TestPhase2B4_I15_LaterStoresIndexedOriginalsWithoutExtracting`<br>`TestPhase2B4_I15_NowPositiveControlCallsExtraction` |
| I16 | 1 通过 | `TestPhase2B4_I16_HeldOriginalReachesSecretaryInAnotherConversation` |
| I17 | 1 通过 | `TestPhase2B4_I17_StartOrganizingReleasesRealPriorityTenExtraction` |
| I18 | 1 通过 | `TestPhase2B4_I18_NewSecretaryUtteranceExtractsWhileImportHeld` |
| I19 | 1 通过 | `TestPhase2B4_I19_HeldImportPausesResumesAndDeletesItsClosure` |
| I20 | 1 通过 | `TestPhase2B4_I20_OmittedOrganizeDefaultsToHeldIndexedOriginals` |
| L1 | 2 通过 | `TestPhase2B4_L1_SecretaryRecordsExactNumbersAndDependencies`<br>`TestPhase2B4_L1_CallsPaginationHasNoDuplicatesAndPreservesNumbers` |
| L2 | 1 通过 | `TestPhase2B4_L2_DeputyLegacyAnswerExtractionEachRecordTheirCall` |
| L3 | 1 通过 | `TestPhase2B4_L3_FailureAndSuccessfulRequestReplayDoNotAddRows` |
| L4 | 2 通过 | `TestPhase2B4_L4_LocalDaysAndSydneyDSTHaveExactSummaries`<br>`TestPhase2B4_L4_SummarySeparatesPurposesAndOmitsEmptyDays` |
| L5 | 1 通过 | `TestPhase2B4_L5_CurrentUnicodeExcerptDeletedRefAndEveryColumnPrivacy` |
| L6 | 1 通过 | `TestPhase2B4_L6_UsageEndpointsEnforceOwnerAndIsolation` |
| L7 | 1 通过 | `TestPhase2B4_L7_UndoClearAndDeleteKeepExactUsageRows` |
| L8 | 4 通过；rollback/new cancel 两子用例也通过 | `TestPhase2B4_L8_CorrectedMemoryDuringDeputyGenerationKeepsReturnedUsage`<br>`TestPhase2B4_L8_UsageIsVisibleBeforeResultTransactionAndSurvivesRollback`<br>`TestPhase2B4_L8_ReturnedButInvalidExtractionStillRecordsUsage`<br>`TestPhase2B4_L8_ReturnedButInvalidSecretaryStillRecordsUsage` |
| L9 | 1 通过 | `TestPhase2B4_L9_IdenticalLegacyRequestsMakeTwoCallsAndTwoUsageRows` |

关键复核：I15 now 对照和 I17 的 organized 增长检查通过；I3 暂停后普通资料照常整理、存储/整理稳定和恢复完成通过（21.98 秒）；I6 新原话优先于历史队列抽取通过（5.40 秒）；L8 rollback/new cancel 都实际先在另一连接读到独立提交的 1 行，SQL 回滚/取消后仍是同一行，未要求取消停止写回答；旧 batch2 F6 通过（0.29 秒），T4 未改该旧测试。

三个既有跳过是 TestInstalledCodexHandshake、TestLiveCodexSecretaryAndLegacyFormats、TestLiveContinuityReplay，未配置真实服务；本轮未新增跳过或调用真实模型。最终统一提交没有重试，没有剩余失败。第 4 批全部已执行验收通过（Go 39 + 浏览器 10 = 49 条主用例），不以不同提交拼接通过结果。

最终模拟浏览器：**9 通过、0 失败、0 跳过、0 重试、0 flaky**（15.926 秒），W1 两条、W2 四种状态、W3、W5 两条均通过。最终真实后端 W4：**1 通过、0 失败、0 跳过、0 重试、0 flaky**（测试 24.8 秒，Playwright 总耗时 27.475 秒）；PCAS_IMPORT_CHUNK_SIZE=1，两千条合成导出，真实预览/导入/暂停/五次稳定读取/继续到 2000/2000/资料库打开原话/390px 无溢出/无 pageerror 均通过，没有人为延时。两类浏览器 commit.txt 均为 5804332。

前端 npm ci/lint/type-check/build 通过；最终定位器改动另行 lint/type-check 通过。最终工作区复用初次同产品基线的 node_modules 与 dist，前端产品代码未改变。本机 Node 20.19.5，npm 有 package 要求 >=22.12 的 engine 警告，不影响本轮已执行检查。

原始产物：完整 Go（历史临时日志未保存在仓库：`/tmp/pcas-b4-c093e0a-final-go-sy1iPK/make-check.log`）、模拟浏览器（历史临时日志未保存在仓库：`/tmp/pcas-b4-c093e0a-final-browser-eKrLwf/mocked.log`）、真实 W4（历史临时日志未保存在仓库：`/tmp/pcas-b4-c093e0a-final-w4-0FkRMl/w4.log`）。浏览器截图/失败轨迹策略产物在最终固定工作区 `web/test-results/phase2-b4-c093e0a-final-mocked`、`web/test-results/phase2-b4-c093e0a-final-real`；成功用例的截图与 JSON 保留。主测试计数、子用例和逐序列结果已从完整原始输出逐项核对。

## 以下为 §13 裁定轮和此前历史


## §13 裁定后的统一运行与补充复核

产品固定在 `41902ca34fbd78c6eef42b8444a98fda7991c252`。§13 从 PR #129 的 `52eaf186` 读取，先在 `4e9e615` 追加冻结，再于 `ff93622` 实施；后续第 4、5 条从 `6a2dc94` 读取，先在 `22dfaf3` 追加冻结，再于 `fedafac` 实施。所有此前黄金条目保留，未改产品代码或既有其他批次测试。即使集成远端在运行期间更新，本轮固定工作区没有随之变化。

已落实的调整：

- b4API 按主程序的 Options 补 `Editor: s`，注册现有删除入口；I4/I8/I10/L5/L7 删除、禁止再导入、闭包和花费隐私预期不变。
- W1/W2 的禁词列表不变，范围改为导入上传/进度区域及导入预览弹窗，不再扫描其他设置内容。
- L3/L8 去掉秘书 HTTP/Go 错误通道断言，保留实际模型请求、失败 0 行、非法格式返回 1 行、准确数字、隐私和重放不增加的花费检查。
- 只在 I3/I12/I19 暂停后的解析完成处接受 `errors.Is(err, worker.ErrLeaseLost)`，其他解析完成处仍拒绝这个错误；增加/保留暂停后 stored、organized 稳定、其他资料/批次继续、恢复后完成且不重复。I3/I12 显式使用 organize=now，保证暂停检查覆盖原本可以被领取的抽取任务；I19 保持 later 和独立的 hold。
- W1/W4 确认按钮及 W2 failed 继续按钮补齐实际可访问标签的定位：「导入 N 条」「导入 2,000 条」「接着导」。W2/W4 依据第 5 条把「已存好」加入存完状态匹配，数量、HTTP 和后续操作检查保留。
- L8 cancel 按第 4 条只验取消前独立可见、取消后同一花费行保留；门闩释放后允许回答正常写入，不要求 Go error、回答不存在或十秒内停止。rollback 的 SQL 回滚、独立可见、花费保留及回答未提交预期不变。修改后的 cancel **仅编译，未执行**，等协调者通知后端修复合入再跑。

### 同一提交上的统一运行

被测验收提交：`ff93622669acc3b2cc068be7ed7ddf4090e3ea07`，固定工作区 `/root/PCAS-wt/b4-T4-ruling-run`。全部 Go 主测试（含 I1–I20、L1–L9）、W1–W3/W5 的 9 条模拟浏览器和真实后端 W4 在此提交各执行一次，未跳过新失败或修改断言碰运气。Go 使用自有 tmpfs pgvector 容器；W4 另用自有临时库和后台进程；模型/向量/通知都是本地假服务。

Go 命令：`make fmt-check lint`；`go test -race -count=1 -v -timeout 30m ./cmd/... ./internal/...`；`make build`，包含 make check 的所有步骤，测试失败也继续收集构建结果。fmt-check、vet、make build 均通过；完整测试退出 1。postgres 包耗时 806.438 秒。仓库主测试合计 **447 通过、6 失败、3 个既有条件测试跳过**；T4 的 39 个主测试为 **34 通过、5 失败、0 跳过**。

| 序列 | 统一运行结果 | 实际观察 / 未到达部分 |
|---|---|---|
| I1 | 通过 | 真实 zip 预览、干扰媒体、计数、日期和只读 |
| I2 | 通过 | 还没导完时原话进入另一段秘书对话模型请求 |
| I3 | 1 通过、1 失败 | 状态错误码通过；暂停允许 ErrLeaseLost 和当前小批存储界限已过，普通新资料抽取等待 30 秒超时，随后的持续稳定/恢复检查未到达 |
| I4 | 2 通过 | 禁止重新导入、blocked 计数、重复/扩展导出均通过 |
| I5 | 通过 | 历史计划仍待确认且不变成今天的动作 |
| I6 | 失败 | 新秘书原话抽取等待 30 秒超时；预期不动，等后端通知 |
| I7 | 2 通过 | 不支持内容、解压限额、四种不同人话错误及原子性 |
| I8 | 2 通过 | 最新消息限额以及 leftOut/alreadyImported/blocked/新存互斥计数 |
| I9 | 通过 | 一半时真实取消，租约过期恢复后不重不丢 |
| I10 | 通过 | HTTP 删除归档，消息/记忆/批次消失，后续模型不再收到被删原话 |
| I11 | 通过 | 被放弃的回答分支保留历史性质 |
| I12 | 通过 | 真实并发两批，暂停一批稳定，另一批完成；恢复目标批后全部完成且不重复 |
| I13 | 通过 | 悉尼、上海原话日期 |
| I14 | 通过 | 非用户消息保留且不调用抽取模型 |
| I15 | later 通过、now 对照失败 | later 存储/词/向量齐全且零抽取；now 存好 3/3、实际调用 3 次，organized=0 |
| I16 | 通过 | 另一段秘书对话实际模型请求收到 held 原话 |
| I17 | 失败 | HTTP 释放 hold、优先级 10、原话进入 3 次实际抽取请求均通过，organized 仍为 0 |
| I18 | 通过 | held 导入不阻止新秘书原话抽取 |
| I19 | 通过 | 接受暂停控制返回后，stored/organized 稳定，恢复存完 2000 条且无重复，hold 保持，删除消息与批次闭包 |
| I20 | 通过 | 省略 organize 等同 later，索引齐全，零抽取 |
| L1 | 2 通过 | 精确花费/依赖及分页 |
| L2 | 通过 | 副手、旧问答、抽取各自花费；三个子用例通过 |
| L3 | 通过 | 模型失败 0 行，成功 1 行，重放无新调用/记录；不要求 HTTP 错误 |
| L4 | 2 通过 | 时区、悉尼夏令时、用途分组、空日和准确汇总 |
| L5 | 通过 | Unicode 80 字符、删除引用不带文字、model_usage 每列无正文 |
| L6 | 通过 | 本人权限及用户隔离 |
| L7 | 通过 | 撤销/清空/删除保留原花费 |
| L8 | 3 通过、1 失败 | 副手上下文失效、非法抽取、非法秘书格式仍各记一行均通过；独立事务 rollback/cancel 均在模型返回、结果事务等待时读到 0 行，预期为 1 行，后续断言未到达 |
| L9 | 通过 | 相同旧问答请求两次实际调用、两行花费 |

五处原删除 404（G01/I4、G04/I10、G06/I8、G12/L7、G13/L5）在补 Editor 后均完整通过，没有改删除和计数预期。T4 的剩余失败清单：

1. I3：预期暂停导入不影响普通新资料抽取；实际等普通新资料抽取 30 秒超时。这是通过暂停控制返回后新到达的失败，保持普通资料应继续的预期，交协调者/后端核对，不推断根因。
2. I6：预期新秘书原话先于历史导入队列完成抽取；实际 30 秒超时，保持原样。
3. I15 now 对照：预期显式 now 有真实抽取且 organized 增长；实际模型 3 次，organized=0。
4. I17：预期开始整理后 organized 增长；实际释放/领取/请求内容已核对，模型 3 次，organized=0。
5. L8 独立事务：预期返回后另一连接可见 1 行、回滚/取消后仍保留；实际 rollback/cancel 都先读到 0 行，后续保留与结果检查未到达。此处是 ff93622 的旧取消分支结果，**不是** fedafac 修改后的取消子用例验收。

另一个仓库失败是既有 `TestPhase2B2_F6_PausedArchiveSkippedOrdinaryProcessedResumeWorks`：预期恢复后能领取归档抽取，实际 `resume claim <nil>`。未修改或重跑旧测试；§13 第 6 条已把 F6 等旧归档用例交 T2，报告交协调者对照该归属，不自行判断实现根因。三个既有跳过为 `TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`，未配置真实服务，本轮未新增跳过。

模拟浏览器统一运行：**7 通过、2 失败**，0 跳过、0 重试、0 flaky。W1 两条、W2 importing/paused、W3、W5 两条通过；W2 done 已到达数量/进度/禁词检查，失败在旧的「完成|已导入」匹配，实际「已存好」；W2 failed 已到达人话错误，失败在旧定位器未匹配「接着导」。W4 统一运行的真实预览计数 2000 通过，随后旧定位器不识别「导入 2,000 条」，120 秒超时，没有导入 POST。这三处后续更改和复核各自记录，不把统一运行结果改为通过。

统一运行产物：Go（历史临时日志未保存在仓库：`/tmp/pcas-b4-ruling-go-zmQ2W6/make-check.log`）、模拟浏览器（历史临时日志未保存在仓库：`/tmp/pcas-b4-ruling-browser-CyYjdD/mocked.log`）、W4（历史临时日志未保存在仓库：`/tmp/pcas-b4-ruling-w4-nIBd4g/w4.log`）。失败截图和轨迹位于固定工作区的 `web/test-results/phase2-b4-ruling-mocked` 与 `web/test-results/phase2-b4-ruling-real`。

### 明确改变测试后的补充复核

`4424ca8` 只修正标签定位：W2 failed 单独通过；W4 已通过实际暂停、继续和最终 2000/2000，随后失败在「完成|已导入」文案，资料库检查尚未到达。保留该轮 W2（历史临时日志未保存在仓库：`/tmp/pcas-b4-labels-browser-jCJKh1/mocked.log`）、W4（历史临时日志未保存在仓库：`/tmp/pcas-b4-labels-w4-ChnI0R/w4.log`） 和该固定工作区 `/root/PCAS-wt/b4-T4-browser-label-run` 的截图/轨迹。

`fedafac5a463ae0ca5b3015552f5fc50fa18a302` 按新增裁定加入「已存好」后：**9 条模拟浏览器全部通过，真实后端 W4 通过**，均 0 跳过、0 重试、0 flaky。W4 耗时 41.5 秒（Playwright 总耗时 46.0 秒）：两千条预览、确认后真实导入、当前小批后暂停、五次实际读取时 stored 稳定、继续到 stored=total=2000、资料库打开「合成历史0」原话、390px 无溢出和无 pageerror 全部通过，没有人为延时。使用固定工作区 `/root/PCAS-wt/b4-T4-followup-run`，npm 依赖和前端产物复用统一工作区；产品前端未变。补充复核输出：模拟浏览器（历史临时日志未保存在仓库：`/tmp/pcas-b4-followup-browser-hdzpiU/mocked.log`）、W4（历史临时日志未保存在仓库：`/tmp/pcas-b4-followup-w4-NsmWJR/w4.log`），截图在该工作区 `web/test-results/phase2-b4-followup-mocked`、`web/test-results/phase2-b4-followup-real`。

前端 npm ci/lint/type-check/build 通过，最新修改另行 lint/type-check 通过；本机 Node 20.19.5，npm 仍给出 package 要求 >=22.12 的 engine 警告。L8 最新 cancel 修改仅作 Go 编译检查（`go test -run '^$' ./internal/postgres`），没有执行该用例。L8 独立花费事务的核心可见性预期、I15 now 对照、I17、I6 保留，等协调者通知后端修复合入再专项重跑。补充复核不当作新提交上的 Go 全量通过，本批尚未宣布通过。

## 以下为既有运行历史

当前产品基线：`origin/phase2/batch4` 的 `41902ca34fbd78c6eef42b8444a98fda7991c252`，已 fetch 并变基。原轮产品基线仍为 `b526328c6529056513bf533d370d799f09430b33`；下面的原轮结果没有替换成新基线结果。CI 冲突处理保留 batch2 和 batch4 的步骤。

本轮被测提交：`c719783`（该产品基线 + 原有 T4 测试与 CI 接线）。所有原轮 Go、模拟浏览器、真实后端 W4 均在 `/root/PCAS-wt/b4-T4-original-run` 的同一固定提交执行，没有加入 I15–I20、W5。没有改产品代码，没有读取 I/L/U4 的新实现，没有修改原有验收文件的预期。

## 新增序列：41902ca 上的运行结果

第 11 节和 T4 末尾补充的预期先在 `631ab49` 追加冻结到 [b4-gold.json](../../testdata/phase2/b4-gold.json)，所有旧条目不变。第 12 节 C 序列属于补充批 4b，未编写。

| 序列 | 新测试 | 当前状态 |
|---|---|---|
| I15 | `TestPhase2B4_I15_LaterStoresIndexedOriginalsWithoutExtracting`；`TestPhase2B4_I15_NowPositiveControlCallsExtraction` | later 通过；now 对照失败：存好 3/3、实际抽取调用 3 次，但 organized=0 |
| I16 | `TestPhase2B4_I16_HeldOriginalReachesSecretaryInAnotherConversation` | 通过：另一段秘书对话的真实模型请求带有原话，hold 保持 |
| I17 | `TestPhase2B4_I17_StartOrganizingReleasesRealPriorityTenExtraction` | 失败：HTTP 释放 hold、优先级 10、实际模型调用 3 次和原话内容检查通过，但 organized=0 |
| I18 | `TestPhase2B4_I18_NewSecretaryUtteranceExtractsWhileImportHeld` | 通过：新的秘书原话照常抽取，held 导入仍未整理 |
| I19 | `TestPhase2B4_I19_HeldImportPausesResumesAndDeletesItsClosure` | 失败：暂停后的 ProcessAttachment 返回 job lease lost，后续继续/删除未验到；同原轮暂停口径待确认 |
| I20 | `TestPhase2B4_I20_OmittedOrganizeDefaultsToHeldIndexedOriginals` | 通过：省略字段等同 later，原话索引齐全，零抽取 |
| W5 | `W5 默认先存着；存好后开始整理，进度随接口返回增长`；`W5 确认前可以改为现在整理，表单实际发送 now` | 修正 T4 定位器后两条通过；首次两条失败记录保留 |

后端测试在 [phase2_b4_deferred_test.go](../../internal/postgres/phase2_b4_deferred_test.go)。分段、分词、向量和抽取都由真实 `Claim` 与处理函数运行，不强制领取被 hold 的抽取任务；直接检查每条原话的 chunk、词索引、向量、未领取任务和 `hold_organizing`，向量只用本地免费的确定性服务，单独抓请求，不混入抽取模型调用数。

I16 抓另一段秘书对话的真实 HTTP 请求；I17 通过 HTTP 释放 hold，并验证真实抽取及优先级 10；I18 用真实秘书入口记录新原话；I19 在解析 goroutine 正在逐批提交时真实暂停、继续，再走归档删除闭包。W5 在既有模拟浏览器文件中，随已有 CI 文件入口自动包含。

本次 Go 被测提交 `8e904ad2481eba06cf184c4bc2f26058eb7105a3`（41902ca + 变基后的 T4 测试）：`go test -race -count=1 -v -timeout 10m -run '^TestPhase2B4_I(1[5-9]|20)_' ./internal/postgres`。7 个主测试：**4 通过、3 失败、0 跳过**，包耗时 6.453 秒，退出 1，没有重复运行。数据库为自有 tmpfs pgvector 容器，已清理；模型和向量为本地假服务。原始输出：deferred.log（历史临时日志未保存在仓库：`/tmp/pcas-b4-deferred-41902ca-MHnm08/deferred.log`）。I15 now 与 I17 的 fake 返回非空 JSON（items 空数组），实际各有 3 次调用；R4 将 organized 定义为已经处理过抽取的条数，本测试要求它增长，未把空 items 当作放宽计数的理由。交 I 核对处理完成后的计数；报告只陈述观察，不推断实现根因。

W5 使用同一产品基线的前端构建产物，npm ci/lint/type-check/build 均通过；本机 Node 20.19.5，仍有 package 的 >=22.12 engine 警告。初次筛选 `--grep '^W5 '` 未选中用例，属运行夹具，不计测试结果。改正筛选后 `8e904ad` 上的两条 W5 均超时：默认「先存着」已显示，但测试寻找「确认导入/开始导入/确认」按钮，页面实际可访问标签是「导入 6 条」；now 用例寻找「现在整理」等文案，实际是「现在就整理」。轨迹只有 preview，没有确认导入 POST，后续行为未验到。首轮日志/JSON：mocked.log（历史临时日志未保存在仓库：`/tmp/pcas-b4-w5-41902ca-UwFGBc/mocked.log`）、mocked.json（历史临时日志未保存在仓库：`/tmp/pcas-b4-w5-41902ca-UwFGBc/mocked.json`）。

T4 在 `25dadd66a79a0161f13a73d29cfe2621b3b5cc59` 只补 W5 定位器对上述标签的匹配；冻结条目及行为断言不变，W1 等旧用例不改。在该明确修正后的提交执行 `npx playwright test tests/phase2-batch4.spec.ts --grep 'W5 ' --retries=0 --reporter=list,json`，**2 通过、0 失败、0 跳过、0 重试、0 flaky**：默认 later、实际 multipart 字段、存完的人话状态、开始整理 HTTP 与进度增长、可选 now 及实际字段、390px 无溢出和无 pageerror 均通过。复核输出：mocked.log（历史临时日志未保存在仓库：`/tmp/pcas-b4-w5-41902ca-FzMeDA/mocked.log`）、mocked.json（历史临时日志未保存在仓库：`/tmp/pcas-b4-w5-41902ca-FzMeDA/mocked.json`）。截图在当前工作区 `web/test-results/phase2-b4-w5-41902ca`；首次轨迹目录被同名输出覆盖，仅保留首轮日志/JSON及本段已核对的观察。修正后 lint/type-check 另行检查也通过。此次是新增序列专项运行，未在 41902ca 重跑原轮或宣称全量通过。

## 三处待协调者确认的口径

1. **W1/W2 禁止词范围**：预期是不给用户展示内部术语；测试扫描整个设置页，实际已有导出说明含「来源」。需确认范围是整页还是导入区域，当前旧断言保留。
2. **L3/L8 秘书错误通道**：测试分别假设模型 HTTP 503 后入口返回 HTTP 错误、非法 JSON 后 DeskTurn 返回 Go 错误；实际为 HTTP 200 / nil error 的 fallback。需确认允许既有 fallback 时是否只按结果未采用及花费行为验收，当前断言保留。
3. **I3/I12/I19 暂停控制返回**：测试假设暂停后的解析正常返回；实际返回 worker.ErrLeaseLost（job lease lost）。需确认是否可视为内部控制返回、按暂停状态和存储稳定及恢复行为验收，当前断言保留。

## 原轮统一运行

使用自有 pgvector 16 容器、tmpfs、随机本机端口；只调用本地假模型/向量/通知服务。不读线上 `.env` 或 `config/`。产品 `make build` 通过。前端 `npm ci`、lint、type-check、build 均通过（本机 Node 20.19.5；package 要求 >=22.12，npm 给出 engine 警告）。

命令：`GOFLAGS=-v make check`，其中数据库测试通过显式 `PCAS_TEST_DATABASE_URL` 指向自有临时库；浏览器两个文件分别 `--retries=0 --reporter=list,json`；W4 使用共享真实后端 runner、`PCAS_IMPORT_CHUNK_SIZE=1`，并设置随机 HTTP 端口。

Go 全量已完成：`make check` 返回 2，失败发生在测试步骤；fmt-check、vet 已过。T4 原轮 32 个主测试：**19 通过、13 失败、0 跳过**。postgres 包耗时 920.944 秒；仓库主测试总计 433 通过、13 失败、3 个既有条件测试跳过（未改）。全部 13 个主测试失败来自 T4，本批以外没有主测试失败。构建另用同一提交的 `make build` 完成并通过。

| 序列 | 原轮结果 | 原轮对应测试 |
|---|---|---|
| I1 | 通过 | ✅ `TestPhase2B4_I1_ZipPreviewCountsMediaGapsAndNeverWrites` |
| I2 | 通过 | ✅ `TestPhase2B4_I2_PartialImportOriginalReachesNewSecretaryConversation` |
| I3 | 失败（含通过用例） | ❌ `TestPhase2B4_I3_PauseStopsStorageAndExtractionButNotOrdinarySource`<br>✅ `TestPhase2B4_I3_ImportStateErrorsHaveFrozenCode` |
| I4 | 失败（含通过用例） | ❌ `TestPhase2B4_I4_BlockedMessageIsReportedAndSkippedWithoutFailingImport`<br>✅ `TestPhase2B4_I4_ReimportAndExtendedExportOnlyAddUnseenMessages` |
| I5 | 通过 | ✅ `TestPhase2B4_I5_HistoricalPlanRemainsCandidateWithoutPresentActions` |
| I6 | 失败 | ❌ `TestPhase2B4_I6_SecretaryOriginalExtractionPrecedes500ImportedMessages` |
| I7 | 通过 | ✅ `TestPhase2B4_I7_UnsupportedAndDecompressedLimitAreAtomic`<br>✅ `TestPhase2B4_I7_FourUploadErrorsAreDistinctAndAtomic` |
| I8 | 失败（含通过用例） | ✅ `TestPhase2B4_I8_RecordCapKeepsNewestAndReportsLeftOut`<br>❌ `TestPhase2B4_I8_CapPrecedesDisjointDuplicateBlockedAndNewCounts` |
| I9 | 通过 | ✅ `TestPhase2B4_I9_CancelAtHalfCommitThenRecoverExpiredLease` |
| I10 | 失败 | ❌ `TestPhase2B4_I10_DeleteArchiveChildrenClaimsBatchAndRecallControl` |
| I11 | 通过 | ✅ `TestPhase2B4_I11_RegeneratedAnswerPreservesHistoricalBranch` |
| I12 | 失败 | ❌ `TestPhase2B4_I12_ConcurrentImportsPauseOnlyOneBatch` |
| I13 | 通过 | ✅ `TestPhase2B4_I13_SpokenDateUsesSydneyAndShanghai` |
| I14 | 通过 | ✅ `TestPhase2B4_I14_NonUserMessagesRemainOriginalWithoutExtractionCalls` |
| L1 | 通过 | ✅ `TestPhase2B4_L1_SecretaryRecordsExactNumbersAndDependencies`<br>✅ `TestPhase2B4_L1_CallsPaginationHasNoDuplicatesAndPreservesNumbers` |
| L2 | 失败 | ❌ `TestPhase2B4_L2_DeputyLegacyAnswerExtractionEachRecordTheirCall` |
| L3 | 失败 | ❌ `TestPhase2B4_L3_FailureAndSuccessfulRequestReplayDoNotAddRows` |
| L4 | 通过 | ✅ `TestPhase2B4_L4_LocalDaysAndSydneyDSTHaveExactSummaries`<br>✅ `TestPhase2B4_L4_SummarySeparatesPurposesAndOmitsEmptyDays` |
| L5 | 失败 | ❌ `TestPhase2B4_L5_CurrentUnicodeExcerptDeletedRefAndEveryColumnPrivacy` |
| L6 | 通过 | ✅ `TestPhase2B4_L6_UsageEndpointsEnforceOwnerAndIsolation` |
| L7 | 失败 | ❌ `TestPhase2B4_L7_UndoClearAndDeleteKeepExactUsageRows` |
| L8 | 失败（含通过用例） | ✅ `TestPhase2B4_L8_CorrectedMemoryDuringDeputyGenerationKeepsReturnedUsage`<br>❌ `TestPhase2B4_L8_UsageIsVisibleBeforeResultTransactionAndSurvivesRollback`<br>✅ `TestPhase2B4_L8_ReturnedButInvalidExtractionStillRecordsUsage`<br>❌ `TestPhase2B4_L8_ReturnedButInvalidSecretaryStillRecordsUsage` |
| L9 | 失败 | ❌ `TestPhase2B4_L9_IdenticalLegacyRequestsMakeTwoCallsAndTwoUsageRows` |

| 浏览器序列 | 实际结果 | 到达的断言 / 限制 |
|---|---|---|
| W1 | 两个用例均失败 | 构建产物上预览请求恰好一次，数量/日期/禁止项说明/实际新存数量的检查已到达；失败在整页禁止词检查读到其他设置内容里的「来源」，确认导入后续断言未到达。待协调者确认检查范围 |
| W2 | 四种状态均失败 | 进度检查之后，整页禁止词检查读到已有导出说明里的「来源」；暂停/继续等后续断言未到达。已询问是否限于导入区域 |
| W3 | 通过 | 构建产物上四种错误各有不同说明、无内部错误码、确认前无导入，390px 无溢出、无 pageerror |
| W4 | 失败（120 秒超时） | 轨迹只有 `POST /v1/connectors/archive → 202`，没有 preview。旧合成文件名是唯一前缀 `.json`，走了通用 JSON 上传；后续 pause/resume/资料库断言未到达 |

构建产物的模拟运行：1 个通过、6 个失败、0 跳过、0 重试、0 flaky；W4：1 个失败、0 跳过、0 重试。没有宣布本批通过。

首次模拟服务启动从仓库根目录运行 Vite，未提供应用入口；已终止自有进程、保存 invalid-fixture 日志并修正启动目录。该启动不计产品结果。随后开发模式的模拟尝试为 7 个失败：W1 出现两次 preview；W3 的轨迹明确出现样式模块 `ERR_NETWORK_CHANGED`，重载后应用未挂载。已改用同一提交的 dist 构建产物，再作一次明确改变服务方式的核对；没有改断言或设置 Playwright 重试。构建模式 W1 次数为 1、W3 通过，开发模式失败日志/JSON仍保留。这两处开发夹具现象不归为产品失败。

## 发现与处理归属

1. W1/W2 的禁止词范围：待协调者确认。保持旧冻结预期和旧断言，不自行决定放宽或改产品。W1 次数在构建产物上为 1，原开发模式次数疑问已由环境对照消除。
2. W3 开发模式在重载时遇到资源 `ERR_NETWORK_CHANGED`，属夹具/环境；构建产物上完整通过。保留开发失败日志，不把改变服务方式后的核对称为重试碰运气。
3. 原轮 L2 的 legacy answer 子用例和 L9 返回 `not found`：T4 夹具没有先初始化配置的代理。已有 `TestDeskServerOwnedAskEditReuse` 明确在 `AnswerDesk` 前调用 Snapshot。已在 `1aa3bd9` 通过真实 `GET /v1/workspace` 补上初始化，不改任何花费、请求次数、关联或原黄金预期；同一产品基线上的单独复核（`-race -count=1 -run '^TestPhase2B4_(L2|L9)_'`）两条均通过，L2 三个子用例均通过。该复核不替换 `c719783` 原轮的两个失败，也不算重新统一全量。
4. 五处 `POST /v1/memory/delete` 返回 404：I4 禁止再导入、I8 混合计数、I10 删除归档、L5 删除引用、L7 删除后花费保留。固定使用协调者第 6 节确认的路径，不自行改成其他接口。预期是已有删除接口能完成相应闭包；建议协调者核对接口契约与集成接线，交 I/接口负责者处理；后续删除效果未验到。
5. I3/I12 暂停后实际 `ProcessAttachment` 返回 `job lease lost`，测试停在内部返回值检查；暂停/继续及按批次隔离的后续断言未到达。已询问协调者是否允许暂停撤销租约作为内部控制返回，以业务状态和存储停止为准。尚不能仅凭内部返回值认定暂停产品行为错误，新增 I19 同样待这个接缝确认。
6. I6：30 秒内没有等到新的秘书原话整理完成；建议 I 核对优先级和领取进度，T4 同时保留夹具/运行时序限制，不宣称已证明某个实现根因。
7. L8 独立花费事务的 rollback/cancel 两个子用例：真实模型已返回一次，结果写入事务已在自有 SQL 门闩等待；另一个连接读到 **0 行** 花费，契约要求此时已独立提交 **1 行**。因此后续回滚/取消检查未到达；建议 L 按第 7 节修正花费提交时点和独立事务。不能用结果事务成功后的记录存在来替代本断言。
8. L8 秘书的格式错误用例：`DeskTurn` 返回 nil error，与测试的 Go 错误返回假设不符；实际非空不合格模型内容、调用一次、花费一行、准确数字和无正文检查均已到达并通过。L3 的模型 HTTP 503 后秘书 HTTP 200，测试停止在 HTTP 错误返回假设，零花费和重放后续断言未到达。已询问协调者是否沿用既有秘书 fallback，不自行改断言或要求产品改变对外错误通道。
9. W4 的合成文件名使入口与预期不符：归属 T4 夹具。交付版本已改为官方 `conversations.json`；没有改预览、暂停、继续和查看原话的预期，本轮原失败仍保留，修正版本未重跑。

构建模式截图、轨迹和原始输出保留在固定验收工作区的 `web/test-results/phase2-b4-original-mocked`、`web/test-results/phase2-b4-original-real`，以及各自临时运行目录；全是合成资料。除已明确修改夹具并复核的 L2/L9 外，失败未作重复试跑；后续测试修改或产品修复需要在新的明确提交上重新统一验收，不能把本轮未到达的断言当作通过。

原轮 Go 原始输出：make-check.log（历史临时日志未保存在仓库：`/tmp/pcas-b4-original-go-gBFB0Y/make-check.log`）。模型花费独立事务及所有失败的原始断言均保留；本轮只报告观察到的现象，不根据 I/L/U4 实现反推或放宽预期。
