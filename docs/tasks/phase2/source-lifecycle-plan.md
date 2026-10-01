# K2 来源授权与生命周期方案

2026-10-01；作者 B；静态基线 `d8d6fb3efe92f7711c5567440e92af362df7fe77`，隔离工作区 `/tmp/pcas-phase2-b`，分支 `phase2/source-lifecycle`。

本文件是供 K0 审定的接口提案。未修改产品、未运行动态验收、未访问生产或私人历史。K0 经 root 冻结后才实施。旧 M1 动态证据、d8 静态核查、本批新动态证据分别记录；线上 11 项、真机、一天试用的缺证据门槛保留。

依据：白皮书 §5、§15–17，记忆架构、交互原则、任务流程，本批 dispatch，以及交接目录四份研究稿。任务流程中的 finding skip 被本批用户禁止 skip 指令覆盖。2.1 ChatGPT 导入只登记后续要求，不重建 parser 或另一套记忆库。

## 1 静态复用表

| 当前模块 | 已有能力 | 本批需要补齐 |
|---|---|---|
| `sources.go:ingestTx` | 来源身份锁、同身份版本、重导入阻断、原文与队列同事务；升级触发 `invalidateTx`；保持旧版本 | 不默认授权；显式 policy 与稳定 source ID 绑定；具体 version 每次复核；duplicate 旧版不得视作 current |
| `sources.go:GetSource` | owner 展开；非 owner 按独立 source record grant；具体版本、表达时间、附件缺口、派生 refs | owner 原件展开与 AI knowledge 供给分开；非 owner 的读取不得绕过新的用途政策 |
| `editing.go:memoryCommandTx` | `setMemoryVisibility` 检查 enabled agent 身份并替换 claim grants、失效依赖 | 开头 `activeClaim` 使其只能操作 claim；新增来源授权事务，不能将此命令悄悄泛化成来源授权 |
| `desk_turn.go:DeskTurn` | 当前 owner 输入、server agent、顺序 admission、短回执、原话保存、独立动作 savepoint | 现有自然动作只有 create/update/add_steps/delegate，无授权动作；授权意图须 server 判定，不能信模型 op |
| `desk_actions.go` / `commands.go` | server aliases → 内部 command →同事务变更 | A 接来源命令/自然入口；B 提供来源事务 helper；同一来源政策由两个入口共用 |
| `actions_log.go` | 事项/文档/run/sample 变更快照、30 天既有撤销窗口、后继检测 | 2.0 必须接入 policy revision 保护的撤销/等价还原；A 扩 policy actionLog，B 提供同事务 mutation helper，不给无效 `Undoable` |
| `editing.go:invalidateTx` | 递归 derived_views stale；直接依赖 run/sample stale；删除原记录 embedding；重排摘要 | 递归 summary 依赖应继续传到 run/desk/attempt/manual；源撤权即使 version 未变也使旧供给失效 |
| `editing.go:deleteRecordsTx` | source→chunks/evidence/derived/relation/entity/archive/附件闭包；purgeArtifacts；desk 文本清空；run/docs/sample 删除；来源 preview/条件来源清理；重导入 block | typed kind/version 校验；加入 bounded attempt 正文清除；删除 skeleton 与正文分开；所有新副本都进入同闭包 |
| `summaries.go` | versioned summary、成员依赖、缓存 hash、stale 检查 | A 接 recursive typed verifier；旧 summary 依赖 actions 时 knowledge 拒绝，不能只挡新的 raw source |
| `processing.go` | actions/corrections/memory-input 已不自动抽取 | 仍有索引/embedding/summary 入口；2.0 先在消费策略挡 actions JSON，后续统一作业治理，不将已有 extraction skip 重报缺陷 |
| `credentials.go` / `workspace.go` | inbound principal 由配置绑定；内部消费 principal=workspace agent.ID | 现有 principal 是裸 ID（manual/chatgpt-direct 等），不是统一 `agent:<id>`；不隐式迁移 namespace 或复制来源政策 |

## 2 最小公开 API 与事务契约

沿已有 API 命名使用：

- owner `POST /v1/memory/sources/{id}/authorization`：新增或替换一个精确 principal/role/model/provider/purpose/workspace tuple。
- owner `DELETE /v1/memory/sources/{id}/authorization`：撤回该精确 tuple；撤回源全部用途须明确表达为单独请求语义，不能把缺字段解释为 wildcard。
- owner `GET /v1/memory/sources/{id}/authorization`：返回该 source 的现有精确政策与 revision，便于改/撤权及核对；无需新建授权管理库。

