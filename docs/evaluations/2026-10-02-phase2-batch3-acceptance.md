# 第 3 批独立验收：第 2 批进 main 后的整库结果

## 状态和基线

执行者 T3；工作区 `/root/PCAS-wt/b3-T3`，分支 `phase2/b3-T3-acceptance`。只按契约编写测试，未阅读 Q1/Q2/K 的实现，未修改产品代码或其他任务的测试。第 8 节裁定批准修改三个冻结后缀，其余既有冻结值保持不变。

第 2 批已进 main 并上线后，按协调者指令 fetch、变基到 `origin/phase2/batch3` 的 `635d1bb69f7ae98b817f7f9481ec3a51f01c460a`。浏览器工作流唯一冲突是用例列表，保留已合入的第 2 批用例并加入第 3 批用例；没有改产品或测试预期。统一本地被测提交为 `119f74bb86b18ac10c59bcb32bbb68396b6a0056`。

整库 **`make check` 退出 0，fmt-check、go vet、Go race 全量和 build 全部通过**。Go 顶层用例 **438 通过、0 失败、3 个既有真实模型用例跳过**；第 3 批 **34 通过、0 失败、0 跳过**。此前单列的 `TestExtractionWithoutModelStaysNotConfigured` 和 `TestExtractionConfirmationRequiresCurrentVerbatimCapture` 都已通过，旧失败已消除。

同提交 V1/V2/V3 **全部通过，零重试**；前端 `npm ci`、lint、type-check、build 通过。本轮没有新发现，也没有调整预期或定向重跑。

此前三轮结果保留为历史。README 第 8 节授权的三个后缀、S7 和 V1 预期修正仍有效；本轮冻结文件与第三轮逐字节相同。PR #86 的 CI 在交付分支推送后验证，最终 CI 结果记录在 PR 及本地 manifest。

初始基线 `66b1a43`；按用户裁定 fetch 了契约提交 `586541120729befc81cb7e15aac7706fd0124516`。在该提交的产品代码上，用自建 `pgvector/pgvector:0.8.2-pg16-bookworm` tmpfs 容器、随机本机端口及 `testStore` 的独立 schema 采集 S6/S10 改动前基准。只使用本机假模型，未读取 `/root/PCAS/.env` 或 `config/`，未调用真实模型、线上库或真实通知。临时采集程序及容器已清理。两组 S6 分别使用新 schema，避免第一次采集自己产生的原话污染第二组输入。

冻结文件 `testdata/phase2/b3-gold.json` 的前三次提交先于首份验收测试提交：初始公历预期；追加 `5865411` 的 P1、要干、最近/这几天裁定；追加 S6/S10 观测。第四次在对应新测试提交之前追加 `0f8e759` 的 range 显示裁定。仅追加，不修改既有条目的值。

## 契约序列到测试的映射

下表保留测试映射，最新逐项结果见末节。测试名统一以 `TestPhase2B3_` 开头，表内列完整后缀。

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
| S6 | `internal/postgres/phase2_b3_compatibility_test.go` | `S6_NoConditionsPreserveBaselineBytesAndOnlyAppendMetadata`：旧字段组逐字相同；新字段组剥去精确的行尾 R12 追加后逐字相同；另见新增预算边界测试 |
| S7 | `internal/postgres/phase2_b3_retrieval_test.go` | `S7_AllExistingFiltersApplyToStructuredHits`：类别、推测、事项排除、项目及可见性；允许项正例和受限原话负例 |
| S8 | 同上 | `S8_SixtyHitsKeepTopTwentyAndExcerptBudget`：60 条留排序最前 20 条；实际摘录不超过 6 段/2400 字符，完成请求；另见新增长元数据测试 |
| S6/S8 补充 | `internal/postgres/phase2_b3_metadata_budget_test.go` | `S6_MetadataDoesNotConsumeDeputyOrManualByteBudget`：副手/手动、旧字段/新字段四组；剥去精确追加后完整交接内容与旧基准逐字相同，依赖相同，追加后可超过 30000 字节。`S8_LargeMetadataStillKeepsTheSameTopTwenty`：60 条带完整长元数据的命中仍保留前 20 条，后 40 条和依赖排除，正常完成 |
| S9 | 同上 | `S9_SecretaryDeputyManualShareStructuredPrefix`：秘书实际 HTTP 请求、副手实际 HTTP 请求、手动转交 brief 及依赖。手动通道没有模型调用，不能声称抓到它的模型请求 |
| S10 | `internal/postgres/phase2_b3_compatibility_test.go` | `S10_PublicRecallFifteenRequestsByteIdentical`：15 组 HTTP 状态和完整响应逐字节比较，无 JSON/ID 归一化 |
| S11 | `internal/postgres/phase2_b3_retrieval_test.go` | `S11_StructuredDependencyCorrectionMarksTurnOutdated`：真实依赖和更正后的 outdated，保留原回答卡片回执 |
| S12 | 同上 | `S12_AliasLengthSubstringCaseAndDeletedEntity`：单字符负例、成都市命中成都、大小写、删除实体负例 |
| S13 | 同上 | `S13_OnlyCurrentUtterancePlansEntities`：同会话前问大理、后问成都，当前结构命中不带前轮地点 |
| K1 | `internal/postgres/phase2_b3_timeline_test.go` | `K1_SaidChronologyEventAndMentions`：记忆说话日期刻意不同于原话日期，按前者排序；事件起止、精度、人地点 |
| K2 | 同上 | `K2_CompletedAndCancelledTurnItems`：真实秘书动作记录与事项 done/cancelled |
| K3 | 同上 | `K3_CorrectionAndChangeUseCurrentText`：correction/change 两类新版本，changed 及当前文字 |
| K4 | 同上 | `K4_SingleRecallGetsTimelineOnlyWithSaidDate`：有日期的单条回忆有时间轴；非回忆单条、无日期单条没有 |
| K5 | 同上 | `K5_UnknownSaidTimeLastAndEmpty`：按 5beb42c 将夹具收窄到无日期导入文件；记忆和资料表达时间均为空，导入记录时间不能顶替说话时间，条目排最后且 at 为空 |
| K6 | 同上 | `K6_AllItemsDetermineStatus`：done/todo、done/cancelled、全 done、全 cancelled |
| K7 | 同上 | `K7_RawOriginalStaysInEvidenceList`：同时引用 M1/S1，原话留在独立依据列表 |
| K8 | 同上 | `K8_UnrelatedItemFromSameOriginalDoesNotChangeStatus`：同资料的另一轮事项，无论 todo/cancelled 都不影响原轮 done |
| K9 | `internal/postgres/phase2_b3_timeline_receipts_test.go` | `K9_OldCreationReceiptSurvivesPurgedActionDetails`：真实创建回执、40 天前的原轮，正常命令清空动作明细后事项才做完；回执仍保留，时间轴 done |
| K10 | 同上 | `K10_UndoneCreationLeavesNoAssociatedItems`：真实当场撤销、事项确实消失、原回执保留且已撤销；独立背景记忆无变化 open，纠正后 changed |
| K11 | `internal/postgres/phase2_b3_timeline_test.go` | `K11_LegacySecretaryMemoryUsesOriginalRecordedTime`：真实秘书原话，记忆和原话表达时间均为空；按原话记录时间排序，覆盖悉尼/上海 |
| V1 | `web/tests/phase2-batch3.spec.ts` | 四状态、说话日期/事件日期、地点人、时间不详、390px 元素和页面不溢出 |
| V2 | 同上 | 点击时间轴按钮，全文自动可见；检查原话 ID 与版本请求 |
| V3 | `web/tests/phase2-batch3-backend.spec.ts` | 独立 schema 数据约定直接写进临时真实后端；假模型 M1；完整实际 HTTP 请求；去年单条时间轴；点击原话 |

