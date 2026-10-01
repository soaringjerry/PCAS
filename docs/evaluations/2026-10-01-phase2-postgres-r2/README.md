# Phase 2 第二次完整 PostgreSQL 回归：执行完整，仍失败

精确产品及 harness HEAD `7706baa4f7c66fb7415c294f2bd703df62503154`，独立 clean 分支 `phase2/postgres-full-r2`，工作区 `/tmp/pcas-phase2-c`。此头为已批准旧修复组合加独立 D1–D3，不含新 action-lineage 红诊断、025 或其消费者修复。

先确认专属 loopback PG 返回 `phase2_c/phase2_c`，保存[实际230顶层选择清单及全部gold SHA](selection.json)，再**仅一次**运行：

```sh
go test ./internal/postgres -count=1 -json
```

每例沿既有唯一 schema 隔离，全部合成输入、本地 fake provider；无选择过滤、重试、新增 skip 或产品改动。Go 默认十分钟超时。2026-10-01 13:51:59.687162 至 13:55:13.103421 UTC，193.416 秒，退出 **1**，无 panic、超时。

**230选择 / 230抵达；顶层215 PASS / 13 FAIL / 2 SKIP。514终态叶495 PASS / 17 FAIL / 2 SKIP。未抵达0。** 父子终态共576条，不把父测试再次算作叶。全部20445事件及367份合成JSON保留。此次没有沿用上一轮的201未执行项：本轮确实全部抵达。

## 本轮实际通过的边界

- 原会话排序独立五组均 PASS，包括真实 PG backend 终止、过期 head 和迟到输出原断言。
- 25个既有 `Phase2Runtime` 顶层均 PASS：三个真实入口、自然/公开授权、scope/deny/undo/knownAt、before-dispatch/in-flight、manual ABA、实际 bytes/span、quota、retention、023/024与显式恢复时钟。
- 新 D1/D2 两叶 PASS：秘书真实收到零claim来源原文；没有 deputy 独立许可时副手 HTTP0；双角色许可则真实完成及自动采纳，实际 deputy Input 和 Indirect 可保留同 exact source，谱系绑定 deputy 自己的 policy。D3除两处清空快照错误返回判断外其控制抵达，**仍记 FAIL**。
- 原 manual extraction fixture、缺目标拒绝控制通过。首次执行正常容量 `normal_indirect_then_kind_revoke_delete` **PASS**：真实 GET 包的完整 Input 无 exact claim，Indirect 有同 exact claim，再实际抵达 kind/撤权/删除拒供与 owner 独立文本断言。没有调词重试、SQL伪造已交付或缩短原31k。独立31k叶红项如下，不称该叶通过。

## 13失败顶层 / 17失败叶责任表

