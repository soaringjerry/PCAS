# M0：记忆核心与二阶段准备（方案待审）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

> **状态（2026-10-01）**：这是第二阶段 2.0 之前做的调查稿。代码现状的调查部分可以当地图用；其中的方案、字段、接口和阈值都没有被批准。2.0 已回滚，第二阶段从 [任务入口](README.md) 开始。

调查日期：2026-10-01。调查提交：`c94b49617764a90841a72bb88adb847ab8f51f33`（main，含 F6 / PR #19）；调度依据为协调分支 `c348e3f` 的 M0 任务包与 dispatch。本次仅静态读代码、类型、迁移及已有报告；没有连接生产、读取秘密、调用模型、运行回放或大规模 benchmark。文中的新增字段、接口、任务边界和阈值均为**待审提案**，没有修改正式契约。

上位依据：[白皮书 §5、§15–17](../../whitepaper.md)、[记忆架构](../../memory-architecture.md)、[服务说明](../../memory-service.md)、[待办清单](../backlog.md)、[稳定化入口](../stabilization/README.md)。PCAS 是秘书、副手、二把手和管家组成的个人支持团队；事项工作室是专属空间。它们都建立在上下文、结构化和向量共用身份与证据的记忆核心上，工作室只增加范围，不另建记忆库或状态库。

## 1 调查结论与最小交付

已有原文版本、事务队列、陈述/实体/经历/关系表、全文与可选向量召回、证据展开、纠正与删除传播。二阶段应扩展这些模块。三个主要缺口：

1. **可定位的信息尚未自动写齐。** 自动抽取只写主体与文本陈述；主体在每个来源内匹配为 `unknown` 实体，未建立跨来源用户身份、地点/人物对象、陈述关联实体及语义经历。自动陈述也没继承表达时间，没抽取有效时间。
2. **没有统一查询规划。** Recall 只有全文/向量/对象加权与 `ValidAt`、`KnownAt` 单点过滤；缺表达时间区间、稳定主体、实体候选、性质和工作室硬范围。向量目前参与同一排序，并非结构化定位之后的补漏阶段。
3. **命中到回答之间仍丢信息。** 秘书和副手虽然调用 Recall，却再次筛成当前陈述；旧原文、未抽取资料、历史陈述可能被丢掉。秘书卡片日期取记录时间，完成状态按同来源第一条事项推定，不能可靠呈现去年意向与后续完成/取消。

最小可交付路径是：保存原文 → 在现有 worker 内补写用户/地点/性质/表达时间及同经历成员 → 同一个 planner 把“我去年说去成都要干什么来着”转成表达时间区间与实体条件 → 结构化定位 → 在相同硬边界内用原文/向量补漏与有限经历展开 → 对每条历史意向核对后续更正和现状 → 返回少量、带原话定位的时间轴卡片。先以成都旅行这个垂直场景贯通三种入口，不同时开发所有角色功能。

稳定化设计和评测集准备可以并行；正式产品实现仍受阶段入口约束：序列测试全部通过、线上实测 11/11、用户试用一天无阻碍使用的问题。本报告不宣布这些已完成。阶段不含复杂实体自动合并、习惯推荐、完整云盘、训练或手机持续采集，只留范围、来源类型和调用目的的接口空间。地点别名和用户身份先采用可靠明确证据与候选，不把全库语义合并当作成都最小交付前置条件。

## 2 真实调用路径与复用点

以下路径是在调查提交上观察到的代码行为；类型定义与模型提示词不等于所有能力已自动实现。