## 规则位置和检查

R1–R4：P 序列；R5：S9/S10/S13；R6：S4/S5/S12；R7：S1/S4/S5；R8：S3；R9：S2/S6；R10：S2/S7；R11：S8；R12：S1/S6/V3；R12a：S6/S8 的 metadata_budget 补充；R13：S2/S7/S8/S9/S11；R14：K1/K5/K7/K11；R15：K2/K3/K6/K8/K9/K10；R16：K4；R17：V1–V3。

- PostgreSQL 测试的 `go test ./internal/postgres -run '^$'` 编译检查通过；没有运行验收函数。
- 首轮 P 序列的同类编译检查报 `undefined: PlanQuery`：当时基线上 Q1 尚未合入。没有添加产品 stub、构建标签或 Skip 来绕过；Q1 合入后再检查。
- `npm run lint && npm run type-check && npm run build` 通过，使用本机已有的 Node 22.23.3。初次 `npm ci` 默认 Node 20 有 engine 提示，后续检查切到仓库要求的 Node 22。
- 两份新增浏览器用例额外做直接 TypeScript 检查，通过。现有依赖未包含 `@types/node`，检查使用 `/tmp/pcas-b3-ts-types` 临时安装的 Node 22 类型，不修改依赖文件。
- V1/V2/V3 的 Playwright `--list` 只收集用例，成功；没有运行浏览器验收。V3 内嵌的 Go seed 程序编译通过。
- CI 加入模拟用例，以及调用现有临时后端 runner 的 V3，重试为零。没有改 runner 或已有用例。
- `git diff --check` 通过。已有测试预期修改：无。

## 待办与发现

三项裁定均已按 README 第 8 节执行；第 2 批进 main 后再次统一确认整库。实际结果见末节；失败不跳过，不作未经批准的预期调整，不重复运行碰运气。

