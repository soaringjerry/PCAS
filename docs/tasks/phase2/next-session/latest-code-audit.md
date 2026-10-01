# R1：已部署最新实现与第二阶段缺口复核

日期：2026-10-01。基线为已部署 main `d8d6fb3efe92f7711c5567440e92af362df7fe77`。
只读源码：`/root/PCAS-deploy/current/source`；以下路径均相对该目录、行号为当前 archive。
使用 `/root/PCAS` 的 git 对象比较提交，未使用其旧 checkout 作为实现基线。
本次只读源码、旧报告与现有测试名称；未启动 DB、模型、生产请求，未重新跑任何测试。
未修改产品、测试、worktree、release、git 或 PR；仅新增本 `/tmp` 研究文档。

## 结论与证据分级

1. **静态确认缺口仍在**：Recall 可返回 source，但三个真实消费者仍用当前 claim map 交集，只供给 claim 文本；原文命中被淘汰。
2. **已有基础应复用**：版本原文、逐字证据、时态 claims、graph/Expand、版本摘要、派生字段清理、claim 授权复核和同会话历史屏蔽。
3. **M1 的动态证据仍属旧基线**：`7aae834d34b3749bdceaf0a2b704b79e837a391e` 上，fake provider 实收证明授权 raw source 在 desk/API/manual 丢失；confirmed claim 纠错/连来源删除下一轮正确。
4. **最新动态待复现**：不能把旧动态结果改写为 d8 本次通过或失败；尤其要在新增会话 admission/order 之后重新采三个入口的最终 payload。
5. d8 的阶段一绿 CI 不证明原文贯通、typed 依赖、工作室硬范围、多模态输入或大数据成本完成。

## 与 M1 基线相比发生了什么

`git diff 7aae834..d8d6fb3`：`runs.go`、`run_context.go`、`retrieval.go`、`sources.go`、`artifacts.go`、`summaries.go`、`processing.go`、provider/codex/siwc 输入适配均无差异。
`desk_turn.go` 变化集中在 N 本轮动作别名、持久会话顺序、过期/不完整轮 capture；263–278 的 claim 交集断点未改。
`editing.go` 新增 `desk-incomplete` 原话删除匹配；不是 typed source hydration 或检索范围改造。
`actions_log.go` 新增动作持久顺序和后继检查；不能据此宣称撤销已经回滚记忆强化。
最新 `docs/tasks/phase2/readiness.md` 是 M0 设计提案；源码/迁移 diff 未显示该提案已实现。

## 三个真实入口的末端装配

| 入口 | 可复用符号与当前断点 | 最终输入/持久化 |
|---|---|---|
| 秘书 DeskTurn | `secretaryContextTx`，desk_turn.go:99/174，当前 `memoriesTx(...,true)`；`secretaryPrompt`:256 Recall；263–278 将 ref 查 `c.Memories`，不命中即丢；无 ref.Version 比较 | 393 `GenerateWithSearchSchema(system,prompt,schema)`；526 保存 question/answer/dependencies/response，未保存完整 prompt、候选/拒绝 manifest |
| 副手 API Execute→requestRun→RunAgents | `prepareRunContext`，run_context.go:87–95，Recall 只保留 Memories；`runCommandTx`，runs.go:108–142，与当前 claims map 交集，prepared 检查版本，锁内 recall 不检查 ref.Version | runs.go:154 Brief；183 Run 文档持久；186 依赖另表；445 Generate(system,run.Brief) |
| 手动交接 | 同 `runCommandTx`，不是独立原文导出；155–158 manual=waiting；相关记忆装配完全相同 | Brief 可见/复制；外部人工附加上下文与外部模型请求无法由 PCAS 观察，不能声称已完整记录 |

Desk 不消费 Recall 的 Summary/Evidence/Coverage/FollowUps；run prepared 也将这些丢弃。
`AnswerDesk` 兼容入口，desk.go:88–127，仍按 current claims map 取 Recall ref，后续统一内核不能漏掉它。
Desk claim 取当前 m.Version，未来若只切换 history 模式，会把旧 ref 版本覆盖为当前版本；run 锁前旧版本直接被排除。
Run 长期 preference/decision 追加（runs.go:119–125）和上轮历史/采纳依赖传播（103–116、145–151）是有价值的已有实现。