| 环节 | 文件与关键符号 | 当前行为、可复用点和限制 |
|---|---|---|
| 组合与 HTTP | [cmd/pcas/main.go](../../../cmd/pcas/main.go) `run`（serve/worker 分支）；[server.go](../../../internal/httpapi/server.go) `recall` / `expand` | `memory.NewService(db)` 校验来源输入；Recall/Expand 实际由 PostgreSQL Store 实现。worker 同一队列路由 source.parse/chunk/extract/embed/tokenize 和 memory.index/summary/embed。不能把薄 Service 误认为已有 planner。 |
| 直接保存 | [service.go](../../../internal/memory/service.go) `Service.Ingest`；[sources.go](../../../internal/postgres/sources.go) `Ingest` / `ingestTx` / `enqueue` | `(owner,connector,external_id)` 身份与 external_version 去重；同版不同内容冲突；原文、版本、source.chunk 事件同事务；支持尚未索引的原文召回。 |
| 连接器与归档 | [connectors.go](../../../internal/postgres/connectors.go) `importBatchTx` / `linkEpisodeTx` / `ImportArchive`；[source_context.go](../../../internal/postgres/source_context.go) `adjacentContext` | 保存表达时间、角色、分支、父消息及缺口；显式 EpisodeKey 或 ConversationID 可建版本化经历成员，非语义自动拆分。抽取最多读六条相邻消息、每条前 1,200 字符，限定只辅助指代，证据必须来自当前 source。 |
| 分段 | [jobs.go](../../../internal/postgres/jobs.go) `ProcessChunks`；[chunk.go](../../../internal/memory/chunk.go) `SplitText` | 1,200 rune、160 重叠，保留 source ID/version、ordinal、start/end；分段后并列排 source.extract/embed/tokenize，不是先抽取再索引。 |
| 自动理解 | [processing.go](../../../internal/postgres/processing.go) `extractedItem` / `ProcessExtraction` / `extractionConfirmation` | 产 kind/text/nature/subject/predicate/quote/confidence/qualification 等，memory 项交 `rememberTx`；task/idea 留 Candidate，desk 跳过 task/idea 防重复行动。长文 12,000 rune 窗口、11,000 步长，作业有 fencing。没有实体数组、时间区间、经历或关系自动输出。 |
| 陈述与主体 | [claims.go](../../../internal/postgres/claims.go) `statement` / `rememberTx` / `createRecord` | 来源内主体匹配；新增 unknown 实体及同名 alias；陈述、逐字证据定位、claim_source_keys、授权及 memory.index 同事务。ProjectID 可写 scope，但自动抽取没传该值。没有把 source.expressed_at 写入新陈述 record_versions。 |
| 显式结构化提交 | [commit.go](../../../internal/postgres/commit.go) `Commit`；[commit.go](../../../internal/memory/commit.go) `CommitRequest` | 已能提交 Entities/Episodes/Claims/Relations/Evidence，跨应用共享 ID、验证证据与版本、写有效/表达时间。公开 Commit 是 owner 创建流程，actor 固定 user；worker 不应直接调用它冒充用户确认，需要复用事务写入规则并保持 actor=ai。 |
| 全文、向量、摘要 | [processing.go](../../../internal/postgres/processing.go) `ProcessIndex` / `ProcessEmbedding`；[summaries.go](../../../internal/postgres/summaries.go) `summarizeTx` | 分词写 record_search/chunks；来源按 chunk 向量化、陈述按全文，区分模型维度。memory.index 再排 memory.embed；索引排 memory.summary。摘要是带依赖的摘录，核对成员/版本/权限，不是另一套事实。 |
| Recall / 关系 | [retrieval.go](../../../internal/postgres/retrieval.go) `Recall` / `recallTx` / `matchedExcerpt` / `evidenceTx`；[graph.go](../../../internal/postgres/graph.go) `graphTx` | 词项 OR、原文匹配、全文、向量与对象加权；history 可读旧版，continue 少量活跃度加权。实际匹配 chunk 优先，未分段时围绕命中截取。有限图展开要求关系和两端授权，并核对时间。Objects 提高项目/主体得分，未构成工作室硬过滤。 |
| 展开 | [retrieval.go](../../../internal/postgres/retrieval.go) `Expand`；[sources.go](../../../internal/postgres/sources.go) `GetSource`；[expand.go](../../../internal/postgres/expand.go) `readEntity` / `readEpisode` | 原文及指定版本、陈述、实体、经历、关系、证据可展开；历史分页。GetSource 返回处理状态、派生文本与附件缺口，可直接复用原话入口。 |
| 卡片 | [desk_turn.go](../../../internal/postgres/desk_turn.go) `secretaryCardsTx`；[SecretaryCards.tsx](../../../web/src/components/SecretaryCards.tsx) `SecretaryCards` / `withQuotes`；[SourceSheet.tsx](../../../web/src/components/SourceSheet.tsx) `SourceSheet` | 已有 sources/timeline 卡片与版本化原文链接；目前基于模型 used 别名选条目，日期来自 source 的 recorded_at，至少两条才建 timeline。状态按同来源首条 work_item，不是按陈述/行动精确关联。 |

### 三个消费入口

| 入口 | 真实路径 | 共用程度与缺口 |
|---|---|---|
| 秘书 | [desk_turn.go](../../../internal/postgres/desk_turn.go) `DeskTurn` → `secretaryContextTx` → [desk.go](../../../internal/postgres/desk.go) `deskContextTx` → `memoriesTx(...,true)`；`secretaryPrompt` 再调用 `Recall` | Recall Query 为最近用户问题+当前输入，remember 模式、15 候选/4,000 预算；Context.Objects 为空，即使在事项页也没传项目检索范围。只接受当前 Memory map 里的陈述，并受模型 MemoryKinds/IncludeInferred 限制，未使用 Recall 原文 Summary/Coverage。生成前后 `checkDeskContextTx` 复核依赖/事项，可复用。 |
| 记忆查找 | [RecallSheet.tsx](../../../web/src/components/RecallSheet.tsx) `RecallSheet` → `/v1/memory/recall` → `Store.Recall`；来源点击 → `SourceSheet` → `GetSource` | 已显示召回摘录、版本来源、缺口和游标。不能解析自然时间区间或按实体/性质筛；当前呈现是原文块，不是核对后的事件卡片。 |
| 副手 | [workspace.go](../../../internal/postgres/workspace.go) `Execute` → [run_context.go](../../../internal/postgres/run_context.go) `prepareRunContext` → `Recall`；事务内 [runs.go](../../../internal/postgres/runs.go) `runCommandTx` → `recallTx` | 锁前可选向量，锁内全文/图；准备结果重验版本，只挑 `memoriesTx(...,true)` 当前陈述，另加长期 preference/decision。允许全局及同项目、排除其他项目和 context_exclusions。Brief 文本上限 30,000 bytes，非实际 token；前一输出/手工编辑的派生依赖仍记录。 |