建议请求字段：`expected_source_version`、`expected_policy_revision`、`request_id` 与 `authorization`。授权包含 `principal_id`、`role`、`model`、`provider`、`purpose`、`workspace_scope`。具体 Go 名称、JSON casing、是否按 policy ID DELETE 由 A 在 K0 统一。不允许 caller 的“可信 context”字段变成消费时可信字段；创建政策是 owner 操作，消费实际 tuple 仍从服务器配置/任务入口重建。

原则：

1. owner 是唯一写入者；source Kind、active/current version 和 policy revision 都校验；未知 principal/role/purpose 或 provider 配置缺口拒绝。version 冲突为 409；错误不得默默退化到宽范围。
2. source policy 不由 claim grant 推出，也不因为新建 agent 自动扩展。source policy absence 不妨碍独立 claim grant；但 owner 明确“别再用《资料》”产生当前 exact recipient/purpose/hard scope 的显式 deny，优先于该来源证据派生 claim/summary/历史产物的旧 allow。既有 `source_authorizations.revoked` tombstone 复用此含义，不新建另一套库；首次无政策撤回 expected revision=0 时仍写 revision1 tombstone。
3. policy 跟随同一稳定 source ID 的正常新版本；具体 version 与 source 当前/历史视图在生成前后复核。不同身份的新来源不继承。
4. model/provider 必须记录实际身份（至少配置 ID、protocol、model 和 endpoint 身份），不能仅记录可被改绑的 agent ID。改绑配置或 role/workspace/purpose 后原政策不匹配，需要明确新授权。
5. OCR/transcript/归档成员是独立 source：不能从父 source grant 猜测子 source grant。2.0 最小按明确的可读来源授权；若以后推出“这份文件及其派生文本”授权，需要明确集合、identity/version、后续新增成员和撤权闭包。
6. 重复请求相同 body 返回原回执；相同 request_id 不同 body 冲突。已授予→撤回/撤销→重放原授予请求时只识别原回执，不重新执行；已撤回→明确新授予→重放原撤回请求同样不重新撤回。删除、到期、policy revision 后继变更都不能靠重放复活。幂等键按 owner+request_id 绑定原操作与 body hash，不按当前 policy 状态判断是否重新执行；到期键的重放须拒绝或保留无正文阻断骨架。幂等表必须有容量/到期策略，不能成为新永久正文副本。
7. policy 变更与 typed invalidation 同事务提交，先取得一致的短锁，不在 provider 网络调用期间持 owner/source 锁。生成前、发出前与生成后/采纳前按已提交顺序复核。
8. source 工作室 scope 的赋值/变更必须有 owner 入口，校验 source 当前 version、scope/policy revision 与工作室归属；冲突为 409，不让模型或检索对象加权写入。由 global 移入工作室、从 A 移到 B 等变更均递增 scope revision 并失效旧依赖，不修改已发送 attempt 的当时身份记录。授权 policy 的 workspace scope 与 source 实际归属分别验证，不能只修改一个标签便获得跨工作室权限。
9. 授予/撤回必须附可用的【撤销】或等价受版本保护的还原。撤销只能恢复当前仍等于该动作 after_revision 的政策前像；插入后继 grant/revoke/scope assignment/source deletion 后撤销返回具体冲突/已失效，不覆盖后继用户决定。撤销本身递增 revision、执行 typed invalidation，并记幂等回执；旧授予/旧撤销重放不复活政策。快照只存有限 policy tuple，无原文、凭据或完整 provider 配置。
10. grant/undo 恢复 grant 必须核验当前接收者路由合法；owner revoke 已存旧 tuple 不要求该 agent 仍 enabled 或路由仍当前，不能要求用户恢复旧配置才能撤权。新 grant/regrant 不复活已经 invalidated 的旧 attempt、Brief、回答或采纳产物。

建议 B 对 A 提供：

