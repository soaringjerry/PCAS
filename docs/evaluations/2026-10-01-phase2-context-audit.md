# 二阶段真实上下文装配审计

2026-10-01，M1。精确产品基线 `7aae834d34b3749bdceaf0a2b704b79e837a391e`，工作区 `/root/PCAS-wt/M1-audit`，分支 `review/phase2-context-audit`。旧独立调查基线为 `a3adb4bd0b453e2119c76a64d398dccb7f0652d7`。本次只读产品和既有测试，仅新增本报告及 [phase2_context_audit_test.go](../../internal/postgres/phase2_context_audit_test.go)，不实现二阶段，不更改公共契约，不合并或部署。

审查依据为 [M0 readiness](../tasks/phase2/readiness.md)、[白皮书 §5、§15–17](../whitepaper.md)、用户独立调查以及候选上的真实消费路径。M0 的“命中到回答仍丢信息”判断在最新候选成立：**显式授权、尚未抽成 claim 的原文能够被 agent Recall 找到，但在 DeskTurn、副手 API 和手动交接装配时被丢弃。** 同 fixture 直接给模型 Recall Summary 能收到原文，因此旧回放结果不能证明这些真实入口。

## 1 证据口径与结论分级

| 级别 | 本次结论 | 证据边界 |
|---|---|---|
| 已实现并有覆盖 | 原文/版本、陈述/逐字证据、时间修订、摘要依赖、纠正/删除/原文展开等基础应复用 | 下文列实现和已有回归；本次追加验证 confirmed claim 进入真实 DeskTurn 请求，纠正/连来源删除后的下一轮供给 |
| 实现存在但缺入口/证据 | Recall 能读授权原文；Summarize 有原文、成员和来源依赖，但没有统一进入三种消费者 | 原文 Recall 本次实证；摘要及历史扩展静态核查，没有证明完整回忆体验 |
| 最新复现缺陷 | 授权原文命中与实际请求之间按当前 claim map 交集导致原文丢失 | 本次 DeskTurn、API RunAgents、manual Brief 实证；明确开启未来原文要求时真实失败 |
| 静态待证 | 历史 claim 版本也会被当前 map 覆盖/淘汰；source 授权入口缺口；日期/状态卡片推定；状态更新在途边界 | 有具体代码，但没有把它们包装为本次端到端复现通过/失败 |
| 后续实验 | 跨来源取消原因、错误/缺失地点时间、非零命中时继续补漏、模糊时间/指代与全历史覆盖 | 已授权 scope 内进一步检索；需独立 gold、真实模型和实际入口评测，不能从单个合成 marker 外推 |

`git diff` 对旧 SHA 与候选核对：`runs.go`、`run_context.go`、`live_replay_test.go` 没有差异；`desk_turn.go` 改动集中于 F11 的锁、schema/失败分类、截断及持久化，254–276 行的 Recall→claim 交集断点仍在。F11 消除的 D1 假发现已撤回，不把已有删除闭包重提为新系统。

## 2 入口矩阵与准确断点

所有文件行号以产品候选为准。

| 入口 | 输入/中间集合/最后装配 | 依赖与本次证据 |
|---|---|---|
| 秘书 `DeskTurn` | [desk_turn.go](../../internal/postgres/desk_turn.go) 98、169：`deskContextTx`→`memoriesTx(...,true)` 获当前 claims；254：agent scope Recall；259–276：ref 按 ID 查 `c.Memories`，失败即 continue，只发送 `m.Text`，忽略 Recall Summary/Evidence/Coverage | 276 追加 claim refs，372/408 前后检查，496 持久化 dependencies。本次 provider 接到“召回的记忆…（没有）”，原文 marker 缺失、deps=[] |
| 副手执行 `Execute`→`requestRun`→`RunAgents` | [run_context.go](../../internal/postgres/run_context.go) 87–95：锁前 agent Recall 只保留 `result.Memories`；[runs.go](../../internal/postgres/runs.go) 96–121：当前 claims `byID` 与 prepared ref（还校验版本）交集；129–142：锁内 recall ref 再与同 map 相交。只补长期 preference/decision，输出 claim 文本 | 142 ContextVersions、186 run_dependencies、154 Brief 持久化；445 Generate 使用 run.Brief。本次 API Brief 和实际 provider 请求均不含原文 |
| 手动交接 `requestRun(agent=manual)` | 同一个 `runCommandTx`，155–158 切换 waiting；不是独立的 Recall Summary 导出路径 | 本次 waiting Brief 不含原文、deps=[]；没有声称观察到用户复制给外部模型后的隐藏上下文 |
| 旧 `AnswerDesk` | [desk.go](../../internal/postgres/desk.go) 88–97 agent Recall/current visible claims；120–127 同样按 ID 查 claims | 此入口仍需纳入兼容设计；本次只静态，不另重复跑一个同断点用例 |
| 用户手动记忆查找 | Recall/Expand/GetSource 在 owner 或明确 record grants 下返回版本原文/陈述/证据 | 本次 agent Recall 原文实证；不等同于秘书输入/最终回答质量 |
| `TestLiveContinuityReplay` | [live_replay_test.go](../../internal/postgres/live_replay_test.go) 104：owner Recall；108–123：Memories/Evidence 的 source ID 命中统计；127：直接 Generate `out.Summary`+gaps | 无 DeskTurn/Execute/run Brief 装配，无真实消费者前后依赖核验；旧 7/7 仅来源级检索基线 |