**待审最小共享接口**：在 `internal/memory` 定义内部版本化 `QueryPlan`、证据条目及覆盖信息，在 `internal/postgres` 实现同一 `PlanRecall` / `ExecuteRecallPlan` / `HydrateRecall` 流程。三个调用方只传 query、可信调用上下文、mode、budget；不各写日期/实体 planner。Plan 仅生成条件和候选实体，不能授予权限；执行与 hydrate 使用同一 Store/SQL/graph/依赖检查。锁前准备向量或模型规划，锁内仅执行数据库条件与复核，不增加锁内外部调用。现有 RecallRequest 保持兼容，先由服务端内部适配；未来正式 API 扩展须协调者批准。

这三个名字只标识必需阶段，不要求增加三套对象或替换 Store。现有接口足够的部分直接调用，只有下表的具体缺口需要扩展；函数最终命名与是否拆函数由实现审查决定。

| 待审阶段 | 应复用的现有函数 | 必须扩展的理由 |
|---|---|---|
| PlanRecall | `normalizedBudget`、`memory.SearchTokens`、`deskLocation`；查询 `aliases` / `entity_versions`；模型解析沿用既有 provider、`strictJSON` 和 `reserveModelCost` | 已有函数提供预算、词项、可信时区和模型边界，但没有将自然问题转为表达区间、主体、地点与硬/软范围。需要一个共享条件构造器，不能在三个调用方复制解析。 |
| ExecuteRecallPlan | `Recall` 的锁前向量准备、`recallTx` 的授权候选查询/实际 chunk 摘录、`graphTx` 的有限展开和分页思路 | 保留 SQL/向量/图实现，只扩展条件过滤、结构化首轮与受控补漏。当前单点时间与对象加分不够，不能仅靠改 Query 文本得到严格范围。 |
| HydrateRecall | `readClaim` / `readEntity` / `readEpisode`、`evidenceTx` / `GetSource` / `Expand`、`applicable_claim_versions` / `claim_source_is_current`；`checkDeskContextTx` / `verifyRunTx`、`secretaryCardsTx` | 已有版本、原文、证据、现状读取与前后复核；需要把历史表达保留并连到后续更正/行动状态，输出表达时间与原话 span，避免当前陈述交集及同来源猜状态。可在现有组装函数扩展，无需新存储。 |

当前 imported/desk 抽取通常为 candidate，默认 IncludeInferred=false 还会挡住这些陈述；只有当前 capture 的高置信、逐字直接表达可为 sourced。这是可信程度策略，不能为找回成都旧计划而全局改成已确认或打开所有推测；显式回忆应允许带原话和限定的历史表达进入安全证据包，仍区分表达、转述和推断。

Hydrate 返回“当时表达 + 后续变化 + 当前状态 + 证据版本/定位 + 未决歧义 + 覆盖缺口”，让秘书/副手消费同一证据包，避免再与“当前陈述集合”相交而丢失历史。日常 continue 仍可加入当前适用的长期约束；显式历史回忆不能只看当前约束。上层生成前后复核包内依赖，记录至既有 desk_turns.dependencies / run_dependencies / derived_dependencies。预算为所有召回阶段的总预算，不能每阶段重置。

## 3 数据模型与边界差距

相关表已在 [001_memory.sql](../../../internal/postgres/migrations/001_memory.sql)、[002_workspace.sql](../../../internal/postgres/migrations/002_workspace.sql)、[012_continuity.sql](../../../internal/postgres/migrations/012_continuity.sql)；类型在 [model.go](../../../internal/memory/model.go)、[contracts.go](../../../internal/memory/contracts.go)。

