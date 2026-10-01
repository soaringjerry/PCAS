# Phase 2 023/024 迁移与恢复独立动态验收

2026-10-01 UTC，产品 `eeab123cc8649838a3bd72b28e19a1e842673489`，运行 harness `56af5b4f98aa279420759dd76f977568a478f8c5`；工作区 `/tmp/pcas-phase2-c`、分支 `phase2/consumer-acceptance-v2`，合成专属 PostgreSQL / 逐例唯一 schema。

**一次完整运行，退出0，4 顶层 / 13 叶级全 PASS，0 SKIP。** 未混未完成的 Desk 消费者测试。

```sh
# PCAS_TEST_DATABASE_URL 从环境提供，不写入凭据
PCAS_PHASE2_EVIDENCE_DIR=/tmp/pcas-phase2-c-runtime-migrations-evidence go test ./internal/postgres -run '^TestPhase2Runtime(023PreservesOldPolicyMeaningAndVersions|024RecoveryUsesExecutionLeaseAndFixedStartupCutoff|024BackfillsOnlyAutomaticExecutionLease|FreshMigrationRestartKeepsLedgerAndNoPolicy)$' -count=1 -json
```

| 项目 | 实际证明 |
|---|---|
| fresh + repeat startup | 24 migrations 已应用、023/024各一次、0隐含sourcepolicy；CheckSchema通过；重复 Migrate 的全 ledger（含 checksum/applied_at）不变，模型调用0。K0原22迁移/35PASS历史未改。 |
| 真实旧022→023 | 先创建真实001..022，两个旧 allow/revoked policy revision7；升级后旧policy所有字段（排除新增explicit_deny）完整 JSON 相等、身份/版本/时间不丢；旧revoked backfill deny、旧active非deny，source原正文与ref保持，deny=>revoked约束、repeat不reset。 |
| 真实旧023→024 | 先创建001..022并实际执行023，再写serialized_request/adapter_arguments/manual_package旧prepared checkpoint；Migrate后automatic只加created+5min lease、manual保持NULL、state仍prepared；repeat可启动，automatic NULL与manual lease都被DDL拒绝。 |
| 显式时钟恢复 | eight checkpoint controls：旧prepared无reserve、reserved无observed、observed dispatched未完成均outcome_unknown；未到lease旧active、created晚于固定startupcutoff、manual prepared、completed、invalidated分别保持正确态。observed dispatched_at原样，reservation不伪造observed，receipt unknown；periodic用同cutoff，即使后来created checkpoint的lease过期也不误恢复；零cutoff拒绝，真实HTTP调用0；恢复后的全row+typeddeps metadata实测与stored严格相等。 |

## 原始证据和范围

[完整 JSONL](acceptance.jsonl.gz)、[全部合成 checkpoint/迁移证据](synthetic-evidence.tar.gz)、[精确计数/SHA256](summary.json)。checkpoint 是 SQL 模拟以前进程留下的状态；**不声称 checkpoint 的 dispatched_at 来自本批真实网络发送**。本次调用的是实际 RecoverContextAttempts；不把取消请求、expiry Cleanup 或编译 no tests 当进程恢复证据。

本次证明恢复方法对明确边界的处理；启动 worker 固定cutoff接线在代码中存在，但未由杀进程/重启集成演练证明。automatic lease的真实生成默认值、并发恢复、真实失联进程付费/重发仍需消费者/worker组合。未证明whole owner quota、三入口实际input、manual交付、在途mutation、State/Export route读gate或保序。线上11题、真机、一天用户试用仍未完成。
