# 第二阶段 K0 共享契约

2026-10-01；基线 `d8d6fb3efe92f7711c5567440e92af362df7fe77`。本文件是首批隔离实现的可审契约，批准状态由协调记录确认。契约、类型、迁移本身不代表 2.0 已实现或通过。线上 11 项、真机和一天试用门槛保留；本批不合并、不部署。

输入研究来自旧 checkout 的 `docs/tasks/phase2/next-session/{scope,latest-code-audit,external-research-eval,chatgpt-import-audit}.md`；研究不是批准契约。d8 静态缺口、7aae834 上旧 M1 动态证据、本批独立验收分别记账。人工 gold 由 C 独立维护，实现者不改 gold。

## 1 权威与最小范围

继续使用三层共享身份、现有 PostgreSQL、Recall/Expand、claims、record_grants、worker 队列、provider。原件和当时情境是持久上下文层；每轮模型工作上下文是按权限、用途、版本、范围、预算得到的投影。工作室只增加范围，不创建第二事实库。work_items 是执行状态权威。

K0 冻结类型、公开授权和生命周期接口、必要兼容迁移。K1/K2 才接三个真实消费者及来源生命周期；C 独立检验。2.0 不实现新抽取/planner、成都完整时间轴、ChatGPT 导入规模体验；这些属于 2.1。2.1 记忆链路后交付官方 ChatGPT 导出迁入，复用已有 parser/API，历史默认不产生今日事项、提醒或唤醒。

## 2 可信任务上下文与硬范围

`TrustedTaskContext` 只由服务端从认证、当前用户请求、已存事项/工作室和最终 provider 路由生成，不能从请求 JSON、模型输出、检索文本、上传角色/branch 直接解码。包含 owner/principal（使用现有 workspace agent.ID 原值）、role、model、provider、channel、purpose、固定 now/timezone、版本视图、硬范围、统一预算。owner 身份能展开资料，不意味着该资料允许外发。

`ContextPurpose`：`knowledge` 日常模型知识供给，`execution_evidence` 精确行动投影，`raw_audit` owner 审计展开。`IngestPurpose` 预留 `historical_memory_import`、`current_instruction`、`incoming_material`；本批只定义，2.1 消费。上传内容中的命令没有行动或授权权限。

硬范围为 `studio`（精确 studio ID）、`owner_global`（当前用户明确全局请求/已批准可信策略）、`unscoped`（没有工作室的独立任务）。工作室默认仅本工作室成员及**单独标记且经授权、适用于任务的 global_constraint**。`WorkingContext.Objects` 仍是软相关线索；不能替代范围。无 source scope tag 的旧原文是 unscoped；studio 不得自动读取。owner_global 可在 owner 明确范围下检索授权资料，但不改变其权限。跨工作室扩展必须重新绑定可信请求，不能由 planner 放宽。

source 范围以 `source_scope_assignments` 关联同一来源：`studio` 指定一个或多个 studio ID；`global_constraint` 需 owner 明确标记；`unscoped` 是缺少关联时的分类。global_constraint 不是所有全局原文，仍需匹配用途与必要性。自动抽取只可建议关联，不能授予/扩大范围。

## 3 来源授权：公开与自然入口

公开 owner-only `GET/POST/DELETE /v1/memory/sources/{id}/authorization`。POST 授予/更新一个精确接收者 tuple；DELETE 撤销同 tuple（保留有版本的 revoked 行用于并发 fencing）。body 为 `SourceAuthorizationRequest`：source 正版本、expectedPolicyRevision（首次 0）、principal、role、model、provider、channel、purpose、硬范围。GET 返回 owner 可审政策，不泄露给未授权模型。

`policy_id`可选：新建空ID+expected0；更新/缩权用已有PolicyID+expected revision，在同事务核source匹配、route合法和tuple唯一后**替换旧tuple并增加同一policy revision**，不得新建窄tuple而留下宽allow。目标tuple已被别的policy占有返回conflict，不默默合并。DELETE可按policyID定位（必须source匹配，提供tuple时亦核一致）或exact tuple定位；缺行的明确撤权可创建deny revision1。新建tuple已存在（包括revoked tombstone）时不能用expected0覆盖，须GET现revision。自然改范围映射同原子更新，不是两次HTTP。undo对PolicyID+after revision恢复旧tuple/语义并生成新revision。