| 对象 | 已有 | 尚缺或须裁定 |
|---|---|---|
| 时间 | Revision 的 ValidTime、ExpressedAt、RecordedAt；[007_temporal.sql](../../../internal/postgres/migrations/007_temporal.sql) `applicable_claim_versions` 区分 change 分组与 correction；ValidAt 是有效时点、KnownAt 是系统当时已知时点 | 自动陈述由 createRecord 写默认未知 valid/expressed，只有 recorded_at=导入时间。source 的表达时间有输入，但秘书 desk 入库没有赋 ExpressedAt。发生时间与适用时间共用 ValidTime，没有独立 event 字段；不能无证据互换。缺表达区间索引/检索，不能把 KnownAt 当“去年说”。 |
| 主体/实体/别名 | 主体实体与 alias；Commit 可写有类型、消歧字段的实体；aliases 已有 lower(alias) 索引 | `rememberTx` 按 name+source_id 匹配，两个来源里的“我”不会自动共用用户主体。地点/参与人不是单独对象；未自动建立 claim→place/person 定位。需有证据的规范对象，别名仅候选，同名保留多个 ID。 |
| 经历与关系 | EpisodeKey/conversation 接入组织与历史成员；Commit 支持有证据关系，graph 预算 | 一个会话可含多件事，当前 conversation 不能等价语义经历；desk 来源未设置 source_contexts，会话关系不自动带入抽取。跨消息“那里/那家火锅”虽可用邻文解释，结果缺明确实体关联及辅助依据链。 |
| 性质、否定、状态 | nature=fact/preference/intention/plan/decision；acquisition、confirmation、qualification；action 状态在 work_items | 自动写 claim 只处理 kind=memory，提示词将愿望分为 idea，因而尚不能保证所有旅行意向形成陈述。confirmed 是认可陈述，不能当完成任务；decision 也不能当已完成。否定/假设当前保留在文本与限定词，不是可检索独立列；“未去/取消/已去”未自动连回原意向或行动。需裁定最小 polarity/modality/事件状态表达，不能添加第二个 TODO 状态库。 |
| 工作室范围 | Claim.Scope 支持 project_id；副手过滤不同项目；Recall.Objects 可加权；事项 context_exclusions | 来源/经历/实体缺统一范围关联与传递规则；自动陈述不继承 desk ThingID 的项目；Recall/秘书没有工作室硬筛。项目改归属、同一资料属于多个工作室、扩展全局时的提示需要明确定义。 |
| 来源升级与历史 | 新来源版 invalidate 旧派生；[013_extracted_source_versions.sql](../../../internal/postgres/migrations/013_extracted_source_versions.sql) `claim_source_is_current` 排除仅旧来源支持的 AI 陈述；迟到抽取被挡；同来源精确相同陈述追加证据 | 新 external_version 按到达递增内部版本，不比较外部版本实际新旧；旧快照晚到可能成为最新版，需连接器版本顺序或显式历史导入规则。替换版本旧原文仍可 historical 展开，不可作为现状。 |
| 纠正与重导入 | [editing.go](../../../internal/postgres/editing.go) `correctTx` 保留修订及纠正来源；claim_key_redirects 将旧精确 key 指向已纠正陈述 | key 基于 Subject+Predicate+Text，跨来源改写/别名/重命名不是相同 key，不能承诺自动阻断所有语义重复。新自动实体解析不能绕过既有 redirect/block；需稳定主体与明示语义纠正关系，冲突候选不能 last-import-wins。 |
| 权限 | 服务端 Scope、record_grants、关系两端授权、摘要按 principal 读取、生成/采纳版本复核、sanitizeItemTx 派生副本保护 | 陈述与原文授权独立：允许模型读陈述不代表允许读原话。新增别名/经历/状态展开也需逐条权限；查询候选、歧义文本、覆盖信息、日志不能泄露不可见对象。Owner 可见来源不等于可放入某模型上下文。 |
| 删除 | deleteRecordsTx 清原文、陈述、片段、派生、归档副本、训练样本、事项中的来源预览及 desk 内容；block-reimport 可保留 source-key/claim-key/record-ID hash | DeleteRequest 区分仅陈述、IncludeSources 和 BlockReimport。仅陈述删除仍允许按原文找回，应向用户说明范围。阻断是标识/精确陈述级，不识别任意跨来源改写；不能宣称“相似内容永不再入”。新索引/关系/卡片缓存需加入同一删除闭包。 |

纠正与真实变化须分开：纠正改变系统对当时表达的理解，真实变化保留前后有效时期；后来取消去年计划仍属于去年表达的查询结果，应标现在已取消，不能因此把它从历史回忆删掉。晚导入 2025 消息的记录时间是 2026，表达时间仍为 2025；系统“2025 当时知道什么”用 KnownAt 时则不能看尚未入库记录。未知时间保留未知，不能补成导入日期。

## 4 统一查询条件草案（版本 1，待审）

该草案是内部结构示意，字段尚未实现。参考时刻与时区由服务端可信设置绑定，固定一次解析；区间采用 `[from,to)`。明确日期可成为硬约束，用户说“大概/好像”则作软线索。身份/授权、删除、明确工作室限制永远不能因零命中而放宽。

```json
{
  "schema_version": 1,
  "purpose": "explicit_recollection",
  "mode": "remember",
  "original_query": "我去年说去成都要干什么来着",
  "anchor": {"now": "2026-10-01T12:00:00+08:00", "timezone": "Asia/Shanghai"},
  "time": {"axis": "expressed", "from": "2025-01-01T00:00:00+08:00", "to": "2026-01-01T00:00:00+08:00", "strength": "soft", "precision": "year", "unknown": "fallback"},
  "subject": {"role": "owner", "entity_ids": [], "resolution": "pending"},
  "entities": [{"role": "place", "mention": "成都", "entity_ids": [], "aliases": [], "resolution": "pending"}],
  "natures": ["intention", "plan"],
  "qualifications": ["asserted", "tentative", "unknown"],
  "workspace": {"project_ids": [], "policy": "global", "allow_global_fallback": false},
  "views": {"historical_expression": true, "current_status_at": "2026-10-01T12:00:00+08:00", "known_at": "2026-10-01T12:00:00+08:00"},
  "budget": {"candidates": 15, "edges": 15, "hops": 1, "tokens": 4000, "fallback_rounds": 2},
  "ambiguities": [],
  "fallback": ["scoped_raw_and_vector", "relax_soft_time_with_disclosure"]
}
```

`strength=soft` 是本报告对回忆年份可能记错的默认提案，白皮书初次精准区间定位仍先执行；需裁定默认严格程度。上例 entity_ids 为空是未解析标识，不代表无条件查询，planner 必须先查服务端用户身份与地点 alias 候选；没能解析主体时返回歧义或用角色证据检索，不假设所有“我”都属于用户。维度可扩展人物、其他实体、属性与有效/发生时间范围；known_at 仅限制系统可知版本，不能替代表达时间。

建议流程：先硬边界→主体/实体候选解析→结构化性质与表达区间筛选；再同边界原文全文/向量补漏。已有明确经历时限定一跳展开，补回同一讨论里没有地名的意向，保留联系的证据。状态核对可以读取查询表达区间**之后**的授权资料，标注“后来”，不把后续完成日期强制筛回去年。零结构化命中不等于没说过；原文 fallback 同样返回可核对出处，并标结构化未完成/时间未知。