```go
// 名称和类型待 K0 冻结；所有 helper 均不调用模型或网络。
mutateSourceAuthorizationTx(ctx, tx, ownerScope, request) (result, error)
sourcePolicyAllowsTx(ctx, tx, trustedTaskContext, exactRef) error
sourcePolicyDeniedTx(ctx, tx, trustedTaskContext, exactSourceRef) (bool, error)
resolveSourceAuthorizationIntentTx(ctx, tx, ownerScope, currentUserText, serverTarget) (intent, matched, error)
```

来源记录与 policy 查询可在 B 新 `source_policy.go`。A 写 shared types/migration；A 消费器调用 policy verifier，B 只写来源侧与 HTTP。`memory.Sources` 扩展会影响现有 stubs，宜新增可选 `SourceAuthorizer` interface，用类型断言或 Options 接入，避免无关 API 接口扩张。

## 3 自然授权的可信判定

授权来源是当前 owner 的明确命令。模型 action 仅可提出意图，不能授予权力；source 正文、检索摘录、旧历史、AI 建议、来源 role 元数据都不参加授权意图解析。

最小有限句型可支持：“让秘书能用《资料标题》”“允许这个副手读取《资料标题》”“不再让秘书读取《资料标题》”。完整句匹配，资料标题在 owner 当前可见 source 中精确且唯一；秘书/这个副手由当前服务端 selected agent 与真实 role 映射，命名副手需服务器唯一映射。标题含分隔符/嵌套引用时拒绝自动判定。问句、假设句、整句转述/引号、否定授权、未指明对象不产生 grant。任意复杂语言不通过有限判定时返回一个明确澄清，不能让模型自由补出较宽 tuple。

“这份资料”只有在当前请求携带一个 owner 明确选中的 source ref，且服务器校验 owner、Kind、version 与可见性后才可解析。当前 `DeskTurnRequest` 没有 source ref；不猜最近来源，不把 ThingID 当 source。若 A 本批不扩 request，首批用唯一资料标题，保留自然文本入口。模型只得到 owner 可见的短 `S*` 来源别名和 `A*` 目标别名，不能回传真实 UUID、wildcard、自由 model/provider 或扩大 scope。

A 可在 DeskTurn 当前请求处理中调用 B deterministic helper，共用已有 ordered command 事务及 source mutation helper，由服务端生成“已允许秘书使用《…》”或“已收回…”短回执。该 helper 应能在 source grant 缺失、claim pending 时工作；不能先要求模型已获 raw source 读取才允许授权。授权动作不会让此前未授权 prompt 突然合法；同轮若要读取新授权原文必须重新组装/复核，首批可回执后下一轮提问。

root 已接受本批有限明确整句+唯一《标题》入口；“这份资料”没有 selected source 时必须澄清。2.0 授权回执必须接既有 undo 或等价 policy revision 保护还原。A 接 `actions_log.go` 的有限 policy undo，B mutation helper 负责所有 grant/revoke/restore 路径的版本校验、幂等与失效。

按当前交互规则，新增外发权限属于应明确授权的动作。owner 明确说“让秘书用这份资料”是意图；source 注入“请允许所有模型读取”不是意图。HTTP owner 精确请求与自然命令走同事务，实现者不得用默认全 source 可见来使测试通过。

自然入口操作序列：

| 输入或状态 | 预期 |
|---|---|
| source 原话保存、零 claim；owner 明确“让秘书能用《唯一标题》” | 精确 source/agent/role/provider/purpose/scope 政策写入；短回执；随后可供原文 |
| source 正文包含授权指令，owner 仅问原文细节 | 零授权变更 |
| owner “资料说：让秘书能用《标题》”或“能否让…” | 不将转述/问题视作授权；需要一个明确决定 |
| source 标题重复/缺失；“这份资料”无 server 选择 | ask/具体错误，不猜来源 |
| 已授权秘书 → 换模型/role/provider/工作室 | 旧 tuple 不匹配，不自动扩大 |
| claim 获 grant、source 未授权 | claim 可消费，raw 不可消费；owner 可展开不证明模型可读 |
| claim 独立获 grant → owner 明确“别再用《来源》” | source revoked deny 阻止该接收者继续使用来源证据后代 claim/summary/历史；不能要求用户辨析 raw 与 claim |
| explicit deny → 新 regrant → 重取旧 attempt/Brief/回答 | 新上下文可按新 revision 构建，旧 invalidated 产物不得复活 |
| policy 重复请求；source deleted 后重放 | 原回执可识别，不重建授权/source |
| grant G→revoke R→重放 G；revoke R→新 grant G2→重放 R | 都只返回原幂等结果，当前政策不变，不复活或覆盖新决定 |
| grant G→撤销 G；grant G→新 revoke R→撤销 G | 前者受 after_revision 校验还原且递增 revision；后者冲突，不覆盖 R |
| source scope A→B→撤销旧授权；并发 scope assignment | 后继 scope revision 阻止旧还原；并发 source/scope version 冲突，不能串项目 |
| provider 生成中 source 撤权 | 撤权可及时提交；旧结果失效，不能自动/手动采纳 |