## source grants：默认创建与公开修改入口

`memory.Scope`，internal/memory/contracts.go:19–25：可信认证 owner/principal；非 owner 要明确 record grants。
`ingestTx`，sources.go:33–140，新 source/version、队列同事务，但不写 source record_grants；attachments 同样走 ingestTx。
`rememberTx`，claims.go:183，只给**创建时已注册 enabled agents**授予新 claim；不等于给来源或其他类型授权，也不回补后来新建 agent。
`setMemoryVisibility`，editing.go:21/31–49，先 activeClaim，仅修改 claim grants；当前不是通用 source 授权入口。
全搜产品 record_grants 写入另见 workspace.go:142，把既有 chatgpt grants 复制给 chatgpt-direct；没有凭空 source 授权。
GetSource/Recall/Expand/Summarize/graph 均已有非 owner 授权过滤；owner provenance 卡片补读不能冒充模型已读原文。
因此要分开验收“普通摄入后如何明确授权原文”和“已明确授权的原文为何在消费装配丢失”；不能用默认全 source 可见掩盖两者。

## typed hydration 与依赖边界

`memory.Ref`，internal/memory/model.go:22–27 已有 id/version/kind；Source/Chunk/Entity/Episode/Claim/Relation/Summary 正式类型都存在。
`RecallResult`，contracts.go:93–99 已有 Summary、Memories、Evidence、Coverage、FollowUps；`Expand` 同文件:110–117 可按类型展开。
`recallTx`，retrieval.go:179/197–206，读取真实 kind/version/body并保留 ref；source 可用匹配 chunk 文本，不要求已抽取 claim。
`verifyRunForItemTx`，runs.go:365–383，**忽略 ref.Kind**，统一查 applicable_claim_versions/readClaim，再查 nature、confirmation、project/exclusions。
`run_dependencies` 写入（runs.go:187）只存 id/version；`sanitizeItemTx`，artifacts.go:60–68，SQL 验证也全部 claim，67 重建 `kind=claim`。
将 raw Summary 拼进 prompt 而不改上述闭包，会出现没有 input deps 的原文、source ref 被 claim verifier 拒绝、或派生副本权限丢失。
统一契约至少需保留 typed ref、来源版本/跨度、视图时点、供给片段、当前/历史适用语义、精确范围与派生 refs；不存在“只改 prompt”的安全独立子任务。
`Summarize`，summaries.go:32–62 已读授权成员；102/166–170 保存版本依赖；Recall.Summary 是即时拼接摘录，并非版本化 summary 对象。

## 实际输入必须与检索命中、Used、派生依赖分开

候选：Recall 返回集合，不等于全 SQL 候选；需记录被预算/权限/范围/版本过滤的理由且避免记录不可见名称。
供给：Desk sent claim 与 c.Dependencies 可证明指定 claim 文本供给；当前不持久完整最终 prompt；Run.Brief 持久但混有历史/派生文字。
Used：desk_turn.go:639–646 只按模型 answer.Used 生成卡片；owner provenance 补读明确不进入 prompt；Used=[] 不能否定供给。
派生：runs.go:103–116/145–151 传播历史答案/上次输出/采纳 refs，可能本轮没有逐字该原文；不能作为本轮供给清单。
Provider 请求边界：ai/provider.go:243 OpenAI system/user messages；246 Responses instructions/input/store=false；250 Anthropic system/user。
siwc/inference.go:54/78 以 instructions+user prompt/store=false；codex.go:285/320 ephemeral thread+text prompt，工具受限；均是文本接口。
未看到 provider 前最终序列化 payload 的通用持久 manifest；验收应捕获实际 HTTP/adapter 输入，schema/框架提示、搜索启用和历史单列。
输入记录只能证明适配器准备/调用或可观测网络尝试，未必证明远端收到；不能证明模型内部因果，也不能覆盖手动交接用户自己添加的内容。
Codex 每次 generate 都在293创建新 ephemeral thread、318 archive，320新增 prompt；当前没有跨 generate复用provider历史。线程 base/developer instructions、outputSchema、web_search配置需一起记录，不能只记文本prompt。
Desk 整个模型调用在 `withOrderedSecretaryTurn` 的事务工作内（desk_turn.go:341–393），依赖/回答至526才写；调用后错误/取消/回滚可能没有实际尝试审计，fallback 的 dependencies 为空。
Run Brief在调用前已持久，但 run状态/最终结果不能证明网络 dispatch；475复核或崩溃失败仍需要区分本次是否尝试发送。
首批 manifest 应按 attempt 分 prepared/dispatched/outcomeunknown/failed，并记录可观测层与传输确认强度；这是端到端输入验收必须先裁定的小范围持久化问题。
建议复用run/desk请求身份与最小typed refs/hash/配置元数据，设删除清理关系；不要因此新增独立事实库或默认永久存一份完整私密prompt。需要全文采证时限定合成fixture/明确保留期。