计划指纹须包含 schema_version、锚点/时区、条件、预算、范围与版本视图，复用现有游标思路。规划、召回候选与卡片都携带 ID/version；缓存继续依赖相关版本和授权，需验证删除与撤权使计划候选、摘要、开放卡片及后续使用失效。模型 JSON 严格校验；无法解释的条件留 unresolved，不能悄悄丢硬限制。最多一个影响答案的消歧问题或少量候选；日常界面不展示 planner 字段。

## 5 actions JSON 污染：观察与治理提案

**已观察**：[commands.go](../../../internal/postgres/commands.go) `saveAction` 把整个 workspace.Item 序列化为 text/plain 来源，connector=actions，版本=item.Version；秘书 `executeSecretaryActionTx` 收敛一条动作内多次保存后也调用它。`ProcessExtraction` 跳过 actions/corrections/memory-input，但 `ProcessChunks`、`ProcessIndex`、`ProcessEmbedding`、`summarizeTx` 没有 actions 排除。`memory_text` 视图包含所有来源，Recall 可直接匹配 JSON 标题、历史、sources 等，summary 也能摘出 JSON。确认存在路径；污染数量、费用和比例未实测，不能估作已发生的确定金额。

**待审最小治理**：定义来源处理/召回用途策略（raw_audit、knowledge、execution_evidence），集中由同一个策略入口消费，避免各环节各维护 connector 黑名单。actions 原始版本与 action_log 不删，继续承担审计、撤销和行动状态证据；日常记忆召回/向量/摘要不直接使用整份 Item JSON。需要“做完了什么”的回忆时，由行动模块构造简短、有 item/action ID、版本、时间和出处的执行投影，读取最新权威 work_items；新状态不能从 JSON 文本推测。投影是派生视图，沿用现有依赖/删除机制，不是新状态库。

治理顺序：先读路径排除旧 JSON（包括未索引原文 fallback）→ worker 防新增分段/向量/摘要污染并妥善确认既有排队作业 → 按来源/版本重建或清理旧派生索引和摘要，不动原始审计 → 验证任务完成/撤销和相关来源链接仍可追溯。corrections 的 JSON 同样需专用“更正前/后”投影，memory-input 与用户原文区分以防重复证据膨胀；这是扩展建议，不擅自扩大本期实现。删除原始来源时仍执行用户指定范围的删除传播，不能以审计为由保留正文。

## 6 最小任务切分与迁移顺序（待审）

文件列表示建议归属，非已批准的分工。每个共享文件一名写入者；先协调类型/迁移，其他任务只消费。不追加模型适配层、日期依赖或独立队列。

| 任务 | 输入 → 输出、文件边界 | 依赖与验收 |
|---|---|---|
| M1 写入抽取 | 来源 Ref+相邻上下文+现有图 → 主体/地点候选、性质/限定、表达时间、语义经历成员、逐字证据；`memory/model.go` 及新增内部 extraction 类型、`postgres/processing.go` / `claims.go` / `source_context.go` / `connectors.go`；新增迁移由本任务独占 | 协调者先裁定身份/关联/状态/时间规则。保持 source/jobs 同事务、actor=ai、来源升级与纠正优先；新增索引考虑 owner+expressed_at、实体/陈述关联和范围，先查查询计划再定索引。不能直接调用 actor=user 的公开 Commit。 |
| M2 统一 planner | 原句+可信 now/timezone+purpose/workspace+budget → 已校验 QueryPlan、实体候选、歧义、回退顺序；新增 `memory/query_plan.go`、`memory/planner.go`（建议名） | 类型先定稿；日期解析用现有 Go time 与时区设置。模型规划走既有 provider 与预算，确定性规划可先覆盖黄金句和模糊年份；失败仍有原文检索路径。无存储权限与产品动作。 |
| M3 检索执行与上下文 | QueryPlan+Scope → 历史意向/现状/证据/覆盖包；`postgres/retrieval.go` / `graph.go` / `expand.go` / `run_context.go` / `runs.go` / `desk_turn.go`；共享类型只消费 M1/M2 | 等 M1 数据、M2 计划；统一表达区间/范围过滤、原文 fallback、向量补漏、后续核对。保留前后权限/版本检查，移除当前陈述相交限制，预算跨阶段共享。planner 外部调用在锁前。 |
| M4 卡片与原话 | 统一证据包 → 时间轴、当时意向/现在状态、原话定位及覆盖缺口；`workspace/desk.go` / `web/src/domain/desk.ts` / `SecretaryCards.tsx` / `RecallSheet.tsx` / `SourceSheet.tsx` | M3 输出稳定后由一个后端接口所有者与前端执行者对接；以来源表达时间排序，未知日期明确未知，取消/完成有依据，每项点开指定版原话并定位摘录。沿用稳定化后的时区工具。 |
| M5 独立评测 | 人工标注合成集+固定状态序列 → 分阶段指标/错误分类；新增 `postgres/testdata/phase2_memory.json`、`postgres/phase2_*_test.go` 与 `web/tests/phase2-memory.spec.ts`、后续评测报告（建议名） | 评测者不写产品算法，预期从原文标注；先假模型验证解析/状态/权限契约，再经独立授权的默认通道真实评测。测试集冻结早于实现调参，保留盲测部分。 |
| M6 收据治理 | 来源用途策略 → 读路径过滤、worker 停新增与派生清理；`postgres/jobs.go` / `processing.go` / `summaries.go` / `commands.go` 和 M3 检索文件 | M3 与 M1 接口协作，不并行改同文件。原始 actions/action_log 保留；提供完成事件投影，先防污染再回填，撤销行为独立回归。 |