DeskTurn 按 ID 交集还不比较召回 ref.Version，而从当前 map 取得 m.Version；如果将来直接改 Recall mode 为 history，不能据此宣称保留了历史版本。runs 的锁前版本不同会直接不选；锁内只按 ID 查当前对象。历史陈述/历史原文贯通需要明确视图和依赖规则，不只换检索模式。

## 3 最小真实入口复现

所有资料均为合成，fake provider 监听 `127.0.0.1:18150`，没有真实模型/账户、通知或生产访问。独立 Docker `pcas-m1-context-audit-20261001` 使用已缓存 `pgvector/pgvector:0.8.2-pg16-bookworm`（image ID `be2dedd21573`），PG 数据为 384MiB tmpfs，随机仅 localhost DB 端口 `33264`，容器启动 PID `4031681`。每次 testStore 建临时 schema 并清理。

授权前置：先 SetModels、Snapshot 注册 model/manual，再 public Ingest。**原始 source grants 实测为 0**。为了单独验证“已授权仍被装配丢弃”，审计夹具明确 SQL 写入该合成 source 对 model/manual 的 record grants，然后用完全相同的 model principal Recall 强断言命中。SQL 只准备授权夹具，不改变产品、检索或生成实现；这不证明普通用户能通过现有公开 UI/API 完成同样 source 授权。

原文为 `auditroute 的交付暗号是 rawSecretM1。`；问题为 `auditroute 交付暗号是什么`。问题、事项 title、模型固定回答都不带答案 marker。抽取/chunk/tokenize 暂不处理，数据库 claims=0。fake server 解码真实 HTTP `messages`、保存完整 system/user 内容，不拿返回 Used/回答中的字符串代替输入证据。

| 同一资料的阶段 | 实际观察 |
|---|---|
| agent Recall | `Memories=[{ID:sourceID, Version:1, Kind:source}]`；Summary 含 `rawSecretM1`；Coverage.complete=false，明确 pending/无向量缺口 |
| 模仿 live_replay 直接 Generate Summary | fake provider 实收原文 marker；这是绕过消费装配的对照 |
| DeskTurn | 调用成功、固定审计回复，实际 provider user 内容 `召回的记忆（引用短别名）：\n（没有）`；marker=false；持久 dependencies=[] |
| manual requestRun | waiting Brief 中“相关记忆”空；marker=false；ContextVersions=[] |
| model requestRun + public RunAgents | API Brief marker=false；queued run 完成 done，fake provider 实收请求 marker=false；ContextVersions=[] |

正对照使用公开 capture→acceptCandidate 创建 `fact` confirmed claim，默认保守 agent 设置，不开放 IncludeInferred；所有模型都在创建 claim 前注册。DeskTurn 实际请求含 `claimBeforeM1`。fake 返回 Used=[]，持久 dependencies 仍含该 `claim@1`，证明输入依赖并不取决于模型声称引用。公开 editMemory 纠正后，新会话下一轮实际请求含 `claimAfterM1`、不含旧 marker；公开 deleteMemory(includeSources=true) 后下一轮两个 marker 均不再供给。这里刻意使用新会话，不将历史回放与直接下一轮供给混在一起。

可复现命令（DB 必须为自己的一次性实例）：