接收者 tuple 使用已注册 agent.ID、实际 model/provider/protocol/channel 与 route fingerprint，精确匹配；fingerprint 由服务端对规范化 endpoint/配置身份计算，不存密钥，不把可改绑的 provider 名称当身份。无通配默认、新 agent 不回补、新 model/provider/实际路由切换不继承。manual 必须绑定独立 channel/外部接收者，不能用自动通道政策授权。无明确接收者的 manual 包拒绝准备，提示一个必要选择。来源按稳定 source ID 授权，版本升级可沿用相同政策；保存授权时 expected source version 防错对象，生成时必须验证具体 ref.Version。

`record_grants` 继续承担记录粗粒度访问；授权 source 政策可同步增加该来源 grant，多个 active 政策保留 grant 至最后一个撤回。**消费原文同时要求 source grant 和精确 source policy**；单独 claim grant、owner 原话卡片可读、旧 SQL source grant 均不替代 provider 外发许可。claim 正文仍可凭独立claim grant消费，不要求附带原文许可；出处标题/角色/quote/span再验source权限。所有非owner raw出口（GetSource/Recall/Expand/summary/消费者）必须有服务端绑定 `Scope.Task`，缺失即 fail closed（missing_trusted_task），客户端JSON不能提供；owner原件审计展开不代表模型外发。旧无政策资料拒绝供给并报可公开缺口，不迁成全角色可读，候选/coverage不泄漏不可见名称或计数。

**显式deny优先**：当前owner明确“别再用《唯一资料标题》”或撤回来源使用，默认含该接收者/purpose/hard scope的来源及其证据派生claim/summary/历史产物。`source_authorizations.revoked` tombstone表达拒绝；未曾授权也可首次撤回创建revision1 deny。没有policy行不阻断独立claim授权的正对照；有匹配deny时，claim verifier检查每条evidence来源而不要求来源allow，任一受禁来源使该条派生内容拒绝（不擅自剥去证据继续供给）。raw仍要求allow。regrant增加revision，仅允许新生成，不恢复已invalidated尝试/产物，不让用户辨析raw/claim。来源owner原件不删除，其他接收者独立授权不扩大。

K1必要小修（协调批准）：023增加explicit_deny，现revoked回填true且deny⇒revoked。普通撤权revoked=true/explicit_deny=true；**撤销首次grant**恢复此前absence语义，保留revoked=true/explicit_deny=false的单调revision骨架，避免把原来独立claim可读变成用户从未表达的deny。sourcePolicyDeniedTx只认revoked且explicit_deny；raw仍要求!revoked，旧attempt/产物仍invalidated不复活。这只恢复原授权语义，不放宽显式deny。该内部恢复只能由undo可信context产生，不能由客户端request字段提交。Result新增action_id/undoable供真实action日志回执。

自然入口首批用有限完整句：“让秘书能用《唯一资料标题》”“允许这个副手读取《唯一资料标题》”“不再让秘书读取《唯一资料标题》”“别再用《唯一资料标题》”（当前选中接收者）。沿用秘书当前请求与动作事务。deterministic helper只解析当前owner req.Text，精确唯一标题映射owner当前source；秘书/这个副手映射服务端selected agent，命名副手须唯一。问句、假设、引号整句、转述、否定授权不自动匹配；来源正文、历史对话、模型输出、导入元数据不参加解析。缺少明确source selection的“这份资料”返回一个必要选择，不猜最近source。若未来接模型动作，只接受当前owner可见S*/A*别名且仍须当前请求独立意图守门；本批不依赖模型op授权。服务端绑定tuple/范围、检查version/revision/路由，直接短回执；同轮不偷用此前未授权原文，下一轮重新装配。2.0必须真实policy undo接入，接入前不得声称Undoable。