推荐合并顺序：① 批准规则与固定评测集、添加兼容迁移；② 原文层可读与数据用途读过滤，防旧数据污染；③ M1 新写入（功能开关/抽取版本），历史回填另排受预算 jobs，不改用户确认/纠正；④ M2+M3 在三个入口共用，保留旧 Recall 请求；⑤ M4 卡片；⑥ M6 旧派生治理；⑦ M5 独立评测/默认通道黄金路径。回填按 source/version/extractor version 幂等，先抽样检查再扩大；迁移须说明旧数据缺字段时的 fallback，不全库同步调用模型。新关联如不能用现有关系类型准确表达，应加小迁移并协调正式类型，不能把地点关联硬塞成 causes。

最小第一次可审演示：冻结十余条成都旅行资料，含两条跨消息意向、一条同名干扰、一条他人计划、一条后来取消、一条后来完成和一条晚导入；分别从秘书、Recall、副手问同一句，得到相同证据 ID/version 集合和状态。这个演示只是开发里程碑，完整通过仍需下述带干扰评测及真实黄金路径。

## 7 状态序列表（预期提案，尚未执行）

| 序列 | 操作顺序 | 预期与重点观察 |
|---|---|---|
| Q1 相同来源重放 | ingest A/v1 → 抽取/索引 → 同内容同版重放 → 再查询 | 来源 duplicate；陈述/实体/关系/经历不增量重复；费用及 jobs 不重复预留；同版不同内容返回冲突。 |
| Q2 来源升级 | A/v1 表达计划 → A/v2 删除或改写该计划 → v1 worker 迟到 → 当前/历史查询 | 当前不沿用仅 v1 支持的自动陈述，历史原话仍可查；迟到 worker 不提交；重复同一陈述追加 v2 证据。升级来源前后重验输出版本。 |
| Q3 晚导入旧消息 | 先录 2026 已取消 → 后导入表达于 2025 的旧计划 → “去年说”/“现在还去吗” | 旧计划进入去年表达历史，后续取消仍主导现状；导入时刻不能替代表达时间；KnownAt 历史不提前看到后来导入资料。外部旧快照同 ID 迟到列冲突，不默认覆盖新版本。 |
| Q4 用户纠正 | 错抽“张三想去”成“我” → 用户 correction → 原来源升级/同 key 重导入 → 改写版本重导入 | 精确 key 指向纠正版本，原错误仍可审计但不作现状；改写/跨来源未决时不复活错误，记录冲突/覆盖缺口。该语义保护需新实现，现有 hash 不充分。 |
| Q5 撤权 | 生成前授权 → Recall/卡片 → 撤销来源或陈述/关系授权 → 展开/后续秘书/副手；另测生成中撤权 | 不可见文字/引用/别名候选/状态/覆盖信息不泄露；依赖结果不采纳；Owner 视图与模型上下文分别验证。 |
| Q6 删除 | 查询并产生摘要/卡片/副手采纳 → 仅陈述删；另组 includeSources+blockReimport → 原归档/来源重放 | 仅陈述删的原文仍可找回并正确说明范围；来源删传播原文/索引/派生/训练/收据副本，旧卡片重新打开失败且不回放正文；同身份/精确 key 阻断。改 ID 改写不承诺现有阻断。 |
| Q7 索引未完成 | 原文提交 → 暂停 chunk/tokenize/embed/extract → 即刻问 → 逐阶段完成后再问 | 刚写即读能命中原文，必要上下文有界补读，不要求用户等；coverage 标 pending 阶段，不能把零结构化说成没提过。时间未知有说明。 |
| Q8 抽取失败与重试 | 原文提交 → provider 不可用/坏 JSON/DB 失败/超时 → 显式重试或自动恢复 → 租约过期完成 | 原文始终在；无效输出不落图；重试幂等，失去 fencing token 不提交；可能已付费请求不静默重复收费。现有 unavailable 常归 provider_not_configured，评测应区分真实原因。 |
| Q9 模糊时间 | 固定 now/timezone → “去年/前年/大概去年” → 错年无首轮命中 → 受控放宽 → 翻页 | 跨时区年界正确，from/to 半开；软年可放宽且明示实际日期，明确严格区间不得静默放宽；游标沿用锚点，不因跨日重解析。 |
| Q10 零结构化命中 | 没有 claim/无地点关联 → 原文有答案 → 首轮空 → 全文/向量/邻文 fallback | 找回版本化原话，标结构化覆盖缺口；超预算返回继续入口，不宣称全历史完整；不存在则诚实空答。 |
| Q11 同名/别名/指代 | 成都城市与“成都”店名；两个张三；上文地名+下文“那里” → 自动解析 → 纠正一个别名 | 不能同名强合并；需证据的别名匹配，只有同经历且出处明确的间接句归属；未决歧义不污染长期身份。 |
| Q12 范围与行动状态 | A/B 工作室都有旅行 → A 内问 → 明确全局问 → 完成/取消/撤销完成/更换项目 → 重问 | A 硬范围不串 B；允许的全局扩展有说明；状态来自精确行动关联及最新权威记录，不按同 source 的另一件任务猜状态；撤销采纳的强化遗留单列 backlog，不改事实。 |
| Q13 收据噪声 | 多次动作生成 JSON/大 history → 同内容原文查询 → governance 前后比较 → 手工审计/撤销 | 日常结果与摘要不出现原始 JSON，减少无意义 embedding；执行证据投影仍能显示完成/取消；审计/撤销不受派生治理影响。 |