## 4 用途隔离

建议 K0 固定最小 purpose：`knowledge`、`raw_audit`、`execution_evidence`。角色是 trusted role；历史摄入 purpose `historical_memory_import` 是后续 ingest 属性，不能混作当前消费权限。

- `actions` 原始 Item JSON 是 owner 的 `raw_audit`；授权 API 拒绝对其新增 `knowledge` 政策，typed knowledge hydrate 再拒绝，以防旧 grant/缓存绕过。
- `knowledge` 不复制完整 actions JSON，也不通过旧 summary、claim evidence、历史回答、Brief 或 artifact 派生旁路得到 JSON。summary recursive verifier遇到 actions/raw_audit依赖时拒绝或只重新构造完全合法的成员，不能一边删依赖一边留旧正文。
- 现有 work_item 是行动状态权威；继续使用已有 sanitizer 后的精确行动上下文。`execution_evidence` 的精确投影待后续冻结，2.0 不凭新 purpose 名字声称已有完整状态投影。
- `corrections`/`memory-input` 也含内部 JSON；首批 raw knowledge 按内部审计类型隔离，合法 claim 新版本不受影响。纠正原文不能通过 before 字段把旧错误重新写成当前事实。
- owner GetSource/原件审计不承担 AI knowledge 供给职责；消费器持 owner token 也不能绕过 trusted task purpose。

## 5 纠正、升级、撤权、删除闭包

| 变更 | 原件与正常历史 | 需要即时失效/清理 | B/A 边界 |
|---|---|---|---|
| source v1→v2 | v1 可在明确合法历史视图中保留；current 只读 v2；精确 ref 不能换成新正文 | v1 支持的 claim/current derived、summary、desk answer、run/Brief/manual、attempt 结果均复核失效 | B ingest 调统一 helper；A typed current/history verifier与消费者 |
| claim correction | source 可以独立合法保留，纠正 key/redirect 保留 | 旧 claim/summary/run/desk/artifact/attempt 结果依赖闭包 | B correct；A消费者与递归 typed helper |
| source policy revoke/narrow | owner 原件保留；不因撤权删除其他主体的独立资料 | 对失效 tuple 停止供给；明确 source revoke 的 deny 沿 source→evidence target claim→summary/历史产物传播，即使 claim 有旧 grant；相关快照正文清除；后续 manual取/复制/submit/adopt拒绝；regrant不复活旧产物 | B policy+editing；A attempt/manual/历史读取/采纳 gate |
| delete claim, IncludeSources=false | 独立授权 source 仍可正常使用；不得误判 source 泄漏 | claim 及其派生正文删；来源无关内容保留 | B现有closure；A新增副本helper |
| delete source 或 IncludeSources=true | 来源正文/版本/原blob及派生清除；留无正文阻断标记 | attempt快照、desk question/answer/cards/ask、manual/Brief/run/output、采纳工件/文档/样本、来源preview/correction copies | B删闭包；A purgeArtifacts/attempt清理与正文恢复防护 |
| 已外发后删除/撤权 | 已发送无法撤回，记录发送时点 | 不再供给/回放/采纳；attempt留有界无正文 skeleton | A attempt；B触发purge |

A 已交接统一 `invalidateTypedContextTx(ctx,tx,ownerScope,memory.ContextInvalidation)`：包含 RecordIDs、reason、可选 Recipient；同 transaction 完成。source删除/替换不能用 Recipient 过滤豁免副本；revoke 可限定当前接收者，沿 source→evidence target claim→summary/产物递归失效。B 的 `sourcePolicyDeniedTx(ctx,tx,task,source)(bool,error)` 供 A claim verifier逐evidence来源检查显式deny；absence返回false，不能要求raw allow才给独立claim。禁止由B在editing.go编造消费者表的私有布局。