`SourceAuthorizer` 是可选新interface，不扩现有Sources/stub。定义SourceAuthorizations和SetSourceAuthorization；撤回用同请求Revoke=true，HTTP DELETE映射此入口。请求带request_id；相同ID/相同body返回已有回执，相同ID不同body冲突；重放复核source/policy及原expected revision+1，已有后继/撤权返回conflict，不重授资料。沿用既有workspace_commands幂等账本；即使账本以后清理，持续单调policy/scope revision仍阻止旧expected=0重授。B提供mutateSourceAuthorizationTx、sourcePolicyAllowsTx、sourcePolicyDeniedTx及resolveSourceAuthorizationIntentTx；A入口调用同helper。父文件/归档/OCR派生是独立source，不从父授权外推子授权。

范围关联 owner `GET/PUT /v1/memory/sources/{id}/scope`，`SourceScopeEditor` 定义 `SourceScope` / `SetSourceScope`；写请求含 request_id、exact source ref、expected scope revision、完整 assignments。首次 revision=0；替换后 revision单调加一、同请求幂等异文冲突。studio 必须同owner现存project；global_constraint必须当前owner明确操作，不从抽取推断。范围缩小与typed失效同事务，扩大仍不增加接收者政策。缺旧assignment按unscoped解释；首次显式写空集合等于unscoped，不等于global。

2.0授权/撤权/范围变更必须接入现有action undo，保存政策/范围前后revision及recipient tuple。撤销检查当前after revision及后继，恢复此前语义并生成新revision，不能倒退revision或恢复已删来源；授权被撤销即触发相同typed失效。K0冻结此要求，K1 A actions_log.go+B mutation协作接入；接入前不发假Undoable，不把“可以再说撤权”当已满足撤销验收。

撤权按owner可见的已存tuple+expected revision执行，即使旧agent已禁用/旧provider改绑也可撤回；新授权及undo恢复grant须核当前实际route、source与scope，不能先恢复旧配置才能撤权。manual共享Command增 `ManualRecipient` 表示当前owner选择，服务端必须重建/核实实际destination再绑定可信Task，不能把客户端任意tuple直接当可信。

## 4 Typed hydrate 与统一验证

`Ref` 必须保留 kind、ID、正 version。span 使用 UTF-8 文本 **rune 半开区间 [start,end)**，先检查范围再逐字核对；最终载荷位置另用 UTF-8 byte 半开区间，不混用。历史 v1 不得被 hydrate 成当前 v2。source 可以在零 claim/pending extract 时供给已保存可读纯文本；OCR/transcript pending 不代表原件内容已可读。

| kind | 2.0 行为 |
|---|---|
| source | exact source_versions+record_versions，验证 active/删除、grant、policy、scope、purpose、视图、正文可读，保留 version/span/角色/表达时间/缺口 |
| claim | 沿用 applicable_claim_versions、record grant、范围、nature/confirmation/exclusions，逐evidence来源查explicit deny；无source policy不阻独立claim；补入原话/出处另须source allow |
| summary | exact derived_views 版本、非 stale；递归复核每条 typed 依赖的所有边界，防止旧摘要绕过 actions/policy；循环/缺依赖拒绝 |
| chunk | 不作为最终独立证据；核对 exact chunk→source version 和合法 rune span，归一成 source 证据和依赖；不能把旧 chunk 接到新 source |
| entity/episode/relation/未知 | 本批不供给模型正文；显式 unsupported_kind 缺口，不伪造成 claim/source。将来有完整 verifier 后再开放 |

summary 内容依赖出现未支持 kind 则本批拒绝该 summary。Recall.Summary 是临时摘录，不能当 versioned summary，无独立依赖不得直接拼入。现有实体/关系仍可参与内部定位，但不能旁路向模型输出未经验证名称/事实。2.0 summary 返回可用能力以 K1 接入及 C 动态验证为准。

`EvidenceEntry` 保存 typed ref、source exact ref/span、角色、表达时间、历史/已更改标记、供给片段、依赖和 gaps。`TypedDependency` 保存 ref、purpose、硬范围；summary/历史回答/Brief/产物递归依赖不能以本轮直接输入冒充。current 必须当前合法版本；history 只在明确合法历史目的且原版本仍有权限时读，并标记已更改。删除/撤权永远不被 history 放宽。

同一verifier在生成前、dispatch reservation、adapter前再次fence、生成后、采纳前、manual重取、下一轮历史回放使用。精确签名在§8冻结，B不私写第二verifier。旧artifact restore不得固定kind=claim；旧依赖从memory_records.kind核实回填，缺失/未知拒绝。