## 8 带干扰项的评测设计与指标（待审）

### 已有证据的界限

[2026-09-29 报告](../../evaluations/2026-09-29.md)、[原始 JSON](../../evaluations/2026-09-29-continuity.json)、[continuity.json](../../../internal/postgres/testdata/continuity.json) 记录：12 条合成资料、5 个查询，必需来源合计 7/7，核对五条生成回答的对象/状态错误为 0。它证明当时指定 ChatGPT 订阅与本地 BGE 向量配置下，这五个样本可以找回所需**来源**、区分 AI 提案及他人状态、显示附件缺口；另外做了纠正后摘要重建。不是七个独立数据集，也不是证据原子级召回率。

查询检索单次为 19–56 ms，含生成为 3.350–8.543 s；真实抽取只处理六个指定来源，JSON 记录单条 7.362–11.482 s、12 条向量阶段总计 247 ms。这些是单次小样本，不是吞吐/P50/P95 基线。请求预算 6,000，候选最多 12；上下文只记录字符/字节，未记录模型实际 token。没有每条费用、每次费用、规模分布、全库索引完成后的召回率、并发吞吐或线上真实用户泛化结果；免费本地 embedding 和订阅通道不能外推商业 API 费用。

[2026-09-30 边界报告](../../evaluations/2026-09-30-memory-boundaries.md) 和现有 `TestSourceReplacementRetainsHistoryAndRefreshesRepeatedEvidence`、`TestRecallReturnsMatchingTailChunk`、`TestRecallGraphHonorsEffectiveAndKnowledgeTime`、`TestRunKeepsRelevantOldMemoryAheadOfRecentNoise`、`TestTemporalChangeCorrectionAndHistoricalKnowledge`、`TestConnectorReplayAndSummary` 等是可复用回归证据（分别位于 extraction_lifecycle_test.go、retrieval_boundaries_test.go、run_retrieval_test.go、replay_test.go、continuity_test.go，均在 [postgres 目录](../../../internal/postgres)）。报告明确未做 broad semantic benchmark、真实账户验收或生产部署。本次没有重新运行，不把旧绿色写成本次通过。

### 新评测集与独立预期

建议先建 12 组小型场景，每组 6–12 条资料、2–3 个问法，约 24–36 个固定查询，额外混入确定性无关噪声；规模是计划，不是已制作/已运行。每条标 source external ID/version、表达/发生/记录时间、角色/分支、范围、gold 主体/地点/性质、逐字证据 rune 定位、同经历关联、后续更正/取消/完成证据与可见主体。查询分别标必需证据集合、禁止证据集合、容许候选/歧义、应显示缺口和允许回退。人工预期先于模型输出；歧义可以以澄清为正确答案，不能强迫系统猜。

| 场景组 | 必须包含的干扰项及正确结果 |
|---|---|
| 成都基础 | 去年用户意向与今年别人的计划；同词但不同性质（报道/假设/不想去）；只返回用户当时想做的事。 |
| 日期记错 | 真记录是前年，用户“大概去年”；先按年查，再放宽提示实际年份；“严格只查去年”则不可放宽。 |
| 同名人/地与别名 | 两个张三；成都城市/成都店名；蓉城需有别名证据；不按名称合并。 |
| 仅上文地名 | 上文“去成都”，下文“想看展/春熙路那家火锅”，另一个分支谈北京；有依据地补回同经历意向，不全会话扩散。 |
| 间接指代 | “到那里再去那家店”，夹入另一个人物和目的地；不确定时给候选，引用原话和上下文联系。 |
| 取消/完成/否定 | 一项考虑、一项决定、一项后来已做、一项后来放弃、“还没做”的否定；历史意向均可显示，当前状态分开，取消不是忘记历史。 |
| 更正与重导入 | 错主体/错地点/错状态被纠正；同来源升级、精确重复、跨来源改写；报告仍未解决的语义冲突，不复活已明确错误。 |
| 历史导入 | 2025 消息在 2026 导入，另有当前取消与历史 branch；表达过滤、系统已知过滤分别验证。明确反例：2025-03 表达、2026-10 导入必须出现在“去年说”的时间轴 2025-03，不能标成 2026-10 或查不到。 |
| 低活跃 | 五年前仅一次意向，十余条近期同词无关资料；明确回忆不按衰减硬排除，检索/展示不强化。 |
| 受限来源 | claim 可见 source 不可见、关系一端不可见、同名不可见实体；模型仅见授权内容，owner 原话展开另行检查；不泄露候选名称和覆盖计数。 |
| 删除重放 | 删 claim 与删 source 两种；已生成摘要/desk/采纳结果/归档；重放旧版与换来源 ID；按删除范围断言，标明 hash 阻断的有限保证。 |
| 工作室边界及 JSON | A/B 同主题、未归项目全局记忆；十余次 actions 收据与撤销；按批准范围查，正常回忆无 JSON，行动状态仍可追溯。明确反例：同来源含“看展”和“吃火锅”，只完成吃火锅；看展必须保持未完成，不能按同 source 第一条事项的 done 状态跟着完成。 |