## 范围、结构化遗漏与跨来源补漏

WorkingContext.Objects（contracts.go:71–76）用于相关对象；retrieval.go:137 给 subject/project 加分、163–165 OR 纳入相关成员，**不是工作室 hard scope**。
Recall WHERE 已有 owner/grants/active/version/time 硬边界（159–162）；项目 claim 末端在 runs.go:134/382过滤；这些不能直接作为来源级 scope 规范。
原文source body与record/version在摄入事务提交后可走未索引词面路径（sources.go:90–94；retrieval.go:139–165）；但agent还需source grant，多模态还需派生文本。不能从“无需claim完成”推导承诺的可检索延迟。
SQL对memory_text逐body位置匹配、tokens及embedding/chunk相关子查询排序后LIMIT；候选预算限制返回量，不证明扫描量有界。pending数量上限100（244），不是全积压计量。本次未跑EXPLAIN/规模测试，索引扫描与摄入后延迟仍待测。
Desk 通用 recall Objects=[]（256）；ThingID 下逐 claim verify（268–270）；来源尚无对应的工作室匹配/允许全局回退契约。
`extractedItem`，processing.go:219–230，只含主体/性质/谓词/quote 等；455 写 statement 没有系统地点、时间、经历关系写齐。
因此抽取地点/主体/时间缺失不应作为永久 raw 排除；权限/可信范围仍不可放宽。候选定位应容许有结构命中但取消原因、同行人等原文未抽取。
`Recall`，retrieval.go:115–122，pending/gaps 可标不完整，但 FollowUps 仅零命中时给文字建议；没有非零不完整后的自动补漏循环。
跨来源工具已在 graphTx、Expand、source_context adjacent messages；新 planner 应复用其边界而非另建秘书专用检索规则。

## 删除/纠错/撤权已有闭包与新增类型要求

`correctTx`/`invalidateTx`，editing.go:114/207–214，版本纠正，递归 stale derived_views，标记依赖 run/training stale。
`deleteRecordsTx`，editing.go:313–343，IncludeSources 与 source→chunk/evidence/derived/relation/entity/archive/attachment派生来源递归闭包。
同文件:443 purgeArtifactsTx；506–514 清空 desk 原话、回答、cards/ask，收据仅骨架；516–525 删除派生文档/run/训练/证据/版本。
Desk 生成前后 `checkDeskContextTx`（383/419）复核；历史 `deskTurnsTx`（584–589）对 stale deps 改占位，避免同段旧答案继续供给。
Run claim 在生成前428、生成后475、采纳入口复核；typed 资料应沿相同 gate，不删掉现有保护。
旧 M1 confirmed claim 新会话及同 conversationId 纠错/删除实证有效；新 raw/version/span/summary/source撤权闭包仍需最新入口验证。
IncludeSources=false 删除 claim 后独立授权 source 可保留，这不应被误计泄漏；IncludeSources=true、撤 source grant 才按明确目标断言禁供给。

## actions、undo 与原始多模态

