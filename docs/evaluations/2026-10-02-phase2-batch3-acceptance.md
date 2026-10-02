# 第 3 批独立验收：测试先行交付

## 状态和基线

执行者 T3；工作区 `/root/PCAS-wt/b3-T3`，分支 `phase2/b3-T3-acceptance`。只按契约编写测试，未阅读 Q1/Q2/K 的实现，未修改产品代码、已有测试或它们的预期。

正式验收 **待协调者通知**；本报告没有将测试已写或静态检查通过记为验收通过。集成分支的被测提交号、统一 `make check` 和浏览器运行结果届时填写。当前未运行 P/S/K/V 验收。测试分支已变基到 `origin/phase2/batch3` 的 `1cec28b`；该提交仍未包含 Q1，因此 P 编译的待实现状态未变化。

初始基线 `66b1a43`；按用户裁定 fetch 了契约提交 `586541120729befc81cb7e15aac7706fd0124516`。在该提交的产品代码上，用自建 `pgvector/pgvector:0.8.2-pg16-bookworm` tmpfs 容器、随机本机端口及 `testStore` 的独立 schema 采集 S6/S10 改动前基准。只使用本机假模型，未读取 `/root/PCAS/.env` 或 `config/`，未调用真实模型、线上库或真实通知。临时采集程序及容器已清理。两组 S6 分别使用新 schema，避免第一次采集自己产生的原话污染第二组输入。

冻结文件 `testdata/phase2/b3-gold.json` 的前三次提交先于首份验收测试提交：初始公历预期；追加 `5865411` 的 P1、要干、最近/这几天裁定；追加 S6/S10 观测。第四次在对应新测试提交之前追加 `0f8e759` 的 range 显示裁定。仅追加，不修改既有条目的值。

## 契约序列到测试的映射

以下全部 **待运行**。测试名统一以 `TestPhase2B3_` 开头，表内列完整后缀。

| 序列 | 文件 | 测试后缀及断言 |
|---|---|---|
| P1 | `internal/memory/phase2_b3_plan_test.go` | `P1_ChengduRecall`：悉尼 2025 年、said、intention/plan、Recall |
| P2 | 同上 | `P2_EveryTimePhrase`：R2 所有说法，预期从冻结公历文件读取 |
| P3 | 同上 | `P3_LocalCalendarBoundaries`：悉尼、上海、UTC；周一/周日/元旦；23/25 小时夏令时日 |
| P4 | 同上 | `P4_NoTimeAndMeaningIndependent`：无时间、不能识别的说法，以及不判断句子意义 |
| P5 | 同上 | `P5_MostRecentMonthAndDay`：3 月与 3 月 12 日取不晚于今天的最近一次 |
| P6 | 同上 | `P6_NaturesAndRecallWords`：逐个性质关键词和回忆关键词，包含「要干」；负例 |
| P7 | 同上 | `P7_AxisAndFirstPhrase`：said/either，以及第一个时间说法 |
| S1 | `internal/postgres/phase2_b3_retrieval_test.go` | `S1_StructuredHitsFirstWithLocalMetadata`：实际 HTTP 请求里的前两条成都计划、老王排序、时区和元数据；另有 `S1_EventPrecisionAndInclusiveRangeDisplay` 检查 day/month/year/range 显示，右端不多一天 |
| S2 | 同上 | `S2_VisibilityAndExcerptTimePriority`：关闭记忆不可见、原话时间排序、依赖及 R2a 收紧 |
| S3 | 同上 | `S3_RelaxTimeOnlyWhenEntityMatches`：错年放宽与 R8 原句；正常年份和只有时间时不加说明 |
| S4 | 同上 | `S4_TimeOnlyNaturePriorityAndSaidAxis`：保留「我上周说了什么计划」，我不构成实体条件；性质、said/either、半开事件边界 |
| S5 | 同上 | `S5_EntityOnlyAcrossYears`：跨年的实体命中先于词法补漏 |
| S6 | `internal/postgres/phase2_b3_compatibility_test.go` | `S6_NoConditionsPreserveBaselineBytesAndOnlyAppendMetadata`：旧字段组逐字相同；新字段组剥去精确的行尾 R12 追加后逐字相同 |
| S7 | `internal/postgres/phase2_b3_retrieval_test.go` | `S7_AllExistingFiltersApplyToStructuredHits`：类别、推测、事项排除、项目及可见性；允许项正例和受限原话负例 |
| S8 | 同上 | `S8_SixtyHitsKeepTopTwentyAndExcerptBudget`：60 条留排序最前 20 条；实际摘录不超过 6 段/2400 字符，完成请求 |
| S9 | 同上 | `S9_SecretaryDeputyManualShareStructuredPrefix`：秘书实际 HTTP 请求、副手实际 HTTP 请求、手动转交 brief 及依赖。手动通道没有模型调用，不能声称抓到它的模型请求 |
| S10 | `internal/postgres/phase2_b3_compatibility_test.go` | `S10_PublicRecallFifteenRequestsByteIdentical`：15 组 HTTP 状态和完整响应逐字节比较，无 JSON/ID 归一化 |
| S11 | `internal/postgres/phase2_b3_retrieval_test.go` | `S11_StructuredDependencyCorrectionMarksTurnOutdated`：真实依赖和更正后的 outdated，保留原回答卡片回执 |
| S12 | 同上 | `S12_AliasLengthSubstringCaseAndDeletedEntity`：单字符负例、成都市命中成都、大小写、删除实体负例 |
| S13 | 同上 | `S13_OnlyCurrentUtterancePlansEntities`：同会话前问大理、后问成都，当前结构命中不带前轮地点 |
| K1 | `internal/postgres/phase2_b3_timeline_test.go` | `K1_SaidChronologyEventAndMentions`：记忆说话日期刻意不同于原话日期，按前者排序；事件起止、精度、人地点 |
| K2 | 同上 | `K2_CompletedAndCancelledTurnItems`：真实秘书动作记录与事项 done/cancelled |
| K3 | 同上 | `K3_CorrectionAndChangeUseCurrentText`：correction/change 两类新版本，changed 及当前文字 |
| K4 | 同上 | `K4_SingleRecallGetsTimelineOnlyWithSaidDate`：有日期的单条回忆有时间轴；非回忆单条、无日期单条没有 |
| K5 | 同上 | `K5_UnknownSaidTimeLastAndEmpty`：无说话时间、也无原话退回日期的条目排最后，at 为空 |
| K6 | 同上 | `K6_AllItemsDetermineStatus`：done/todo、done/cancelled、全 done、全 cancelled |
| K7 | 同上 | `K7_RawOriginalStaysInEvidenceList`：同时引用 M1/S1，原话留在独立依据列表 |
| K8 | 同上 | `K8_UnrelatedItemFromSameOriginalDoesNotChangeStatus`：同资料的另一轮事项，无论 todo/cancelled 都不影响原轮 done |
| V1 | `web/tests/phase2-batch3.spec.ts` | 四状态、说话日期/事件日期、地点人、时间不详、390px 元素和页面不溢出 |
| V2 | 同上 | 点击时间轴按钮，全文自动可见；检查原话 ID 与版本请求 |
| V3 | `web/tests/phase2-batch3-backend.spec.ts` | 独立 schema 数据约定直接写进临时真实后端；假模型 M1；完整实际 HTTP 请求；去年单条时间轴；点击原话 |