actions 原始 JSON、corrections 原始 JSON、memory-input 不进入 knowledge raw/summary 通道；raw_audit owner 原件可查。execution_evidence 只读精确 work_item/action ID、版本与权威状态投影。本批先封知识入口和递归摘要，完整 worker/旧索引治理后续分派。

## 5 四种记录与最终边界

| 集合 | 精确含义 |
|---|---|
| candidates | 已授权检索返回的 ref/span/阶段/保留或拒绝原因，不复制候选正文，不承诺全 SQL 候选或整个库完整 |
| input | 最终装配、模板、schema/tools 配置和截断之后实际提供的材料及 byte 映射；adapter 参数与可观测序列化请求分别标层 |
| Used | 输出声称使用的 ref；验证在实际输入、权限和引用支持中的状态；Used 空不代表没收到，存在不证明内部因果 |
| indirect_dependencies | 本轮派生摘要、历史回答、Brief、产物的依赖，用于失效/删除，不代表其原文逐字进入 input |

`ContextManifest` v1 关联 attempt、request/turn/run 身份、recipient、固定视图/范围、四集合、coverage、截断、输入 bytes、tokens（actual/estimated/unknown）、观测层。没有计量时 token 指针为空，不能把 bytes×比例写 actual。provider 内部隐含提示/工具结果不可见部分记 unknown。

最后装配点只保存一份精确载荷字节快照（文本消息/角色/顺序/schema/配置；不复制附件 blob）；材料映射可指到多个 input span。超单次上限必须**先裁剪最终载荷并同步映射/缺口，或拒绝调用**；禁止送全文却存一部分声称全文。hash/refs 仅定位，不能替代精确正文证据。普通日志不含正文、文件名、SQL值。

manual 在准备前完成同样验证并持久 attempt；`GET /v1/workspace/runs/{id}/package`（K1 新入口）在 owner 读取时逐次复核，成功 HTTP 返回记 PCAS delivered，客户端复制无回执时不声称复制成功。原 State/Snapshot/导出中的 Brief 也须 sanitize，不能绕过 package。外部模型是否收到一律 unknown，除非新增可信回执；提交 manual 结果/采纳重新验证。路径以 K1 路由接入确认，不宣称 d8 已存在。

## 6 在途顺序、锁与独立 attempt

1. embedding/规划在锁外。短事务取得既有 owner gate，读取准确版本/政策/范围并构造可信 dependencies；不要在 owner gate 内调用 provider。
2. attempt prepared 与输入快照在独立事务持久，再取得 owner gate 复核 typed deps；authorization 与 source mutation 使用同一 gate。记录 `dispatch_reserved_at` 并提交。adapter实际调用前再短事务fence检查当前typed依赖/政策revision和attempt未失效；reservation后的已知撤权/删除必须取消，prepared→未发barrier可以独立测试。许可只适用于本次立即调用，不可恢复/重试复用旧reservation。reservation是发送许可，**不是网络已发送或收到**。
3. 若撤权/删除/更正在 reservation 前提交，本次不得调用 provider。若 reservation 先提交，随后的撤权可以立即提交；已获发送许可或已发的请求可能无法收回，但返回内容必须失效，不得采纳/历史回放。reservation→真实网络调用的微小窗口如无传输回执只能记 unknown，不能声称精确远端接收顺序。
4. provider 无 owner/source 长事务锁。Desk 的会话顺序锁只保护同会话保序，source mutation 不取它；需要把当前 Desk provider 所在事务中的 owner 写/锁移出，否则不满足 barrier 测试。有限 DB lock_timeout 超时失败，不通过无限锁等待延后撤权。
5. 生成后短事务重验并提交业务结果/持久产物 dependencies；采纳再验。来源 mutation 从 typed reverse deps 递归失效 summary、run、desk、manual、artifact、training、attempt 快照；source DELETE 清正文，撤权清依赖外发快照正文并禁止复供，纠正阻断 current/回放，合法历史单独授权读。

