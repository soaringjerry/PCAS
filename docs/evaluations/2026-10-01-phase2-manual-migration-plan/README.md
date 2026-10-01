# 旧manual测试迁移盘点（只读，尚未修改或执行）

盘点基线 `a5046d53ca2d9535a0c265096ff81447fcd4af35`。直接 `AgentID:"manual"` 共26处，其中 extraction_lifecycle 已按1043173合法迁移并动态通过；剩余25处缺ManualRecipient。下表按真实用例归并；一个helper还影响两个undo组。此文是独立夹具迁移提案，不宣称全postgres已通过。

共同最小夹具变化：每个需要交付的用例显式配置稳定本地文字provider，并在该条requestRun填写 `ManualRecipient:{Provider:已配置ID}`；提交结果前调用真实GET package，保DeliveredAt/unknown。原有Brief正文正反断言迁到package正文，同时保State.Brief不能成为未复验供给旁路。纯claim测试不追加raw来源授权。既有HTTP/embedding服务器与模型计数保留；只有embedding provider时另加独立文字destination，不复用embedding模型为人工文字目标。共同workspaceCommand不自动加target、不自动GET、不放宽错误。

| 文件与用例（原位置） | 原业务断言 | 必要变化/注意 |
|---|---|---|
| actions_log_test.go TestUndoAdoptionThenEarlierEdit:117（breakdown/summary/draft） | 自动采纳、undo清Doc/Sample/checklist/notes；随后更早rename可undo，Run完成正文不丢 | 独立文字target；该request显式target、paste前GET；各kind原输出及undo全部保留 |
| auto_adopt_test.go TestAutoAdoptSkipsStaleAndEmpty:375 | stale/empty/blank完成行均不自动采纳，Doc/checklist为空 | 显式target/GET作为合法prepared fixture；仍用原SQL状态构造测试autoAdopt内部跳过；不得改为把done状态吞掉 |
| desk_test.go TestDeskAnswerWithholdsRevokedArtifactTitle:102 | Run确有私密claim依赖；撤权后Desk不能泄露派生标题 | 复用既有model文字provider，manual显式target与GET后paste；原Desk真实请求/标题拒供断言保留 |
| artifacts_test.go TestEditedArtifactRetainsFieldProvenance:24,54（task/idea/project） | 编辑后依赖保持，撤权不供给编辑派生内容，删除后Export无原私密内容 | 初次GET建立含claim正对照，再paste；撤权后的新run显式target，负正文检查迁到真实GET包；sanitize与导出原断言保留 |
| artifacts_test.go TestPromotedIdeaRetainsArtifactProvenance:76,96 | promotion复制依赖；legacy删除provenance后读取修复；撤权无供给；删除能独立修复旧promotion | 初次真实GET后paste；撤权新包负正文；复制count==1和删除Export断言均保留 |
| run_retrieval_test.go TestRunKeepsRelevantOldMemoryAheadOfRecentNoise:24 | 旧相关claim不能被近期噪声挤掉，ContextMemoryIDs有该claim | 明确文字target；正文“蓝色灯塔”正对照迁GET package，原排序/依赖断言保留 |
| replay_test.go TestRunGrantRevocationAndArtifactCleanup:112,121,129,134 | 实际claim检索、派生transitive依赖、超长brief不直接含claim、kind限制、撤权、删除Export清理 | 每条request单独target+GET；首条GET后paste；大31k事项的最终package若触发新24000B估算预算不能简单缩小丢原排挤语义，应报告具体边界冲突再裁定；kind/撤权负断言在GET正文 |
| workspace_test.go TestWorkspaceMemoryLifecycle:119,134 | 初始无demo/仅manual、即时Recall、允许claim供给、paste/adopt、旧revision冲突、撤权不漏、删除 | 在最初agents==1断言后配置目标；allowed正文移GET；刷新真实GET导致的revision如有需Snapshot边界，旧staleExpectedRevision负对照仍保留 |
| undo_test.go TestDeletionExpiresItemAndDocumentSnapshots:240 | Run真实依赖；删除使相关action snapshots过期且Undo ErrExpired；非相关action可undo、源preview清除 | 配置target/GET给run-backed文档合法谱系；仍SQL直接建Doc不调model/不加adoption samples，原expiry与owner文字完整保留 |
| ux_regressions_test.go TestMixedWritingSurvivesArtifactRevocationAndDeletion:144 | 私密派生被撤权删除，owner自行写的引言保留 | 配置target、GET后paste；原mixed writing/保留自有文字断言不变 |
| ux_regressions_test.go TestContinueThatKeepsCurrentItemResult:225,230,233 | 同事项合法前结果保留，其他事项私密结果不混入 | 前两run分别GET后paste；第三GET的正文包含revise paragraph two且不含其他结果；相同canonical target不改route |
| ux_regressions_test.go TestRunSemanticRetrievalDoesNotHoldOwnerLock:268 | embedding回调能拿owner锁；semantic-only claim进入输入 | 现有vector仅Embedding需另配置文字target；保callback/锁/embedding向量；输入正断言移GET |
| ux_regressions_test.go TestLegacyMixedWritingIsQuarantinedForOwnerReview:283 | 模拟legacy provenance缺失后不静默删owner文字；隔离内容可owner读、非ownerForbidden | 初次GET后paste，全部quarantine/原owner文字/精确Forbidden保持 |
| ux_regressions_test.go TestContinuationHonorsCurrentItemScopeBeforeSemanticRetrieval:313,318,344（exclude/move-project/revoke） | 当前scope先守门，embedding query无私密但含合法旧结果，实际供给/依赖同边界 | 前两GET后paste；后续SetModels加vector时必须保同一个文字target/route，避免route单变量变化掩盖scope因果；最后正文移GET；embeddinginput原断言保留 |
| stabilization_undo_test.go stabilizationUndoCompletedRun:291→U8/U9 | U8自动采纳undo/readopt/undo对称，完成正文/cost保留；U9后续check命令或SQL变化挡undo | 仅helper内显式配置/target/GET该真实manual-run；不改公共command；U8 model-worker分支既有fake/次数与原两分支均保留，U9两种changed_since/newer_action保持 |

缺destination是负对照，不能由helper悄悄修复。新增独立phase2用例应验证已有文字provider但manual请求缺target仍拒绝且无run/attempt/HTTP；已有malformed/manual GET/paste未交付拒绝用例继续保持。

以上仅授权前清单，旧文件未写入。需要root逐文件授予单写入者与精确产品基线后执行；夹具契约迁移与产品回归分别报告。原始正文、撤权/删除/undo/版本/owner保护/模型计数不削减。
