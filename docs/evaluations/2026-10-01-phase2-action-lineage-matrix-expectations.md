# 动作字段谱系：独立正式矩阵预期（先冻结，待指定产品头执行）

本矩阵按已审定契约 ba93afc0581116905915c26f873a5bf69da2dba8 的 contracts §9 冻结；不是实现通过证明。首次产品 fb1c3eae / harness 336bf00 的红诊断、退出和原断言保留。新动态必须等待协调者指定含 025 及迁移配套的组合产品 SHA；9480cf2 的旧修复回归批次不覆盖本矩阵，其全量结果此刻尚未返回。

只改 D 独占的新 `internal/postgres/phase2_action_lineage_test.go` 与本命名前缀文档。复用 C 只读真实 Store/schema/loopback HTTP helper，不改其 gold/helper/assertions。每叶用例独立 schema；显式合成 PCAS_TEST_DATABASE_URL，loopback、数据库 `_test` 后缀或 `phase2_` 前缀；所有材料为合成，零真实账号/模型。

## 固定证据与判据

- 秘书真实 HTTP 收到源 atom，是生成前正控。模型输出字段确实写入 owner state；不是 SQL 伪造派生正文。正文 origin 引用真实成功 action ID，Runs 和 DeskActions 独立；读 action_log 和 durable deps 仅用于核证产品写入事实。
- 三出口为实际 DeskTurn 的 fake HTTP、实际 RunAgents 副手 fake HTTP、fresh ManualRunPackage；不是只测规划/Brief，也不把 manual 包说成外部收到。每个输入/包与持久 attempt manifest 保存。
- 无权限须遮派生并保 owner 独立段。双角色允许须真实供给派生字段且记录 exact source/version 的 Indirect；按 §9，即使相同 ref 已在 Input 仍须有 Indirect。Used 只来自 fake 明确 reply 且经产品校验，不作为血缘授权来源；Used=[] 同样保依赖。
- 非供给失效还要检查 canonical 字段、动态稳定 check/condition ID、promotion 副本及动作审计正文的清除；不以一处 UI/Snapshot 隐藏代替清理。无正文 context_task/context_stale 和 durable origin/deps 可保留。
- owner 原文选择不含 source/derived atom；上下游查询避免夹带测试 marker。正控不得借 query/system 同句制造 source Input。

## 十二条最小风险序列（含已审定共用门补充 2bdf253）