attempt 使用独立生成 UUID 与 request/operation ID、ordinal，持久层不 FK 到 source/claim/desk/run/workspace_owners，也不在独立事务读主事务锁住的行，避免 FK 等待/死锁。诊断容量使用独立 owner 级 advisory gate；不能让调用方持有该 gate 后等待独立 attempt 写。所有短事务 lock 顺序固定 owner gate→diagnostic gate；不跨 provider 保持。必要 refs 在独立写前由服务端传入，dispatch 前再次验证闭合竞争。

attempt 状态 `prepared/dispatched/completed/failed/outcome_unknown/invalidated`；每次真实重试新 ordinal，prepare/dedup 不等于 dispatch。`dispatched_at` 只在适配器观察到 send attempt 时写，外部 received 另为 `unknown/acknowledged`。响应确认不等于全文模型上下文可见。回滚/fallback 保留已尝试骨架；崩溃留下 reservation/prepared，恢复标 outcome_unknown，不能自动当未付费重复发送。幂等键 `(owner,operation,ordinal)`。模型/网络超时和业务状态分开记账。

## 7 保留、容量与删除闭包

本批工程初值（经协调选择，**非测量/非产品性能承诺**）：正文7天、每owner64MiB、每attempt256KiB；无正文骨架30天、每owner10000条及metadata总64MiB并取严、每attempt全部逻辑诊断metadata64KiB（manifest、recipient、attempt依赖旁表及其余可变字段均计入）。metadata先裁剪可省候选诊断并标coverage，不能去掉核心input映射或typed deps后继续称完整；核心装不下即拒绝外发。期限保存为明确 `body_expires_at/metadata_expires_at`，清理按显式服务端时间执行，测试无需真实sleep。预算参考候选15/边15/一跳/记忆4000与总输入8000 token仍待测，不冒充精确计量。

新 attempt 先在诊断 gate 下清到期记录/正文，再回收最早诊断正文以腾出字节，保留候选 refs/映射并标 `capacity_omitted`；超单次载荷通过最终裁剪或拒绝解决。正文保留状态 `retained/expired/deleted/revoked/capacity_omitted` 与传输状态独立。骨架容量不足则拒绝外发（record_capacity），不能默默跳过 attempt；保留失败给清楚恢复提示。并发 quota 检查与写入原子，删除/到期同步释放计量。

长期metadata中的stage/reason/validation/gaps/error_code只使用受控code及无正文计数；禁止夹带原文、来源标题、自由用户文本或provider错误response。metadata_bytes计量所有可变逻辑诊断字段的UTF-8/规范JSON序列化字节，包括manifest、recipient、operation标识/状态codes及每条attempt typed dependency旁表，不得只限manifest。依赖写/删及错误/状态更新同诊断gate下重算，owner汇总metadata_bytes≤64MiB；durable产物依赖随产物另计。PG行/索引/WAL/备份物理开销另测，不宣称DB总占用64MiB。单manifest DDL限值仅附加防线，writer不得相信客户端metadata自报值。

到期清除只是诊断正文丢失，不改变发送事实、权限或事实版本；hash 不能证明仍有可审完整文本。30 天骨架到期可以删除 attempt，但**活着的回答/Brief/summary/artifact 的 durable typed deps 随产物生命周期保留**，不得 FK cascade 从 attempt 删除这些 deps。即使所有诊断记录过期，纠正/撤权/删除仍能找出存活产物。K1提供确定worker周期入口（启动恢复后立即扫、正常最长一小时批量清理）；所有诊断读先检查expires，不能等待下一次写才隐去到期正文；物理清理延迟如实记账。

source删除：原文、切块、向量、OCR/transcript/archive副本及所有派生、attempt快照/手动包/下载缓存清理，留无正文最小阻断/骨架。仅删除claim且保留source：不能清掉仍有独立政策的原话；依赖该claim的产物仍失效。source撤权包括source→evidence.target(claim等)→summary/产物依赖的接收者闭包，清其诊断快照/派生供给，不扩大为owner删除原件。regrant不复活旧invalidated副本。更正使旧诊断成为受限历史，禁止current与自动回放；不重标成当前。备份/附件沿既有清理，不以诊断例外永久留正文。

同来源身份/精确 key 重导入阻断是当前保证边界；改 ID、跨来源语义改写不声明现有绝对保证。迟到 worker 使用现有 fencing/version checks，不能重建已删除/已失效材料。

## 8 共享存储与交接