commands.go:550–555 `saveAction` 把完整 Item JSON 摄入 connector=actions；ingestTx 正常排 chunk 队列。
processing.go:334–340 **已跳过 actions/corrections/memory-input 的自动抽取**，不能把“仍自动抽取actions”作为当前缺陷。
但 ProcessIndex/Embedding/Summary 没有统一用途过滤（processing.go:78–84/98–116；summaries.go:32）；Recall 也未排除 actions，因此仍可索引/向量/摘要/owner raw命中 JSON。
source grants 缺口暂时抑制 agent raw 暴露，不等于治理完成；授权原文贯通前必须定义 action 知识投影与用途，保留审计权威记录。
undoActionTx，actions_log.go:101–121/235+，先查窗口/后继顺序，再指纹与已启动工作；这是最新已有撤销强化。
adoptRunTx，runs.go:319–325，会留样本且 recordUse(adoption)；editing.go:241–245 更新 use_events/activity。
undoActionTx:127 仅恢复 work_items/work_documents/agent_runs/training_samples；未同步删除 use_events 或回滚 activity 强化，属静态待动态复现的独立 backlog。
attachments.go:19–77 保存原 blob 并可授权打开，79–213 支持 archive/PDF/OCR/audio transcript及 derived_from；应复用原始来源链。
最终 generation 只接受文本，不等于原始图片/音频已直接供模型；多模态需求需明确“原件可追溯”和“供给OCR/transcript/原件”分别验收。

## 建议核验/修补优先级与依赖（研究建议，不启动开发）

1. P0 冻结统一契约：source授权政策、工作室硬范围/允许全局回退、current/history时点、typed input与派生依赖；三个入口共同确认，否则检索/消费/删除agent会各写不同语义。
2. P0 冻结独立 gold 与旧证据分层：复用 M1 fixture 后在 d8 隔离库捕获desk/model/manual完整输入；零claims raw命中、confirmed正对照、Used=[]、同段下一轮纠错/删除分别验收。
3. P0 实现通用 typed hydrate/authorize/verify，再贯通消费者；同步run_dependencies kind反序列化、artifact verifier、summary依赖与阶段gate。依赖1；以2为验收。
4. P0 定义摄入/原件/派生文本 source grants生命周期与可操作公开入口；测试新agent、source升级、claim可见source不可见；与3并行设计但使用同一政策。
5. P1 持久供给 manifest与最终payload证明：候选/实际输入/Used/间接依赖独立字段，精确版本/跨度/截断理由；其删除闭包先设计，不能产生新保留副本。
6. P1 加范围内非零补漏：未知/误抽地点时间、跨来源取消原因、pending和预算不足；只软化模型抽取过滤，不放宽授权/工作室边界。依赖1/3/5。
7. P1 actions用途过滤、合法完成/取消事件投影、旧派生治理；先保留actions审计/undo，再避免JSON日常暴露；与原文贯通同批安全门槛。
8. P1 新typed资料纠错/删除/撤权/在途/采纳闭包，用同会话历史+引用源版本+摘要+手改派生+原blob组合验证。依赖3/4/5/7。
9. P2 精确时间线/action关系与undo强化核验：卡片现用recorded_at（desk_turn.go:648–661）和同source首事项（668），不能外推真发生时间/该活动完成；采纳撤销use_event另列。
10. P2 三入口真实模型、规模/P95/token/费用评测：复用现有预算/游标/窗口job，记录真实usage；先测后定优化，不立即换DB、不用30KB或Tokens×3冒充token指标。

可复用现有回归：TestRecallReturnsMatchingTailChunk、TestTemporalChangeCorrectionAndHistoricalKnowledge、TestSourceReplacementRetainsHistoryAndRefreshesRepeatedEvidence、TestConcurrentSummaryAndGrantRevocation、TestSecretaryOriginalSourceDeletionScrubsHistoryAndReplay、TestApprovedUndoHistoricalOrder。
这些测试本次仅阅读定位，未执行；既有测试绿、M1假模型证据、最新静态判断、未来真实模型/规模实验应四栏分开报告。