| 叶用例 | 真实操作序列 | 固定断言 |
|---|---|---|
| create_denied_used_empty | secretary-only raw；真实 create_task(title/notes/owedTo/waitingFor)+同轮 N1 add_steps；fake Used=[]；追加 owner 独立 notes/check；再 deputy/manual | 模型实际收到原文、所有实际派生字段带真实 action origin；两个新角色未获 raw 许可不得供给；owner notes/check 保留；空 Used 不免除依赖 |
| dual_role_direct_and_indirect | 同一 raw 分别正式授权 secretary、deputy、manual；同样 create+steps；下游查询能检索该 raw | deputy/manual 真正文含派生；exact ref 在实际 Input 和 Indirect 均保留，两个角色不互相继承授权 |
| dual_role_check_only_indirect | owner 创建普通 task，source atom 仅写模型 add_steps，title/notes/query 与该 source 无词项交集；双角色许可 | 实际 check 供给，Input 不含该 source，Indirect 包含 exact source/version/policy；不是凭 manifest 虚称检索命中 |
| mixed_update_and_stable_ids | owner 创建 task、idea、project；模型原支持 update.notesAppend 分别写 notes/body/goal，update.title；create_idea.condition；owner 删前一独立 check 后再写派生 check | 原 owner 段未染来源；新增段有 origin；check/condition 用稳定 ID；unauthorized manual 遮派生保 owner。无现存秘书 progress 写入口，不造新 op；progress reader 不冒充已覆盖生成 |
| role_and_studio_scope | 正式 source scope 归 studio A；secretary 精确 A 生成 task；manual 错 role 许可不放行；manual 正 role 但 studio B 许可不放行；最后改精确 A 许可 | server 从 ThingID/ProjectID 绑定 Task；错误 role/scope 都无派生供给，精确 A 正控真供给+Indirect；不靠客户端回传 origin 授权（客户端注入排列不在本十二叶实测覆盖内） |
| original_route_changes | 分离秘书 provider 与副手/manual provider；合法真实副手完成一份结果，核真实自动采纳，公开 undoAutoAdoption 撤销并核 Adopted=nil；停止 worker，prepare 旧 manual/queued deputy；先取合法旧包；仅改变秘书实际 route；包重取、旧 queued 执行、已完成旧结果采纳与三个新出口 | 目标 route 必须保持完全相同；旧包/queued run/已完成结果采纳受控拒绝；新出口 owner 内容仍可用且不带旧 marker；prepare 的 Run.contextDeskActions/Task.desk_actions 和实际 manifest.desk_actions 保真正 origin IDs |
| revoke_regrant | 双角色许可生成混写字段；先仅撤 deputy policy，秘书/manual 真供给而 deputy 遮派生，原 secretary action 不 stale；再正式撤秘书许可；检查清 canonical/副本；恢复 grant 后三个出口 | 先核 recipient-specific 因果，不因 target-only revoke 清仍合法 secretary origin；撤原 secretary 后永久 stale；regrant 不能复活旧派生；owner 段保留；后续真正重新生成可建立新 origin，不把旧字段改标签 |
| source_correction | 双角色许可生成；同 connector/externalID 更正为 v2 不同 atoms；三个出口 | old v1 派生永久失效，exact version 不漂移到 v2；canonical/动作副本清旧 marker，owner 独立段保留 |
| source_delete | 双角色许可生成；正式 Delete IncludeSources；三个出口 | 原文及全部派生/副本清理，stable origin 无正文可留；owner 独立段保留；没有旧 marker/许可复供 |
| undo_before_blocks | 模型派生+owner 段；owner 改写产生 beforeBlocks；撤来源后真实 undoAction，随后三个出口 | undo 不恢复失效旧派生，不把快照当 owner 文本；成功或受控 conflict/expired 均须保当前独立 owner 段；expired 必须稳定 audit ID 仍在且原 changes 清空；after fence/origin live 验证 |
| promotion_and_two_origins | 真实 idea 模型 notesAppend→body；合法真实副手 run 自动采纳正控，公开 undoAutoAdoption 撤销并核 Adopted=nil，再真实 adoptRun(progress) 正文形成 Runs origin；owner 独立段；真实 ideaPromote；撤源 | task.notes 复制两类 origin；原 idea 与 task 副本同清派生，owner 段保留；不得造假 Run 或 action ID |
| diagnostic_and_audit_expiry | 真实生成；显式 CleanupContextAttempts 使 body 和 metadata 到期；仅将自己 action 审计时钟推进至原清理窗口，触发既有清理；先合法读取，再撤权、三个出口 | diagnostics/audit 正文到期不让合法活字段永不可读；origin Task/deps 仍在且仍能读合法字段；随后撤权仍定位/清理，owner 段保留，旧派生不能复活 |

十二叶不是全字段×角色×mutation 的排列。三出口拒供集中在六个生命周期序列；生成支持只测现有 vocabulary，不新增 progress 等动作。delegate prompt/title 与同轮委派 gold 由 C 独占，本矩阵不重复冒领。

## 执行约束

先冻结并 commit，再写 harness/compile；产品指定后提交精确 harness SHA，正式矩阵一次。原红诊断另列在组合头的既有回归中，不改其断言。任何首次失败保存原日志/证据，产品问题报协调者交 A；只有确定 harness 缺陷且说明后才允许有因补验。记录退出、十二叶计数及实际 HTTP 次数；可清理自己的 schema，不能 skip、降低断言、无限重跑。最终全 PG 必须在含 025 与本矩阵的组合头另验，不能把不同头的绿拼成新谱系全链路证明。

共用 origin 上限/DAG/cycle 的穷举不在本矩阵；promotion 双 origin 与 route 的实际 prepared/dispatch/adopt 序列覆盖本批具体穿透风险。矩阵仍为十二叶。此阶段只 compile，不使用此前 9480cf2 的 230 抵达/13 顶层 fail 批次作为新谱系结果。