迁移 `022_phase2_context_contract.sql` 只添加：source_scope_assignments、source_authorizations（tuple、revision、revoked）；context_attempts 和 attempt_typed_dependencies（独立骨架/快照）；context_artifact_dependencies（存活产物 durable typed deps）；既有 run_dependencies/derived_dependencies 加并核实 kind。保留现有 legacy 字段兼容运行；无政策、无范围、缺 kind 的旧数据不能默认授权。迁移不触发模型/全库收费整理。

K1 A 实现 typed verifier、最终装配/捕获、attempt写入/清理和 `invalidateTypedContextTx(ctx,tx,Scope,ContextInvalidation)`；请求含 RecordIDs、Reason（corrected/replaced/deleted/revoked/scope_changed）、可选Recipient。B在现有source replace、correct/delete/revoke/范围更改事务调用同hook。撤权精确recipient只清其诊断/派生供给；若暂未精确实现，可保守使该source所有派生供给失效/清快照，不删owner原件，其他仍有授权recipient可重新生成。source删除/替换不能用recipient过滤保存依赖副本。hook递归处理summary并清诊断正文；B不私写A消费者布局。

冻结的内部函数签名（K1/K2实现，不是K0空stub）：

```go
// B；free functions只读tx，不调用模型/网络。
func sourcePolicyAllowsTx(ctx context.Context, tx pgx.Tx, task memory.TrustedTaskContext, source memory.Ref) error
func sourcePolicyDeniedTx(ctx context.Context, tx pgx.Tx, task memory.TrustedTaskContext, source memory.Ref) (bool, error)
func (s *Store) mutateSourceAuthorizationTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.SourceAuthorizationRequest) (memory.SourceAuthorizationResult, error)
func (s *Store) mutateSourceScopeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.SourceScopeRequest) (memory.SourceScopeResult, error)
// matched=false：不执行授权，可由A给一个必要选择；只有当前owner原句参加解析。
func (s *Store) resolveSourceAuthorizationIntentTx(ctx context.Context, tx pgx.Tx, ownerScope memory.Scope, currentUserText string, serverTarget memory.TrustedTaskContext) (memory.SourceAuthorizationRequest, bool, error)

// A；同tx/source owner gate下验证/失效，不调用模型/网络。
func verifyTypedContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, task memory.TrustedTaskContext, dependencies []memory.TypedDependency) error
func hydrateTypedContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, task memory.TrustedTaskContext, refs []memory.Ref) ([]memory.EvidenceEntry, memory.Coverage, error)
func invalidateTypedContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.ContextInvalidation) error
```

helper不能自行取得长期provider锁；caller遵守owner gate→diagnostic gate。mutation在已有事务中由caller负责request幂等、action log begin/flush及workspace revision，公开wrapper复用该协议。policy/范围undo必须与typed失效同事务，旧路由禁用不妨碍撤回，但恢复grant要验当前route。

K1实际接收者共同helper（A实现、B复用）：`Store.contextRecipientTx(ctx,tx,scope,agentID,role,manual)`与`Store.trustedTaskContextTx(ctx,tx,scope,agentID,role,hardScope,manual)`。role为secretary/deputy/manual；自动读取server registry实际model/protocol/endpoint，manual用owner选择的已配置Provider重建并核对其字段，不信任client fingerprint。subscription读取本地选定model/account身份，不在锁内联网选model；未能确定实际model拒绝原文权限绑定。`sourcePolicyRevokeIsExplicit(ctx)`仅undo内部absence恢复返回false。`recordSourcePolicyActionTx(ctx,tx,scope,before *SourceAuthorization,after SourceAuthorization)`、`recordSourceScopeActionTx(ctx,tx,scope,before,after SourceScopeResult)`由B mutation调用，记录到已开始的既有action buffer，A undo校验after revision和后继。

协调已扩A所有权到workspace.Command/model、commands.go、desk_actions.go、desk_schema.go、actions_log.go（限policy undo）、相关prompt；A写新的manual handler，B只在server.go注册route。首批自然入口使用deterministic helper，无需模型动作作为必要路径。K0只增加共享Command payload，不在消费者接入；K1开工前给冻结提交。任何契约调整先通知协调与C，不能为通过既有gold缩减安全断言。