测试先行交付时发现清单为空；历轮结果分别列在后文。range 显示问题由协调者在 `0f8e759` 裁定：显示到实际覆盖的最后一天。已按裁定追加冻结预期，V1 及 S1 检查显示 06-14、不显示排除端点 06-15。[Draft PR #86](https://github.com/soaringjerry/PCAS/pull/86)，base 为 `phase2/batch3`。

## b9d26f0 / 5beb42c 的测试补充（待正式运行）

按协调者两次明确指令，先分别提交冻结预期，再写 K9/K10/K11 并调整 K5 夹具。冻结文件已有值不改；K5 的 `at` 为空、排最后的预期不改，只将原无来源夹具改为没有日期的导入文件。没有修改其他旧测试预期，也没有改产品代码。

K9 通过真实秘书创建任务并保留回执，把该轮、原话、任务及创建动作日期设为相对今天的 40 天前；夹具和可见性设置在老化动作之前完成。随后用正常工作台命令触发既有 30 天明细保留策略，先断言原动作确有明细、再断言明细为 `[]` 且已过期，并经历史接口确认回执保留，然后才把事项做完并问回忆，断言 `done`。

K10 当场真实撤销秘书创建事项，断言原事项数据库行已消失、原轮回执仍存在并已撤销。独立背景事实从保留的原话构造，避免把本条时间轴规则和第 1/2 批的记忆撤销混在一起；分别检查 `open` 和纠正后的 `changed`。纠正版本保留同一原话的证据关系，防止「丢掉关联资料」掩盖过期事项关联错误。

K11 用真实秘书入口产生原话，再直接设置其表达时间为空、记录时间为相对今天 45 天前；旧记忆自身表达时间为空，与有自己说话日期的 40 天前记忆一起引用。断言原话记录时间成为 `at`，正确排在后者前面，覆盖悉尼和上海。

本轮只做 PostgreSQL 测试编译（`go test ./internal/postgres -run '^$'`）、gofmt、`git diff --check`、冻结预期只增不改及 31 个 P/S/K 编号覆盖审计；没有运行任何验收函数，继续等待协调者通知。

## 2e2b8f9 / R12a 的测试补充（待正式运行）

S6、S8 已有预期保持不变。本轮先在第 3 批之前的 `586541120729befc81cb7e15aac7706fd0124516` 上补采 S6 的交接字节边界基准，仍使用自建 tmpfs pgvector 容器、随机本机端口、独立 schema 和本机假模型。只采集旧字段组；副手与手动的交接内容逐字相同，都是 29830 字节，保留两条记忆和一段原话。冻结模板只用占位符无损表示 29200 个固定 notes 字节，展开后另用观测 SHA-256 校验；被测内容不做 ID、空白或其他字节归一化。采集工作区、临时程序和容器已清理。

在 `metadataBudgetRuling` 中先追加并提交冻结预期，再写补充测试。S6 记忆带 R12 时间、地点、人后，应为 32352 字节；大于原 30000 字节上限是允许的。四组使用各自新 schema；模型组从真实 HTTP 请求中检查完整交接内容，手动组检查 Brief 且不得调用模型。仅剥去两条记忆行的精确 R12 后缀后，完整内容须与旧基准逐字相同，包括原话行和顺序；ContextVersions 和实际 run_dependencies 保留原三条依赖。

S8 补充用 60 条独立结构化命中，按说话时间构造前 20 条的顺序。每条增加时间、事件月份、长地点和人名；实际交给模型的记忆行累计超过 30000 字节，仍须完整保留前 20 条及其元数据，排除后 40 条，记录 20 条记忆依赖并正常回复。原有 S8 的原话 6 段/2400 字符检查保留。

本轮 `go test ./internal/postgres ./internal/memory -run '^$'` 编译通过，未运行验收函数；冻结文件只增不改审计、gofmt 和 `git diff --check` 通过。继续等待协调者通知正式验收。

## cd5815e 首轮预跑（历史记录，裁定及复验见下一节）

本轮运行的产品基线和测试提交均已记录：集成 `cd5815e`，被测 `d98a527cf8a3a5dd0c4d1f41039b21ca93397e00`。不读 Q1/Q2/K 实现；只用契约、数据约定、schema、已有测试以及本轮实际输出分类。造数修正发生在预跑结束之后，下表仍保留修正前的实际失败结果。

全量命令是 `PCAS_TEST_DATABASE_URL=<自建临时库> GOFLAGS='-v' make check`，执行 Go race 测试，PostgreSQL 包耗时 374.869 秒。fmt-check、go vet 通过；test 失败导致 make 退出 2，build 目标未执行；随后在同一产品提交上单独执行 `go build -trimpath`，通过。全部 Go 顶层用例计 371 通过、23 失败、3 跳过；23 个失败包括 T3 的 21 个以及第 2 批的 2 个。3 个跳过均是既有显式真实模型用例（`TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`），本轮未接入真实模型；T3 没有跳过。

前端使用 Node 22.23.3，`npm ci && npm run lint && npm run type-check && npm run build` 通过。浏览器用现有 `real-backend.sh` 的临时后端、独立 tmpfs 数据库、本机假模型运行 `tests/phase2-batch3.spec.ts` 与 `tests/phase2-batch3-backend.spec.ts`，`--retries=0 --reporter=list,json`；三条各运行一次，无重复。

### T3 各序列实际结果

测试后缀均补上统一前缀 `TestPhase2B3_`，文件位置在前面的映射表。

| 序列 | 测试后缀 | 本轮实际结果 |
|---|---|---|
| P1 | `P1_ChengduRecall` | 通过 |
| P2 | `P2_EveryTimePhrase` | 通过 |
| P3 | `P3_LocalCalendarBoundaries` | 通过 |
| P4 | `P4_NoTimeAndMeaningIndependent` | 通过 |
| P5 | `P5_MostRecentMonthAndDay` | 通过 |
| P6 | `P6_NaturesAndRecallWords` | 通过 |
| P7 | `P7_AxisAndFirstPhrase` | 通过 |
| S6 | `S6_NoConditionsPreserveBaselineBytesAndOnlyAppendMetadata` | 失败：新字段组缺少 R12 后缀（M2 已知缺口）；旧字段组通过 |
| S10 | `S10_PublicRecallFifteenRequestsByteIdentical` | 通过 |
| S6 | `S6_MetadataDoesNotConsumeDeputyOrManualByteBudget` | 失败：新字段组缺少 R12 后缀（M2 已知缺口）；旧字段组通过 |
| S8 | `S8_LargeMetadataStillKeepsTheSameTopTwenty` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S1 | `S1_StructuredHitsFirstWithLocalMetadata` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S2 | `S2_VisibilityAndExcerptTimePriority` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S3 | `S3_RelaxTimeOnlyWhenEntityMatches` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S4 | `S4_TimeOnlyNaturePriorityAndSaidAxis` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S5 | `S5_EntityOnlyAcrossYears` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S7 | `S7_AllExistingFiltersApplyToStructuredHits` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S8 | `S8_SixtyHitsKeepTopTwentyAndExcerptBudget` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S9 | `S9_SecretaryDeputyManualShareStructuredPrefix` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S11 | `S11_StructuredDependencyCorrectionMarksTurnOutdated` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S12 | `S12_AliasLengthSubstringCaseAndDeletedEntity` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S13 | `S13_OnlyCurrentUtterancePlansEntities` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| S1 | `S1_EventPrecisionAndInclusiveRangeDisplay` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| K9 | `K9_OldCreationReceiptSurvivesPurgedActionDetails` | 通过 |
| K10 | `K10_UndoneCreationLeavesNoAssociatedItems` | 通过 |
| K1 | `K1_SaidChronologyEventAndMentions` | 失败：公共夹具 setMemoryVisibility 报 invalid input，产品断言未到达 |
| K2 | `K2_CompletedAndCancelledTurnItems` | 通过 |
| K3 | `K3_CorrectionAndChangeUseCurrentText` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| K4 | `K4_SingleRecallGetsTimelineOnlyWithSaidDate` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |
| K5 | `K5_UnknownSaidTimeLastAndEmpty` | 失败：公共夹具 setMemoryVisibility 报 invalid input，产品断言未到达 |
| K6 | `K6_AllItemsDetermineStatus` | 通过 |
| K7 | `K7_RawOriginalStaysInEvidenceList` | 失败：公共夹具 setMemoryVisibility 报 invalid input，产品断言未到达 |
| K8 | `K8_UnrelatedItemFromSameOriginalDoesNotChangeStatus` | 通过 |
| K11 | `K11_LegacySecretaryMemoryUsesOriginalRecordedTime` | 失败：公共夹具 Commit 报 invalid input，产品断言未到达 |

| V1 | `V1 四种状态、说话日期和事件日期、人地点与无日期，390px不溢出` | 失败：日期为“2月3日2025年”，匹配器只接受年在前；其余断言未完整执行 |
| V2 | `V2 点击时间轴条目直接打开当时的原话` | 通过：全文自动可见、原话 ID 与版本正确、无页面异常 |
| V3 | `V3 真实结构化记忆→秘书实际请求→去年的时间轴→当时原话` | 失败：seeder 的 SQL 参数类型冲突；产品断言尚未执行 |

### 首轮观察与夹具修正（造数错误按第 7 节不算产品发现）

| 编号 | 现象和契约预期 | 归属及处理 |
|---|---|---|
| B3-M2-01 | S6 秘书和副手/手动的新字段组没有 R12 行尾内容。旧字段组逐字通过；有新字段应追加后缀，字节边界组应从 29830 增至 32352，但实际仍为 29830。条目和依赖检查没有失败 | 第 2 批 M2 已知缺口，本轮仍如实记失败。待 M2 合入后由协调者通知最终一轮 |
| B3-T3-01 | 多数 S/K 通过公共 `b3Claim` 的 Commit 准备普通记忆时报 invalid input；K1/K5/K7 在后续 setMemoryVisibility 时报同样错误，均未到达产品断言。T3 契约要求独立按数据约定造数，不依赖第 2 批抽取 | T3 夹具修正：普通记忆、版本、项目、证据、活动和结构化字段全部在同一事务直接插入；仍通过真实可见性命令、秘书/副手、Correct、Undo 验收。已有冻结预期不改 |
| B3-T3-02 | V3 在 sources 的 INSERT 中同一个 `$2` 同时用于 uuid 与 text，SQLSTATE 42P08。读 schema 还发现 evidence.locator 无默认值但 seeder 未给值 | T3 夹具修正：ID 和外部编号使用独立参数，补 `locator='{}'`；完整 seeder 已在新 schema 中单独执行成功，未重跑 V3 |
| B3-T3-03 | V1 实际显示“2月3日2025年”，日期值与冻结值 2025-02-03 一致，但匹配器强制年在前。R17 未规定年月日的排版顺序 | 等协调者裁定是否允许修匹配器；当前未修改该断言，也未修改日期预期。V3 的说话日期匹配也有同样的年在前假设，下一轮前需一并确认 |
| B2-OBS-01 | 既有 `TestExtractionWithoutModelStaysNotConfigured` 在 extraction_failure_test.go:72 报 provider_not_configured | 第 2 批 E1/T2，由协调者确认接口行为和测试预期；T3 未修改 |
| B2-OBS-02 | 既有 `TestExtractionConfirmationRequiresCurrentVerbatimCapture` 的 unresolved_subject、unresolved_predicate、paraphrased_assertion 三个子用例，实际 confirmation=adopted，原断言 candidate | 第 2 批 E1/T2，由协调者确认最新契约及既有测试；T3 未修改 |

修正后只运行临时 `TestB3FixturePreflightOnly`：上海/悉尼的完整公共数据集，以及 V3 seeder 在完整新 schema 上造数成功，没有模型调用，也没有运行任何验收函数。临时检查文件已移出仓库；正式测试文件做 Go 编译、单独 TypeScript 检查和 diff 检查。冻结 JSON 与被测提交逐项、逐值完全相同。

### 运行边界和未覆盖部分

S6 新字段组是已知 M2 未完成的实际失败；S1 的排序/元数据、其他失败 S/K 的检索与时间轴行为由于造数失败仍未验证，不能算已知缺口通过或产品失败。V1 在第一个日期匹配处停止，后续四状态、事件区间和 390px 检查未完整执行；V3 在 seeder 阶段停止，真实秘书请求、时间轴和点开原话未验证。等待最终运行指令后，在新的同一提交重新统一全量和浏览器。

全量和造数预检均只使用 T3 自建 `pgvector/pgvector:0.8.2-pg16-bookworm` tmpfs 容器、随机 loopback 端口和每测试独立 schema；浏览器 runner 管理自己的容器和 PID。未读取线上配置、未使用真实模型、线上库或真实通知。所有本轮临时服务和数据库已清理。原始日志、浏览器 JSON/trace、manifest 和造数预检记录保存在本地 `/tmp/pcas-b3-t3-round1-d98a527/`。

## f6613bc 第二轮重跑（历史记录，三项裁定已在下一节处理）

### 被测提交和命令

集成 `f6613bc4a6c67e64f5611c10636eff2425d146ce`；统一被测 `348651e71315c37f2902262a0aef2cd896df58d2`。本轮只按 README 第 7 节授权改变 V1/V3 的说话日期匹配：检查时间元素的 `dateTime` 对应冻结时刻，并检查可见月日与年份，不规定排列顺序。冻结日期值未改，`b3-gold.json` 与统一被测提交逐字节相同。

- `PCAS_TEST_DATABASE_URL=<自建临时库> GOFLAGS='-v' make check`：退出 2，PostgreSQL 包 322.814 秒。fmt-check、go vet 通过，Go race 测试失败阻止 make 的 build 目标；随后相同产品代码单独 `go build -trimpath` 通过。
- Node 22.23.3：`npm ci && npm run lint && npm run type-check && npm run build` 通过；两份浏览器测试额外直接 TypeScript 检查通过。
- 现有 `real-backend.sh` 运行 `tests/phase2-batch3.spec.ts` 和 `tests/phase2-batch3-backend.spec.ts`，`--retries=0 --reporter=list,json`。V1/V2/V3 各运行一次。

统一计数是 Go 382 通过 / 12 失败 / 3 跳过，其中 T3 24 通过 / 10 失败 / 0 跳过。3 个跳过仍是 `TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`，未接入真实模型。

### 全量与补跑逐项结果

测试后缀均补上 `TestPhase2B3_`。补跑单列在最后一列，不将不同提交结果合称统一全量通过。

| 序列 | 测试后缀 | 348651e 统一全量 | ffcd259 夹具修正后补跑 |
|---|---|---|---|
| P1 | `P1_ChengduRecall` | 通过 | — |
| P2 | `P2_EveryTimePhrase` | 通过 | — |
| P3 | `P3_LocalCalendarBoundaries` | 通过 | — |
| P4 | `P4_NoTimeAndMeaningIndependent` | 通过 | — |
| P5 | `P5_MostRecentMonthAndDay` | 通过 | — |
| P6 | `P6_NaturesAndRecallWords` | 通过 | — |
| P7 | `P7_AxisAndFirstPhrase` | 通过 | — |
| S6 | `S6_NoConditionsPreserveBaselineBytesAndOnlyAppendMetadata` | 失败：旧字段组通过；新字段已追加，精确后缀顺序待裁定 | — |
| S10 | `S10_PublicRecallFifteenRequestsByteIdentical` | 通过 | — |
| S6 | `S6_MetadataDoesNotConsumeDeputyOrManualByteBudget` | 失败：旧字段副手/手动通过；新字段 32352 字节、条目和依赖检查通过，精确后缀及剥离后逐字比较失败 | — |
| S8 | `S8_LargeMetadataStillKeepsTheSameTopTwenty` | 失败：前 20 条及依赖正确、追加后超过 30000 字节；后缀顺序待裁定 | — |
| S1 | `S1_StructuredHitsFirstWithLocalMetadata` | 通过 | — |
| S2 | `S2_VisibilityAndExcerptTimePriority` | 通过 | — |
| S3 | `S3_RelaxTimeOnlyWhenEntityMatches` | 通过 | — |
| S4 | `S4_TimeOnlyNaturePriorityAndSaidAxis` | 通过 | — |
| S5 | `S5_EntityOnlyAcrossYears` | 通过 | — |
| S7 | `S7_AllExistingFiltersApplyToStructuredHits` | 失败：受限记忆均未交付；类别/推测/项目的原话负例断言待修正裁定；排除/可见性子用例通过 | — |
| S8 | `S8_SixtyHitsKeepTopTwentyAndExcerptBudget` | 通过 | — |
| S9 | `S9_SecretaryDeputyManualShareStructuredPrefix` | 通过 | — |
| S11 | `S11_StructuredDependencyCorrectionMarksTurnOutdated` | 失败：造数时 setMemoryVisibility 报 invalid input，未到达产品断言 | 通过，预期未改 |
| S12 | `S12_AliasLengthSubstringCaseAndDeletedEntity` | 通过 | — |
| S13 | `S13_OnlyCurrentUtterancePlansEntities` | 通过 | — |
| S1 | `S1_EventPrecisionAndInclusiveRangeDisplay` | 通过 | — |
| K9 | `K9_OldCreationReceiptSurvivesPurgedActionDetails` | 通过 | — |
| K10 | `K10_UndoneCreationLeavesNoAssociatedItems` | 通过 | — |
| K1 | `K1_SaidChronologyEventAndMentions` | 失败：造数时 setMemoryVisibility 报 invalid input，未到达产品断言 | 通过，预期未改 |
| K2 | `K2_CompletedAndCancelledTurnItems` | 通过 | — |
| K3 | `K3_CorrectionAndChangeUseCurrentText` | 失败：造数时 setMemoryVisibility 报 invalid input，未到达产品断言 | 通过，预期未改 |
| K4 | `K4_SingleRecallGetsTimelineOnlyWithSaidDate` | 失败：造数时 setMemoryVisibility 报 invalid input，未到达产品断言 | 通过，预期未改 |
| K5 | `K5_UnknownSaidTimeLastAndEmpty` | 失败：造数时 setMemoryVisibility 报 invalid input，未到达产品断言 | 通过，预期未改 |
| K6 | `K6_AllItemsDetermineStatus` | 通过 | — |
| K7 | `K7_RawOriginalStaysInEvidenceList` | 失败：造数时 setMemoryVisibility 报 invalid input，未到达产品断言 | 通过，预期未改 |
| K8 | `K8_UnrelatedItemFromSameOriginalDoesNotChangeStatus` | 通过 | — |
| K11 | `K11_LegacySecretaryMemoryUsesOriginalRecordedTime` | 通过 | — |

| 浏览器 | 348651e 结果 | 已验证内容及限制 |
|---|---|---|
| V1 `V1 四种状态、说话日期和事件日期、人地点与无日期，390px不溢出` | 失败 | 说话时间属性和可见月日/年份通过；第二行的事件日期匹配失败。剩余状态、无日期和 390px 等断言未完整执行 |
| V2 `V2 点击时间轴条目直接打开当时的原话` | 通过 | 全文自动可见、原话 ID/版本请求正确、无页面异常 |
| V3 `V3 真实结构化记忆→秘书实际请求→去年的时间轴→当时原话` | 通过 | SQL 造数、实际秘书模型 HTTP 请求里的日期与人地点、去年单条时间轴、时间属性和可见日期、390px、点开原话全部通过 |

### 补跑的原因和边界

统一全量中的 S11、K1、K3、K4、K5、K7，在创建带 `model` 可见性配置的记忆之前没有先配置假模型，导致 `setMemoryVisibility` 返回 invalid input。T3 将假模型配置移至造数之前，回应内容在取得记忆引用后设置；没有改变产品、冻结预期或行为断言。

修正提交 `ffcd259c65fe42b0e01e2335a10223c21231316d`；仅补跑 `go test -race -count=1 ./internal/postgres -run '^TestPhase2B3_(S11|K1|K3|K4|K5|K7)_' -v`，六个顶层测试全部通过，PostgreSQL 包 3.949 秒。依 README 第 7 节，这些造数失败不计产品发现。K5 的无日期导入文件仍排最后且 `at` 为空；K11 的旧秘书记忆使用原话记录时间、悉尼/上海两组通过。

S8 原长元数据断言把文字/排序和精确后缀合并报错。`b9210feb676411330522f91a19c0f791dd75c973` 仅拆开诊断，不改变通过条件；单独补跑这一条，仍失败。日志确认 20 行均包含对应的冻结排序文本，失败均为 R12 精确后缀，人名实际先于地点；未以重复运行取得通过。

### 待协调者裁定的三项

| 编号 | 实际输出与冻结断言 | 归属与处理 |
|---|---|---|
| B3-R12-02 | S6 秘书实际后缀是 `/ 说于 2025-07-03 / 事件 2025-11 / 匿名甲 / 成都`，冻结后缀是相同日期后 `/ 成都 / 匿名甲`。S8 长元数据同样人先、地点后。S6 预算组完整内容为预期的 32352 字节，条目和依赖正确，但精确后缀匹配、剥离后的字节比较失败 | R12 示例是否规定地点在人之前，已问协调者。若规定则 Q2 修；若不规定则 T3 按裁定调整后缀断言。当前预期保留，精确剥离后的逐字相同比较尚未通过 |
| B3-S7-02 | S7 的类别/推测/项目三组均隐藏受限记忆，但保留原话及其依赖。第 1 批 R2a 明确这三种设置不影响原话，原话跨项目共享；T3 当初额外断言原话也应消失，范围过严。事项排除、手动关闭可见性两组通过 | T3 断言修正待协调者批准：保留记忆不可见断言，只将上述三组原话及依赖改为允许并要求存在。未修改其他任务测试或自行改变冻结预期 |
| B3-V1-02 | 第二行可见文字为 `4月5日2025年和老王见面已完成4月12日 周六老王成都`；事件冻结值是 `2025-04-12`。行内已有说话年份，但事件自身只显示月日；当前事件匹配器要求年份在事件月日之前 | R17 是否允许同年事件共用行内年份，已问协调者。若允许则 T3 依裁定拆开检查；若必须事件独立显示年份则 U3 修。V1 后续断言尚未完整执行 |

首轮缺少 M2 元数据的问题已消除：S1 两时区及事件精度/区间右端通过，V3 真实链路通过，S6 新字段实际已出现在模型内容中。上述 R12 顺序问题单独等待裁定，不沿用首轮“新字段不存在”的结论。

### 第 2 批旧抽取测试单列

- `TestExtractionWithoutModelStaysNotConfigured`：`extraction_failure_test.go:72`，`provider_not_configured`。
- `TestExtractionConfirmationRequiresCurrentVerbatimCapture`：`unresolved_subject`、`unresolved_predicate`、`paraphrased_assertion` 三个子用例实际 `adopted`，旧断言 `candidate`（`extraction_lifecycle_test.go:64`）。

与首轮相同，README 第 7 节已交由 T2 修改；T3 不修改，在第 2 批进 main 之前单列，未纳入第 3 批产品发现。

### 运行边界与交付

只使用 T3 自建 tmpfs PostgreSQL16/pgvector0.8.2 容器、随机 loopback 端口、独立 schema 和本机假模型；浏览器 runner 管理自己的临时数据库与服务。未读取线上配置，未使用真实模型、线上数据库或真实通知。没有更改冻结 JSON，未读 Q1/Q2/K 实现，也未修改产品代码或其他执行者的测试。

原始 `make-check.log`、前端和 build 日志、浏览器 JSON/trace 与服务日志、六条夹具补跑日志、S8 诊断日志以及带提交号/计数的 manifest 保存在 `/tmp/pcas-b3-t3-round2-348651e/`。临时数据库与服务已清理。测试及报告交付在 [Draft PR #86](https://github.com/soaringjerry/PCAS/pull/86)，base `phase2/batch3`。三个裁定到达后继续按通知修正、复验；本报告不宣称最终验收通过。

## f5b2a42 第三轮统一验收（2026-10-03，历史记录）

### 被测提交与统一运行

集成提交 `f5b2a42b361c7eb67852538c028a277816c2664e`；先提交冻结文件修正 `f140f53`，再提交测试调整 `a174b9270faa3f69a5df30f767605124b13403b6`。全部正式测试只运行这个被测提交；编译预检不运行验收函数，运行期间工作树和提交保持不变。

- `PCAS_TEST_DATABASE_URL=<自建临时库> GOFLAGS='-v' make check`：Go race 全量一次，PostgreSQL 包 307.403 秒；fmt-check、go vet 通过，退出 2。T3 **34 通过 / 0 失败 / 0 跳过**；整库 **392 通过 / 2 失败 / 3 跳过**。
- 同提交单独 `go build -trimpath` 通过。make 因单列的两个既有失败停止在 test，未运行自身 build 目标。
- Node 22.23.3：`npm ci && npm run lint && npm run type-check && npm run build` 通过；两份新增浏览器测试的直接 TypeScript 检查通过。
- 现有 `real-backend.sh` 的临时真实后端运行 `tests/phase2-batch3.spec.ts`、`tests/phase2-batch3-backend.spec.ts`，`--retries=0 --reporter=list,json`：**3 通过 / 0 失败 / 0 跳过 / 0 重试**。

### 第 8 节获准的预期修正

只修改 T3 自己的测试和冻结文件，不修改其他执行者的测试或产品代码。

| 裁定 | 冻结值及断言的改动 | 本轮验证 |
|---|---|---|
| R12 人在前、地点在后，各自按名字排序 | 三个既有叶值 `preBatch3Baseline.S6.metadataSuffix`、`metadataBudgetRuling.S6.metadataSuffix`、`metadataBudgetRuling.S8.metadataSuffixTemplate` 改为人先、地点后；旧值、批准的新值和批准提交一并记入追加的 `secondAcceptanceRuling`，便于审计。其他既有冻结值均未改 | S6 精确剥离后逐字比较、S6 副手/手动四组字节预算、S8 长元数据全部通过；S1 追加乱序的两个人、两个地点，实际模型行尾精确为 `/ 刘乙 / 赵丙 / 成都 / 西安` |
| S7 类别/推测/项目只限制记忆 | 这三组保留受限记忆文本及记忆引用不存在的断言，移除原话及原话引用必须消失的断言；不增加原话必须出现的要求。手动关闭可见性、事项排除两组仍要求原话及其引用也不出现 | 五个子用例全部通过，允许的记忆正例也通过 |
| V1 事件年份按说话年份决定是否要求 | 事件标签自身检查月、日，有不同年份才要求显示事件年份；无说话时间时按用户时区当前年份比较。精度为 year 时年份本身仍是事件内容。原说话时间 `dateTime` 和可见月日/年份断言保留。区间必须显示 12 日至实际最后一天 14 日，不出现排除端点 15 日，允许同月共享一个月份 | 原四条、四状态、时间不详、390px 全部通过；冻结并追加的跨年单条（2025 年说、2026-01-12 的事件）要求事件标签显示 2026 年，也通过 |

冻结审计证实：上述三个后缀是仅有的既有叶值修改；只追加 `secondAcceptanceRuling`，其中包括批准记录、多实体顺序、跨年事件夹具。S6 旧基准的完整文本和字节数、S10 的十五个完整响应字节、全部 P/K/range 日期预期均保持不变。未按实际输出生成新日期预期。

### 各序列的同提交结果

测试后缀均补上统一前缀 `TestPhase2B3_`；文件映射仍在前文。所有下列结果都来自 `a174b92` 的一次统一全量。

| 序列 | 测试后缀 | 结果 |
|---|---|---|
| P1 | `P1_ChengduRecall` | 通过 |
| P2 | `P2_EveryTimePhrase` | 通过 |
| P3 | `P3_LocalCalendarBoundaries` | 通过 |
| P4 | `P4_NoTimeAndMeaningIndependent` | 通过 |
| P5 | `P5_MostRecentMonthAndDay` | 通过 |
| P6 | `P6_NaturesAndRecallWords` | 通过 |
| P7 | `P7_AxisAndFirstPhrase` | 通过 |
| S6 | `S6_NoConditionsPreserveBaselineBytesAndOnlyAppendMetadata` | 通过 |
| S10 | `S10_PublicRecallFifteenRequestsByteIdentical` | 通过 |
| S6 | `S6_MetadataDoesNotConsumeDeputyOrManualByteBudget` | 通过 |
| S8 | `S8_LargeMetadataStillKeepsTheSameTopTwenty` | 通过 |
| S1 | `S1_StructuredHitsFirstWithLocalMetadata` | 通过 |
| S2 | `S2_VisibilityAndExcerptTimePriority` | 通过 |
| S3 | `S3_RelaxTimeOnlyWhenEntityMatches` | 通过 |
| S4 | `S4_TimeOnlyNaturePriorityAndSaidAxis` | 通过 |
| S5 | `S5_EntityOnlyAcrossYears` | 通过 |
| S7 | `S7_AllExistingFiltersApplyToStructuredHits` | 通过 |
| S8 | `S8_SixtyHitsKeepTopTwentyAndExcerptBudget` | 通过 |
| S9 | `S9_SecretaryDeputyManualShareStructuredPrefix` | 通过 |
| S11 | `S11_StructuredDependencyCorrectionMarksTurnOutdated` | 通过 |
| S12 | `S12_AliasLengthSubstringCaseAndDeletedEntity` | 通过 |
| S13 | `S13_OnlyCurrentUtterancePlansEntities` | 通过 |
| S1 | `S1_EventPrecisionAndInclusiveRangeDisplay` | 通过 |
| K9 | `K9_OldCreationReceiptSurvivesPurgedActionDetails` | 通过 |
| K10 | `K10_UndoneCreationLeavesNoAssociatedItems` | 通过 |
| K1 | `K1_SaidChronologyEventAndMentions` | 通过 |
| K2 | `K2_CompletedAndCancelledTurnItems` | 通过 |
| K3 | `K3_CorrectionAndChangeUseCurrentText` | 通过 |
| K4 | `K4_SingleRecallGetsTimelineOnlyWithSaidDate` | 通过 |
| K5 | `K5_UnknownSaidTimeLastAndEmpty` | 通过 |
| K6 | `K6_AllItemsDetermineStatus` | 通过 |
| K7 | `K7_RawOriginalStaysInEvidenceList` | 通过 |
| K8 | `K8_UnrelatedItemFromSameOriginalDoesNotChangeStatus` | 通过 |
| K11 | `K11_LegacySecretaryMemoryUsesOriginalRecordedTime` | 通过 |

| 浏览器 | 同提交结果 | 覆盖内容 |
|---|---|---|
| V1 `V1 四种状态、说话日期和事件日期、人地点与无日期，390px不溢出` | 通过 | 四状态、说话日期属性及可见文字、事件年月日、区间实际最后一天、时间不详、人地点、390px 页面及条目不溢出；新增跨年事件要求年份 |
| V2 `V2 点击时间轴条目直接打开当时的原话` | 通过 | 全文自动可见，原话 ID 和版本请求正确，没有页面异常 |
| V3 `V3 真实结构化记忆→秘书实际请求→去年的时间轴→当时原话` | 通过 | 独立 SQL 造数、真实秘书链路与捕获的模型 HTTP 请求、去年单条时间轴、说话日期、人地点、390px、打开当时原话 |

### 发现清单及旧抽取失败单列

第三轮第 3 批产品发现清单为空，前两轮的造数失败已修正且本轮统一通过；R12、S7、V1 的三项待裁定均按第 8 节处理并通过。没有新增未执行的第 3 批序列或通过跳过掩盖的失败。

以下两组既有失败保持原样，由 T2 按契约调整，第 2 批进 main 之前继续单列，不归为第 3 批发现：

| 既有测试 | 本轮实际输出 | 处理 |
|---|---|---|
| `TestExtractionWithoutModelStaysNotConfigured` | `extraction_failure_test.go:72`：`provider_not_configured` | 第 2 批 T2；T3 未修改 |
| `TestExtractionConfirmationRequiresCurrentVerbatimCapture` | `extraction_lifecycle_test.go:64`：`unresolved_subject`、`unresolved_predicate`、`paraphrased_assertion` 实际 `adopted`，旧断言 `candidate`；其他子用例通过 | 第 2 批 T2；T3 未修改 |

三个跳过仍是既有的 `TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`，本轮只运行假模型，不接真实模型。上线后的真实默认通道、黄金路径和冒烟测试由协调者按契约第 6 节执行，本地验收未覆盖线上运行。

### 运行边界与交付

全量使用自建 `pgvector/pgvector:0.8.2-pg16-bookworm` tmpfs PostgreSQL，随机 loopback 端口和每测试独立 schema。本机假模型的实际 HTTP 请求是模型输入断言的来源；浏览器 runner 使用自己管理的临时数据库与 PID。未读取线上配置，未调用真实模型、线上库或真实通知；仅按 ID 清理自己的容器，临时服务已清理。

日志、34 条 Go 的逐项结果及整库计数、两个单列失败的完整输出、浏览器 JSON/服务日志、冻结修改审计和被测提交 manifest 在 `/tmp/pcas-b3-t3-round3-a174b92/`。测试和报告交付在 [Draft PR #86](https://github.com/soaringjerry/PCAS/pull/86)，base 为 `phase2/batch3`。第 3 批本地验收已通过；整库 `make check` 的两组旧测试仍等待 T2 的调整合入。

## 635d1bb：第 2 批进 main 后的整库确认（2026-10-03）

### 基线和命令

按协调者通知 fetch、变基到 `635d1bb69f7ae98b817f7f9481ec3a51f01c460a`。变基只在浏览器工作流的模拟用例列表产生冲突：合并后保留已合入的 `tests/phase2-batch2.spec.ts`，并保留 T3 的 `tests/phase2-batch3.spec.ts`；第 2 批真实后端步骤原样保留，T3 的 V3 步骤保留。其他变基自动完成。

统一本地被测提交 `119f74bb86b18ac10c59bcb32bbb68396b6a0056`，Go 与 V1–V3 都在该提交上各跑一次；执行期间工作树保持干净，冻结文件与上一轮逐字节相同。

- `PCAS_TEST_DATABASE_URL=<自建临时库> GOFLAGS='-v' make check`：**退出 0**。fmt-check、go vet、Go race 测试及 make 的 build 目标全部通过，PostgreSQL 包耗时 794.975 秒。
- 全部 Go 顶层用例 **438 通过 / 0 失败 / 3 跳过**。T3 **34 通过 / 0 失败 / 0 跳过**。没有定向补跑。
- Node 22.23.3：`npm ci && npm run lint && npm run type-check && npm run build` 通过。
- 临时真实后端的 V1–V3，`--retries=0 --reporter=list,json`：**3 通过 / 0 失败 / 0 跳过 / 0 重试**。

### 第 3 批序列结果

测试后缀统一加上 `TestPhase2B3_`；文件映射仍见前文。下表全部结果来自本轮 `119f74b`。

| 序列 | 测试后缀 | 本轮结果 |
|---|---|---|
| P1 | `P1_ChengduRecall` | 通过 |
| P2 | `P2_EveryTimePhrase` | 通过 |
| P3 | `P3_LocalCalendarBoundaries` | 通过 |
| P4 | `P4_NoTimeAndMeaningIndependent` | 通过 |
| P5 | `P5_MostRecentMonthAndDay` | 通过 |
| P6 | `P6_NaturesAndRecallWords` | 通过 |
| P7 | `P7_AxisAndFirstPhrase` | 通过 |
| S6 | `S6_NoConditionsPreserveBaselineBytesAndOnlyAppendMetadata` | 通过 |
| S10 | `S10_PublicRecallFifteenRequestsByteIdentical` | 通过 |
| S6 | `S6_MetadataDoesNotConsumeDeputyOrManualByteBudget` | 通过 |
| S8 | `S8_LargeMetadataStillKeepsTheSameTopTwenty` | 通过 |
| S1 | `S1_StructuredHitsFirstWithLocalMetadata` | 通过 |
| S2 | `S2_VisibilityAndExcerptTimePriority` | 通过 |
| S3 | `S3_RelaxTimeOnlyWhenEntityMatches` | 通过 |
| S4 | `S4_TimeOnlyNaturePriorityAndSaidAxis` | 通过 |
| S5 | `S5_EntityOnlyAcrossYears` | 通过 |
| S7 | `S7_AllExistingFiltersApplyToStructuredHits` | 通过 |
| S8 | `S8_SixtyHitsKeepTopTwentyAndExcerptBudget` | 通过 |
| S9 | `S9_SecretaryDeputyManualShareStructuredPrefix` | 通过 |
| S11 | `S11_StructuredDependencyCorrectionMarksTurnOutdated` | 通过 |
| S12 | `S12_AliasLengthSubstringCaseAndDeletedEntity` | 通过 |
| S13 | `S13_OnlyCurrentUtterancePlansEntities` | 通过 |
| S1 | `S1_EventPrecisionAndInclusiveRangeDisplay` | 通过 |
| K9 | `K9_OldCreationReceiptSurvivesPurgedActionDetails` | 通过 |
| K10 | `K10_UndoneCreationLeavesNoAssociatedItems` | 通过 |
| K1 | `K1_SaidChronologyEventAndMentions` | 通过 |
| K2 | `K2_CompletedAndCancelledTurnItems` | 通过 |
| K3 | `K3_CorrectionAndChangeUseCurrentText` | 通过 |
| K4 | `K4_SingleRecallGetsTimelineOnlyWithSaidDate` | 通过 |
| K5 | `K5_UnknownSaidTimeLastAndEmpty` | 通过 |
| K6 | `K6_AllItemsDetermineStatus` | 通过 |
| K7 | `K7_RawOriginalStaysInEvidenceList` | 通过 |
| K8 | `K8_UnrelatedItemFromSameOriginalDoesNotChangeStatus` | 通过 |
| K11 | `K11_LegacySecretaryMemoryUsesOriginalRecordedTime` | 通过 |

| 浏览器 | 本轮结果 |
|---|---|
| V1 `V1 四种状态、说话日期和事件日期、人地点与无日期，390px不溢出` | 通过，含跨年事件日期检查 |
| V2 `V2 点击时间轴条目直接打开当时的原话` | 通过 |
| V3 `V3 真实结构化记忆→秘书实际请求→去年的时间轴→当时原话` | 通过 |

### 旧失败及发现

第 2 批的两组旧抽取测试已随 main 的调整通过：`TestExtractionWithoutModelStaysNotConfigured` 通过；`TestExtractionConfirmationRequiresCurrentVerbatimCapture` 及原失败的 `unresolved_subject`、`unresolved_predicate`、`paraphrased_assertion` 三个子用例均通过。T3 没有改这些测试。

发现清单为空。3 个跳过仍为既有的 `TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`；本轮不接真实模型，第 3 批没有跳过。线上真实通道及黄金路径仍由协调者按契约执行。

### 交付和边界

只使用自建 tmpfs PostgreSQL16/pgvector0.8.2、随机 loopback 端口、独立 schema 和本机假模型。未读线上配置，未调真实模型、线上库或真实通知；数据库按本轮记录的 container ID 清理，浏览器 runner 清理自己的容器和 PID。

原始全量日志、逐项结果、前端日志、浏览器 JSON/服务日志、被测提交及退出码 manifest 在 `/tmp/pcas-b3-t3-main-sync-119f74b/`。运行结束后只更新本报告，测试和冻结预期保持原样。报告与测试在 [PR #86](https://github.com/soaringjerry/PCAS/pull/86)，base `phase2/batch3`；PR 最新 head 的 CI 结果另记录在 PR 与本地 manifest，避免将先前提交的检查结果算入当前交付。
