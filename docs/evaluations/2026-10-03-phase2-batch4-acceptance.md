# 第 4 批独立验收：原轮结果与先存原话补充（2026-10-03）

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

本次 Go 被测提交 `8e904ad2481eba06cf184c4bc2f26058eb7105a3`（41902ca + 变基后的 T4 测试）：`go test -race -count=1 -v -timeout 10m -run '^TestPhase2B4_I(1[5-9]|20)_' ./internal/postgres`。7 个主测试：**4 通过、3 失败、0 跳过**，包耗时 6.453 秒，退出 1，没有重复运行。数据库为自有 tmpfs pgvector 容器，已清理；模型和向量为本地假服务。原始输出：[deferred.log](/tmp/pcas-b4-deferred-41902ca-MHnm08/deferred.log)。I15 now 与 I17 的 fake 返回非空 JSON（items 空数组），实际各有 3 次调用；R4 将 organized 定义为已经处理过抽取的条数，本测试要求它增长，未把空 items 当作放宽计数的理由。交 I 核对处理完成后的计数；报告只陈述观察，不推断实现根因。

W5 使用同一产品基线的前端构建产物，npm ci/lint/type-check/build 均通过；本机 Node 20.19.5，仍有 package 的 >=22.12 engine 警告。初次筛选 `--grep '^W5 '` 未选中用例，属运行夹具，不计测试结果。改正筛选后 `8e904ad` 上的两条 W5 均超时：默认「先存着」已显示，但测试寻找「确认导入/开始导入/确认」按钮，页面实际可访问标签是「导入 6 条」；now 用例寻找「现在整理」等文案，实际是「现在就整理」。轨迹只有 preview，没有确认导入 POST，后续行为未验到。首轮日志/JSON：[mocked.log](/tmp/pcas-b4-w5-41902ca-UwFGBc/mocked.log)、[mocked.json](/tmp/pcas-b4-w5-41902ca-UwFGBc/mocked.json)。

T4 在 `25dadd66a79a0161f13a73d29cfe2621b3b5cc59` 只补 W5 定位器对上述标签的匹配；冻结条目及行为断言不变，W1 等旧用例不改。在该明确修正后的提交执行 `npx playwright test tests/phase2-batch4.spec.ts --grep 'W5 ' --retries=0 --reporter=list,json`，**2 通过、0 失败、0 跳过、0 重试、0 flaky**：默认 later、实际 multipart 字段、存完的人话状态、开始整理 HTTP 与进度增长、可选 now 及实际字段、390px 无溢出和无 pageerror 均通过。复核输出：[mocked.log](/tmp/pcas-b4-w5-41902ca-FzMeDA/mocked.log)、[mocked.json](/tmp/pcas-b4-w5-41902ca-FzMeDA/mocked.json)。截图在当前工作区 `web/test-results/phase2-b4-w5-41902ca`；首次轨迹目录被同名输出覆盖，仅保留首轮日志/JSON及本段已核对的观察。修正后 lint/type-check 另行检查也通过。此次是新增序列专项运行，未在 41902ca 重跑原轮或宣称全量通过。

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

原轮 Go 原始输出：[make-check.log](/tmp/pcas-b4-original-go-gBFB0Y/make-check.log)。模型花费独立事务及所有失败的原始断言均保留；本轮只报告观察到的现象，不根据 I/L/U4 实现反推或放宽预期。
