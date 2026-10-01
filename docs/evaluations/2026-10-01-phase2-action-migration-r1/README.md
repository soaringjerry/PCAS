# 025 独立迁移动态验收：6顶层 / 18叶通过

精确产品+harness HEAD `9d60f310beec1748b4e99ed22a3e86b4493cd1e9`，包括025冻结DDL与C `cabe349` 迁移harness、A origin产品；独立clean工作树 `/tmp/pcas-phase2-c-025` / 分支 `phase2/action-migration-acceptance`。本批没有D4 WIP或D action矩阵，也没有重复其他runtime套件。

2026-10-01 UTC，专属合成loopback PG、每例唯一schema。先实际-list并保存[精确六个顶层、gold SHA与命令](selection.json)，再**仅一次**：

```sh
go test ./internal/postgres -run '^(TestPhase2Runtime025PreservesLegacyActionsAndUndoMeaning|TestPhase2Runtime025OriginDDLOnlyAcceptsObjectOrSQLNull|TestPhase2Runtime025FreshExactly25AndRestartStable|TestPhase2Runtime023PreservesOldPolicyMeaningAndVersions|TestPhase2Runtime024BackfillsOnlyAutomaticExecutionLease|TestPhase2RuntimeFreshMigrationRestartKeepsLedgerAndNoPolicy)$' -count=1 -json
```

退出 **0**，2.942秒；**6顶层全PASS / 18终态叶全PASS / 0 FAIL / 0 SKIP / 未抵达0**，334事件、16合成JSON。Go默认十分钟超时，未延长、重跑、修改产品或断言。

| 动态边界 | 实际证据 |
| --- | --- |
| 024真实旧action→025 | 原022旧schema应用023、024后硬断言24条、无origin列；真实PG文档trigger收集5行changes/afterHash，含command-live、desk-undone、worker-expired、newer-command。升级前后全部原action JSON（移除唯二新增字段）及work_items完整JSON相同，身份/source/body/hash/time/order不丢；所有5行context_task SQL NULL、context_stale=false。 |
| 旧Undo四叶 | 升级后真实Store.Undo返回精确AlreadyUndone/Expired/NewerAction；live旧创建真实撤销，原事项移除、原receipt undone标记保留。历史undone/expired状态此前由明确SQL fixture装配，不虚称旧二进制API执行。 |
| DDL八叶 | SQL NULL和object允许；array/string/number/boolean/JSON null分别PG23514；context_stale SQL NULL PG23502。每负控有独立fresh schema/autocommit，不污染Undo正控。默认NULL/false均实测。 |
| Fresh/restart | 精确25条，025恰1且批准固定checksum `ce73cfd82375ee0af81a50736aa07e2f043757d8c78daeb33dfb7650e3e8a1d1`；CheckSchema和再次Migrate后账本字节不变，无model配置/调用。 |
| 原升级链 | 未改旧gold/断言：022→023保政策身份、版本、deny含义；023→024的serialized_request、adapter_arguments、manual_package三种真实旧记录lease回填；原fresh/restart账本及无implicit policy/HTTP控制均PASS。 |

人工预期gold先冻结 `c219755`，SHA256 `34d5cf079b1741147ad56457eafe2fa65b0a97691f61ef53c3a7402b94e2f817`；完整SHA见selection。原K0/fresh22、023/024既有报告及全PG R1/R2红结果保持其精确版本；后来fresh25不覆写此前证据。

## 完整原始证据

[334事件JSONL](acceptance.jsonl.gz)，解压后SHA256 `3aadffe96f895f304742a8b3d4f2da8cebf2d8f934fae01e9b4b54bf158546bf`；[16合成JSON输入/状态/实际旧changes及账本](synthetic-evidence.tar.gz)；[完整整数、逐叶结果与每文件SHA](summary.json)；[未抵达清单](not-reached.json)为[]。

本批证明DDL约束和真实历史状态升级。允许JSON object不意味着该object有合法trusted task/授权；没有运行字段读取/外发/promotion/来源撤权/动作Undo全消费者矩阵、D4或全PG。本版本产品包含origin实现不等于本次选择已验其runtime。原全PG门仍红，最终组合需另跑全量。真实模型opt-in、真实崩溃重启、线上11题、真机和一天用户试用没有由本批完成。