| 顶层（失败叶数） | 冻结原始观察、责任与最小后续 |
| --- | --- |
| ApprovedUndoProtectionPriority (1)、UndoQueuedDelegationAndStartedWork (1) | 两例已迁正式 `Execute(delegateTask)` 且成功创建/排队，随后以同 RequestID 查 action_log/Undo 分别 no rows / NotFound。`actions_log.go:undoableCommand` 未列 delegateTask，Execute不创建 action_log；不是随机ID差异。**正式产品撤销入口缺审计动作**，责任 actions_log.go/commands.go。旧直接内部调用曾用 withActionLog装配，不能再靠 test 内部装配掩盖缺口。所有撤销优先级/未开始可撤、已开始拒撤、模型0/预算/孤儿断言保持，后续尚未抵达的同例分支不称通过。 |
| AutoAdoptWorkerSkipsStaleFlag (1) | 真实HTTP回调把run置stale，最终fence现 failed+stale、Output空、Adopted nil、Docs0；旧要求 done+stale。**root审定2.0预期冲突**：必须精确 failed并保 stale/noadopt/noDocs，新增 Brief/Output空和已发生调用成本记账；不是接受任意状态。当前旧断言仍红。 |
| CodexSecretaryAndLegacyFormats (1) | Secretary步骤成功，legacy AnswerDesk fake要求无outputSchema；正式产品已明确 `answerDeskSchema`，fake Python assert触发，真实HTTP路由500。**C fake协议适配**；保 search/web==true，精确 answer/used/links schema及原业务答案形状；Run继续web==false/output+used，原两种plain/draft业务输出断言保留。本叶后续Run断言未抵达，不称Codex完整通过。 |
| SecretaryModelOutputRecovery (3) | extra_fields/preface/markdown均正确生成原 reply/tasks/receipt，随后 INFO parse/none日志为空，日志helper报JSON EOF。`desk_turn.go`现在只在 parseErr时WARN，删除了成功解析原受控INFO。**产品可观察日志回归**，不能删日志或把正常fake改成错误响应。 |
| DeskAnswerRejectsRevocationDuringGeneration (1) | 真实provider barrier内撤claim，最终返回Answer空+ErrNotFound，旧要求精确ErrConflict。没有旧正文成功返回。**最终fence错误契约回归待产品裁定**；desk.go的check直接传播typed读取NotFound，原精确Conflict仍保留，不宽泛接纳任意错误。 |
| SecretaryMemoryCardsAndRevokedHistory (1) | 删除依赖claim后仅一条历史turn、正文及cards清空；旧要求变更placeholder。**root审定删除契约冲突**：删除必须 Reply/Text空+Cards空，保原ID/一条turn/跨owner隔离；更正/撤权placeholder仍分别保留，不能恢复旧正文。当前旧断言仍红。 |
| Phase2RuntimeSourceDerivedDelegationRoleIsolationAndABA (1) | 仅D3两次 `ContextAttemptSnapshot` 返回 ErrUnavailable+nil；对应旧attempt确invalidated，ABA不恢复，旧读/Export/采纳拒绝及新run生成正控均抵达。**C新harness错误契约误判**：冻结API snapshot为SQL NULL时必须ErrUnavailable；应精确断言该错误且len(body)==0，保持状态/ABA所有断言。不能把此拒读称正文可读。 |
| RunGrantRevocationAndArtifactCleanup (1) | 原31k完整正文真实GET返回507 `{"error":"record_capacity"}`，C误查code字段。**C wire字段适配**；必须507+精确error值，原31k/ownerNotes/间接deps保留，继续硬断言无DeliveredAt/Dispatch/HTTP。此次此叶在协议断言后Fatal，后续无交付检查未抵达，不能预记通过。另一正常容量叶已PASS。 |
| StabilizationD1_DeletionDuringGenerationCannotCommitDerivedText (1) | 原HTTP已证实供给目标，生成中删除后返回安全capture，无私密原文/动作落库，但反馈误为“模型没有响应”，旧要求“上下文已变更”。**产品最终fence反馈分类回归**，desk_turn.go/desk_parse.go；不能删反馈断言来绿。 |
| StabilizationS8_FailureCategoriesKeepOriginalAndPrivateLogs (1) | 仅timeout配置BaseURL含userinfo；新受控recipient构造在外发前拒Invalid，日志context/invalid_input，无法抵达原真实timeout。**C不合法配置夹具**。提案保敏感userinfo非法配置拒发负控，另合法loopback假服务真实超时保原timeout日志/反馈/无泄漏/原文保存，不能删除敏感URL断言。 |
| ContinuationHonorsCurrentItemScopeBeforeSemanticRetrieval (1) | move-project叶semantic请求实际只有 `Continue that Writing`，无非法私密atom，也丢了无依赖的 `Ordinary permitted plan`；exclude/revoke两叶PASS。`verifyRunForItemTx`对 ContextTask scope无条件匹配，source-free先前run随事项studio移动被拒。**产品普通无依赖continuation兼容回归待裁定**，run_context.go；保持合法普通历史正控与不供私密反控，不能删前者。 |
| DeskHistoryCannotBypassDestinationItemScope (3) | 3叶在正控已有project claim但无scope参数的 legacy AnswerDesk 就读不到dependency；尚未触及destination gate。**C新硬scope正控不合法**，不能用SQL造deps或把一般fact设global。提案同project事项绑定真实Scoped DeskTurn，实际秘书输入证明exact claim/519823，真实Desk turn ID供给后续requestRun/delegateTask；同studio允许先作真实下游bytes正控，再excluded/不同studio/新unscoped目标实际RunAgents HTTP无519823/无旧派生answer，保ContextMemoryIDs否定。仅blank Brief不够。 |

归类：产品回归/缺口 **6顶层8叶**（两撤销、成功解析INFO、AnswerDesk错误码、生成中删除反馈、普通无依赖continuation）；C夹具/协议 **5顶层7叶**（Codex、D3、31k、timeout、scoped history）；root审定旧/新契约冲突 **2顶层2叶**（stale状态、删除正文）。此分类不改变本次13/17实际失败计数；所有修复和有因复验均须另有精确头。

## 原始证据与未完成门

[全部JSONL](acceptance.jsonl.gz)，解压后SHA256 `1d033922132809f05aecb4ab9ce1ad2a51679e875711a82a61f6019f2360b6cd`；[367份合成输入/HTTP/manifest/状态JSON](synthetic-evidence.tar.gz)；[整数、叶失败原话、每份SHA和goldSHA](summary.json)；[未抵达清单](not-reached.json)为[]。原本R1 panic红报告及其他精确版本证据未改。

**两项既有 opt-in SKIP**：`TestLiveCodexSecretaryAndLegacyFormats` 需要专用已登录Codex home；`TestLiveContinuityReplay` 需要真实embedding+专用Codex home。本轮不配置真实账户/模型，不新增skip，不把它们计PASS。合成PG全部抵达不等于真实模型全部通过。

完整PG门仍红；025/action-origin/final全组合不在此头；本批不代称D负责的UI或完整Store SIWC结论。真实崩溃重启（区别SQL显式时钟恢复）、线上11题、真机和一天用户试用仍无完成证据。
