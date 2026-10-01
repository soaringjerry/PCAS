# 首轮全PG提前终止后的剩余协议/入口盘点

只读基线7877b08；R1完整原证据e17d8dab保留。前一直接AgentID盘点漏动态agent和多字段赋值，本次按requestRun/worker调用及相关fake逐例查看。

| 文件/用例 | 真实差异 | 已批准最小适配 |
|---|---|---|
| auto_adopt_test.go autoAdoptModel + DestinationsAndUndo/ChangedSince | plain模型正文与正式Run {output,used}不同；动态manual7没target/交付 | 同original output包装Used[]；7manual显式auto-model+GET；ChangedSince硬done/Adopted guard，全部cost/样本/undo不变（9762498） |
| auto_adopt_test.go AutoAdoptFailurePreservesCompletedResult/manual两叶 | 动态agent选择遗漏；savepoint失败验证必须先合法交付 | 显式target+GET；保私密warning不泄露、完成输出/revision、成本、次数、SQL故障回滚全部断言（e99594b） |
| workspace_revision_test.go WorkspaceStaleRevisionCommandsAndReplay/requestRun | command.AgentID多字段赋值manual遗漏 | 明确既有model目标，原background revision/重放深相等断言保留（e99594b） |
| artifacts_test.go EditedArtifactRetainsFieldProvenance | typed run无Store free sanitizer拒绝；project claim无同studio归属 | Store method+server可信Task；真实project先建、acceptCandidate ProjectID同项目，无global放宽/无raw授权；原正文/谱系/删除不变（9762498） |
| approved_undo_test.go ApprovedUndoProtectionPriority 与 undo_test.go UndoQueuedDelegationAndStartedWork | 直接commandTx delegate没有正式prepared上下文 | 正式Execute(delegateTask)，同原ActionID；原优先级/started/预算/orphans全部不变（9762498/e99594b） |
| replay_test.go AsyncRunInvalidatesDuringGenerationAndBudget | plainfake协议错误可能掩盖迟到响应拒绝；Brief只包含query也能误判有供给 | 同旧结果包装output/Used[]；假服务actualbody收取后exactclaim Input payload byte spans验证；原更正/预算/failed-stale断言保留，defer只释放Fatal遗留barrier（e99594b） |
| desk_codex_test.go CodexSecretaryAndLegacyFormats | 新Run传outputSchema，fake原断言没schema且回plain/draft对象 | 正式Run schema断言及包装同原两格式；原Run.Output纯文/JSON两业务断言、秘书/legacy answer schema检查与not-web断言保留（e99594b） |

UX QueuedRunStopsWhenItemScopeChanges的“Should not run”是假服务不应抵达负对照，保HTTP0，未改该fake；所有answer/extraction协议保原请求语义。LiveCodex opt-in原skip保持，未配置真实账号；已核本环境PCAS_LIVE_CODEX_HOME/BINARY都空，下一轮按实际SKIP记录这个旧真实模型门缺口，不称229题全部实测通过，不新增skip。

代码静态另发现：context_generation schema非空统一Registry.GenerateWithSearchSchema，对Codex开启live search；旧Run是Generate/not-web。fake迁移仍保not-web断言，这个外发能力变化需要A核证产品，不能为绿宽松fake。尚未动态运行修复批，所有compile-only不计通过。