## 规则位置和检查

R1–R4：P 序列；R5：S9/S10/S13；R6：S4/S5/S12；R7：S1/S4/S5；R8：S3；R9：S2/S6；R10：S2/S7；R11：S8；R12：S1/S6/V3；R13：S2/S7/S8/S9/S11；R14：K1/K5/K7；R15：K2/K3/K6/K8；R16：K4；R17：V1–V3。

- PostgreSQL 测试的 `go test ./internal/postgres -run '^$'` 编译检查通过；没有运行验收函数。
- P 序列的同类编译检查报 `undefined: PlanQuery`：当前基线上 Q1 尚未合入。没有添加产品 stub、构建标签或 Skip 来绕过；Q1 合入后再检查。
- `npm run lint && npm run type-check && npm run build` 通过，使用本机已有的 Node 22.23.3。初次 `npm ci` 默认 Node 20 有 engine 提示，后续检查切到仓库要求的 Node 22。
- 两份新增浏览器用例额外做直接 TypeScript 检查，通过。现有依赖未包含 `@types/node`，检查使用 `/tmp/pcas-b3-ts-types` 临时安装的 Node 22 类型，不修改依赖文件。
- V1/V2/V3 的 Playwright `--list` 只收集用例，成功；没有运行浏览器验收。V3 内嵌的 Go seed 程序编译通过。
- CI 加入模拟用例，以及调用现有临时后端 runner 的 V3，重试为零。没有改 runner 或已有用例。
- `git diff --check` 通过。已有测试预期修改：无。

## 待办与发现

正式运行前由协调者通知集成提交，在同一提交上统一运行全量与浏览器。失败不跳过、不放宽、不重复碰运气，按归属派回 Q1/Q2/K/U3。

当前验收发现清单为空，因为正式验收尚未执行。range 显示问题由协调者在 `0f8e759` 裁定：显示到实际覆盖的最后一天。已按裁定追加冻结预期，V1 及 S1 检查显示 06-14、不显示排除端点 06-15。Draft PR 的 base 是 `phase2/batch3`；链接在交付消息中提供。