现有 `CorrectRequest.Target.Kind`、`DeleteRequest.Targets[].Kind` 未与 SQL 真实 kind 逐一核对；本批 B 将按冻结契约拒绝错误 kind，避免以 source 身份走 claim correction。来源替换走已有 Ingest 稳定身份/新外部版本；若需要交互纠正原文的 expected-version API，由 A 明确共享 request，不能以 claim Correct 伪装。

不可只靠延长生成事务锁保护授权：撤权必须能在 provider barrier 期间提交，生成返回重新核对并拒绝结果。attempt 要在独立短事务记录，并避免对未提交 source/desk/run 行的 FK。失效后的重放/请求回执必须经过相同 gate，不能从 JSON 保存响应直接返回旧正文。

## 6 记录生命周期工程决策请求

历史研究里的 7 天正文/30 天元数据原为待审提案。root 于本批已明确裁定工程初值：正文快照保留 7 天，每 owner 正文合计上限 64 MiB，每 attempt 256 KiB；无正文 attempt 骨架保留 30 天，每 owner 最多 10,000 条。这是有界存储初值，后续按成本基线校准，不是法定时长或性能/收益保证。删除与撤权优先于上述到期时间。

snapshot 容量须在外发前原子保留。A 草案的初始策略为先清到期正文/骨架，再回收最旧诊断正文以满足 owner 字节上限，原 attempt 明记 `capacity_omitted`，保留其 refs/映射与传输状态；不能冒充正文仍完整。单 attempt 超限只能裁剪真正最终载荷并同步映射/缺口或拒绝调用，不能发送全文却只记录半份。骨架条数仍不足返回具体 `record_capacity` 错误并拒绝外发，不静默丢 attempt。未知 token/费用/外部收到分别标 unknown，不写0。到期/删除/撤权即时清正文，留最小身份、state、时间、invalidated reason与hash；hash不能被描述成全文证据。正文不能进入普通日志或三个消费者各存一份；最终输入只存一份 bounded snapshot，候选记录只存refs/version/span和阶段。删除闭包优先于保留期。

清理须有确定 worker/周期入口，不仅依赖“下一次读时清理”。A 草案明确启动/宕机恢复立即扫描，正常最长一小时批量清理；覆盖正文 7 天、骨架 30 天、owner 总字节和条数上限，记录失败与具体可重试阶段，不能因为没有新请求而永久留存。所有读取也先检查到期；后台清理失败不延长正文可读期，物理清理延迟如实记账。存活回答、Brief、summary、artifact 的 durable typed deps 随产物生命周期保留，不从 attempt 到期 FK cascade 删除，以免到期后失去删改闭包。C 独立冻结边界预期，B 不改断言来适配实现。

## 7 文件所有权与未决项

B 可修改 `sources.go`、`editing.go`、新来源策略文件、`server.go` 和新来源 API 文件，以及本方案。A 负责 `memory` shared types/migrations、`workspace.Command`、`commands.go`、`desk_actions.go`、secretary schema、`actions_log.go` 的有限 policy undo、typed verifier/manifest/attempt、run/desk/artifact/summary 消费者。A 新 manual handler 由 B 在独占的 `server.go` 注册。C 独立写 fixtures/预期/测试。B 不写测试，不降断言，不 skip。

K0 尚须冻结：精确 policy 请求/撤权定位与幂等字段；provider endpoint 身份；workspace/global constraint 权限与 scope assignment owner 入口；policy undo 的前/后 revision 快照；typed invalidation/purge helper；正常版本更新与真正 source correction 历史读语义；attempt 周期/宕机恢复清理入口。有限自然句型、无 selected source 必澄清、必须可撤销及 §6 初始容量/时长已经 root 裁定。上述工程接口继续由 A/root 收敛，不把 SQL 选择推给用户。

2.1待续：复用现有ZIP/JSON mapping parser、archive_entries、来源context与队列；表达时间/角色/branch保留；历史迁入不自动创建今天事项/提醒/唤醒；同identity重导不复活删除/纠正；确定性raw保存与收费派生分批。当前不实现parser/UI或导完整私人历史。

本阶段验收由C从自然来源授权与已授权消费两条路径分别举证。SQL grant夹具只能诊断组装问题；静态方案、合成fake provider通过、真实模型、真机、连续任务收益各自单独报告。