```sh
docker run -d --name pcas-m1-context-audit-20261001 \
  --tmpfs /var/lib/postgresql/data:rw,noexec,nosuid,size=384m \
  -e POSTGRES_PASSWORD=m1-disposable-only -e POSTGRES_DB=pcas_m1_audit \
  -p 127.0.0.1::5432 pgvector/pgvector:0.8.2-pg16-bookworm
docker port pcas-m1-context-audit-20261001 5432
# 使用输出端口设置 disposable DSN，并等 pg_isready。
PCAS_TEST_DATABASE_URL='<disposable DSN>' PCAS_PHASE2_CONTEXT_AUDIT=1 \
  go test ./internal/postgres -run '^TestPhase2ContextAudit$' -count=1 -v

# 显式要求原文贯通：候选上的预期真实失败，不是默认 CI 契约。
PCAS_TEST_DATABASE_URL='<disposable DSN>' PCAS_PHASE2_CONTEXT_AUDIT=1 \
  PCAS_PHASE2_AUDIT_REQUIRE_RAW=1 \
  go test ./internal/postgres -run '^TestPhase2ContextAudit/authorized_unstructured_source$' -count=1 -v

docker stop pcas-m1-context-audit-20261001
docker rm -v pcas-m1-context-audit-20261001
```

定稿测试观察模式退出 0，总计 1.580s，两个子用例通过；日志 `/tmp/m1-context-audit-observation.log`。未来原文要求退出 1，1.342s，明确失败 `future expectation: authorized raw Recall hit was dropped before DeskTurn provider request`；日志 `/tmp/m1-context-audit-required-raw.log`。观察模式 PASS 仅说明采证和已实现的正对照断言完成，**不等于原文贯通已通过或二阶段验收通过**；默认不启用该审计时的 skip 不作为验收证据。未重跑完整产品回归，完整候选验收归 A1。

## 4 供给、引用与依赖不能混用

| 集合 | 当前能证明什么 | 当前记录/缺口 |
|---|---|---|
| 检索返回候选 | 在调用 scope/budget 内 Recall 返回了哪些 ID/version/kind | RecallResult 中有 refs/evidence/summary/coverage；实际消费者通常不持久保存这一层。不是全 SQL 候选全集，不是实际输入 |
| 最后模型供给 | 实际请求包含哪些文字、对象和版本 | Run.Brief 持久化；DeskTurn dependencies 记录发送 claims/派生输入 refs，但没有持久化完整实际 prompt/独立输入 manifest。本次仅 fake HTTP 捕获实际供给 |
| 模型声称引用 Used | 模型输出选择了哪些已提供 alias | Desk sources/timeline 卡片按 [desk_turn.go](../../internal/postgres/desk_turn.go) 653–677 的 answer.Used 生成；来源为 owner 响应补读，不能反推原文曾发送给模型。Used=[] 也可能有实际供给/deps |
| 间接派生依赖 | 复用上轮答案、手改文档等应追溯到哪些输入 | runs.go 103–116、145–151 传播历史/上次输出/artifact refs，即使本轮原claim超预算仍保留；Summarize 102、166–170 保存版本依赖。它们不是本轮逐字原文输入清单 |

输入记录证明供给范围，不能证明模型内部因果、真实“使用”了哪条证据，不能推断手动用户/远端 provider 额外隐藏上下文。建议验收分别报告这四个集合和拒绝原因，不以 Used/source cards 代替输入观测。

## 5 已有基础与授权/校验扩展范围

原文/版本/证据追溯：sources.go `ingestTx` 的来源身份、external version 去重/冲突、record_versions.expressed_at、队列同事务；retrieval.go 原文未索引 fallback、匹配 chunk、Evidence/Expand；claims.go 逐字 quote 定位和 source keys。公开 Commit 支持 entities/claims/episodes/relations、时间和证据版本；自动 ProcessExtraction 453–455 仍只传主体/文本/nature等 statement，没有系统地点/时间/经历写齐。不能把显式 Commit 能力当自动抽取完成。

时间/更正：migrations/007_temporal.sql 及 013_extracted_source_versions.sql 的 applicable_claim_versions 区分 correction 与 change 的组/有效/已知时间；来源升级保留历史，AI claim 当前视图要求来源版本支持。editMemory/Correct 留版本、纠正来源、redirect/block；[TestTemporalChangeCorrectionAndHistoricalKnowledge](../../internal/postgres/replay_test.go) 等已有覆盖，本次没有重跑，也不承诺所有语义改写重导入都被阻断。

摘要：summaries.go 32–62 对 root/成员授权、版本、适用 claims 读取，102 将成员 refs 留为 Dependencies，166–170 写 derived_dependencies，缓存带 membership hash/principal。Recall 的 `Summary` 是查询时拼接摘录，**并不等同于 Summarize 的版本化派生对象**。