分三层运行：① 固定假模型/确定性结构化提交验证规划解析、SQL条件、序列/权限、卡片和原文定位；② 小型固定真实模型抽取/规划/生成回放，独立默认通道授权后才执行，记录模型/提示词/plan/extractor 版本及实际费用，失败按抽取→规划→召回→核对→呈现归因；③ 稳定化完成后真实部署跑白皮书黄金路径 3，检验体验。模型模拟不能替代②③；本期 M0 只制定方案。

| 指标 | 计算与记录口径 | 建议阈值与基线 |
|---|---|---|
| 证据召回 | `命中的 gold source/version/span / gold 必需 span`，去重；另列 source 级与 episode 组完整率；记录各阶段增益与 fallback | 固定集证据召回建议 ≥95%，成都黄金组 100%；原有 7/7 仅来源级，span 基线待测。 |
| 错人/错事/错状态 | 分别计不正确主语、经历归属、当时性质与当前完成/取消；分母为回答条目，另计查询全正确率 | 固定硬边界组要求 0 次；全场景暂建议 ≤2% 且逐条复审，样本少时同时公布整数，不用比例掩盖一个严重错例。无新基线。 |
| 权限/删除泄漏 | 检索、候选、歧义、上下文、卡片、原文、摘要、缓存/生成/采纳各出口检查 forbidden span/ID | 0 次，任何泄漏阻断阶段通过；仅陈述删保留的授权原文不误计泄漏。新增图与 planner 无基线。 |
| 覆盖缺口 | 应报告的 pending/失败/未知时间/附件缺失/预算截断命中率，以及错误 complete=true 次数 | 固定状态序列应报告缺口 100%，错误完整声明 0；覆盖只指查询范围与处理步骤，不宣称世界中所有资料已接入。 |
| P50/P95 延迟 | 分写入返回、索引/抽取延迟、规划/执行/hydrate、首卡、含生成总延迟；按无/有向量及 fallback 分组，多轮记录冷热缓存 | 小型本地已索引检索暂议 P95≤500 ms；含默认通道生成先测再定，不能用旧 19–56 ms 五次值作为 P95。完整 history 分页另报。 |
| 实际上下文 token | 记录完整 prompt 输入 token（含计划/历史/约束/原文）与输出，provider 使用量或对应 tokenizer 标明模型版本；字符/字节另列 | 记忆片段目标≤4,000 token、总输入暂议≤8,000；未获实际计数记 unknown，不将现有 b.Tokens×3 或 30,000 bytes 当真实 token；截断保留核心证据和缺口。 |
| 每条资料处理费用 | 原始资料为分母；分抽取/tokenize/embed/summary、窗口数、重试/失败预留与实际账单，分别算新写/回填 | 先建立真实费率及 token 基线，再裁定金额上限；订阅无逐请求账单则费用 unknown，并报告额度/耗时与可算的估计，禁止填 0 代替未知。 |
| 每次查询费用 | 规划、query embedding、生成的总实际使用量/费用；区分 Recall 与含回答三入口，冷/热与回退 | 阈值待测后裁定；建议相同模型/正确证据下与旧流程配对比较，先报成本变化，不先声称结构化省了百分之几。 |
| 吞吐与收据污染 | jobs 完成资料数/秒、积压清空耗时、失败率；actions 在日常候选/摘要的比例及无意义向量调用数 | 本次无吞吐数据，不设生产承诺；固定评测治理后原始 actions JSON 暴露=0，治理不能使合法执行证据消失。 |

## 9 需要先裁定的事项

1. 用户主体如何获得跨来源稳定 ID；导入中的“我”/assistant/转述如何映射；来源内 unknown 实体的兼容回填如何保留旧版本与纠正 key。
2. 地点/人物与陈述的最小关联表达、辅助指代证据及语义经历拆分规则。现有 Relation.Type 没有 located_at/mentions；采用小迁移关系类型或陈述规范引用需统一决定。
3. 意向、否定、计划、完成/放弃如何表示及连到行动；没有 work_item 的历史完成自述如何核对。不得把 memory confirmation 或 nature 当 execution status。
4. “去年”默认硬/软程度、表达时间缺失的 fallback、发生时间与有效时间是否本期分列；年份范围基于服务端用户时区，查询途中固定锚点。来源旧版本晚到如何保护现版本。
5. 工作室默认“本范围+适用全局约束”还是严格本范围；跨工作室扩展的明确条件、多人/多范围资料如何授权，项目换归属是否重建派生范围。
6. actions 的知识可见性与执行投影字段、旧派生清理范围；纠正 JSON 是否同批治理。原文审计保留、用户来源删除传播不得变更。
7. 三入口共用证据包/卡片的最小兼容接口，Scope 与版本仍在服务端复核；实际 token 计数与订阅费用的未知值口径。评测阈值、预算及真实模型验收入口需先定再实现。

以上问题是设计裁定清单，不是要求用户逐项批准代码。由协调者收敛契约后分派执行者；本报告的准备工作不阻塞第一阶段稳定化。

## 10 本次交付与验证记录

改动仅本文件。文件路径、代码符号和相对链接已做静态校验；没有运行产品测试，未生成通过率、费用或延迟实测数据。没有启动服务、数据库、容器或临时后台进程，无需清理运行资源。调查分支 `docs/phase2-readiness`；提交与 PR 由 Git/PR 元数据记录，保持调查基线不随文档提交混淆。
