# Phase 2 派生读门定点有因复验 R3：通过

产品精确 `a5046d53ca2d9535a0c265096ff81447fcd4af35`，harness `5f762eba55dc9db4fa6e24d756774d8c5662dd27`。R2重复adopt夹具失败先保存在原报告；最小修正只从真实Snapshot取得已自动采纳Doc/Sample，保留全部原断言并新增auto doc正对照。未改产品或R2红报告。

**一次退出0；1顶层、2叶级（route/disabled）均PASS，0FAIL/0SKIP。** 专属合成schema、本地假HTTP，执行：

```sh
PCAS_PHASE2_EVIDENCE_DIR=/tmp/pcas-phase2-c-runtime-read-gate-r3-evidence go test ./internal/postgres -run '^TestPhase2RuntimeStateExportAndDerivedBlocksRecheckCurrentRecipient$' -count=1 -json
```

真实授权raw来源→Execute→RunAgents→假HTTP完成结果→自动采纳Doc/Sample为正对照，included训练导出含派生正文。仅改变模型route或禁用agent，未改source/policy：State/Export的Run/Doc/Sample旧派生正文及训练导出拒供；无关owner原创Doc与合法owner原始source审计保留。全部原正反断言抵达。

[完整JSONL](acceptance.jsonl.gz) 1796事件；[全部10份合成实际HTTP/输入/Run/读视图证据](synthetic-evidence.tar.gz)；[精确整数及逐SHA](summary.json)。原JSONL SHA256 `6498dea1da6d57b389eecac380d1430b87ae79457c10fefb0096394a42cfdae2`。输入来自实际bytes，不声称第三方内部上下文或外部成功receipt。

与R2的30顶层/67叶PASS组合，仅说明被冻结31组所有叶分别已抵达并通过；R2整批历史仍退出1，不改写为全绿单次运行。旧manual全面回归、完整Store SIWC、UI、真实崩溃重启、线上11题、真机及一天用户试用仍待完成证据。