依赖核验：runs.go 345–383 的 verifyRunForItemTx 对每一 ContextVersion 都查询 applicable_claim_versions/readClaim，校验 agent MemoryKinds、IncludeInferred、项目和 exclusions。当前 refs 类型虽然有 Kind，循环没有 source/episode/entity/summary 的分派。artifacts.go 67 将 run_dependencies 重建成 `kind=claim`。因此把 Recall.Summary 或原文直接追加 prompt/Brief，要么没有完整依赖保护，要么 source refs 被按 claim 拒绝；正确扩展需同步 hydrate、typed verifier、run dependencies 反序列化、派生/撤权/删除闭包，不只是改提示词。

source 授权静态全搜：产品中 record_grants 写入仅见 claims.go 183（创建 claim 授予已注册 enabled agents）、editing.go 31–46（先 activeClaim，仅陈述可见性）、workspace.go 142（复制既有 chatgpt grants 到新 direct channel）。Ingest/ImportBatch/Commit 没有新 source grant；HTTP sources 只有摄入/读取，没有公开 source grant 写入口。已有模型配置迁移只能复制已有授权，不能凭空授予原文。owner 原文可见、worker owner scope 能抽取、claim grant 与模型原文授权是不同事实。此为静态入口/传播缺口，应裁定明确 source 授权方式；不能靠开启 IncludeInferred、默认全量 source 可见修掉。

删除/撤权/在途：本次只新增 confirmed claim 下一轮纠正与连来源删除实证。复用 [F11 D1 更正证据](2026-10-01-secretary-repairs.md#d1-更正证据)：先确认真实模型收到授权 claim，再 barrier 内删除，旧结果/动作不提交、回执留骨架，已有实现通过。F11 的旧未授权夹具 finding 已撤回。desk.go/runs.go 的现有撤权/在途测试、summary 并发撤权测试可复用，但本次未重跑；typed 原文历史包、source/关系撤权和计划状态在途组合仍须新增对应验收，不能由 claim-only D1 外推。

## 6 最小建议顺序与未覆盖边界

1. **先贯通统一安全证据装配。** 在现有 Store/provider/类型上，为 secretary/AnswerDesk、API/manual run 共用按类型与版本 hydrate/authorize/verify 的材料；保留 source/version/span/claim/时间/派生依赖及 coverage，不再以当前 claims 集合排除所有原文。先明确 source 授权入口/政策，与“已授权丢失”分开验收。生成前、在途返回、采纳和下一轮都复核相同证据闭包。
2. **再增强定位与补漏。** 身份/权限/可见/删除是不可放宽的硬边界；明确可信工作室限制同样尊重。模型抽取的地点、主体、时间、性质缺失/错误不能变成永久排除原文的条件。已有 Recall 未索引原文能力要保留；零结构化、非零但缺解释/取消原因/意向成员、pending、未知时间、预算不足均可触发同边界补漏。当前 FollowUps 只在 Memories=0 时给文字建议，没有非零不足时自动追加检索。
3. **再连正确当前状态与卡片。** 当前 Desk 卡片 source date 是 recorded_at，timeline 状态按同 source 第一条 work_item（desk_turn.go 662、682）推定；不是 expressed_at，不是精确陈述/行动关系。复用 work_items 计划/完成/取消、claim nature 与 valid time；不能用 confirmed 表示完成，不能把后来取消从历史表达检索删掉。该精确关联本次为静态待证，不宣布完成修补。
4. **以实际入口扩展评测。** 原 live replay 的 12 合成资料、5 问、7 必需 source hits 保留为检索基线；增加 gold source/version/span、禁止输入、覆盖缺口、完整 prompt/Brief、Used/间接依赖分别计数。includeSources=false 后授权独立原文可读不误计泄漏；includeSources=true/撤权/在途则验证禁止供给/提交。真实抽取/生成、时间/别名/跨来源变化、不同工作室/权限、规模/P95/实际 token/费用另测。

本次没有测真实模型回答质量、真实账号/线上部署、跨来源完整取消链、全历史/向量规模、自然语言 planner、timeline 错状态动态复现、source 授权 UI 或真实手机。没有把旧小样本延迟/7/7当全库泛化/真实入口指标，没有声称结构化抽取已齐或自动省了多少费用。第一阶段稳定化门槛与 A1 验收保持独立。

## 7 交付与资源清理

产品和既有测试零修改；新增两个文件。报告和观察测试包含运行方法、实际输入差异、正对照及显式失败开关，可由 reviewer 独立复现。测试退出后 fake server、RunAgents goroutine、临时 schema 均由 cleanup/cancel 释放；自有 PG 容器已 stop/rm，确认容器不存在、HTTP18150/18151 无监听，日志留本机 `/tmp/m1-context-audit-*.log`。分支/PR 只交证据，不合并、不部署，不触碰 `/root/PCAS` 或 A1。
