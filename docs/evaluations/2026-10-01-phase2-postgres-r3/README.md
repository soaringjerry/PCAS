# Phase 2 第三次完整 PostgreSQL：全部抵达，仍失败

精确产品及全部 harness HEAD **`207bd7062f02cf95d4d29ebaba1391d25a7972f1`**；clean 独立分支 `phase2/postgres-full-r3`，工作区 `/tmp/pcas-phase2-c-postgres-r3`。此头含旧 R2 fixture 修正、A 五小修、D4、025及 D 独立 action 字段矩阵（仅批准 studio fixture 修正）。运行过程中未更新产品或测试。

确认专属合成 loopback PG `phase2_c/phase2_c`、每例唯一 schema，先保存实际 `-list .` 的[236顶层选择清单、全部gold及测试文件SHA](selection.json)，再仅一次执行：

```sh
go test ./internal/postgres -count=1 -json
```

无筛选、重试、新增skip或真实账号；Go默认十分钟超时。2026-10-01 14:21:14.731488 至 14:25:37.256886 UTC，262.526秒（Go包261.985秒），退出 **1**，无panic或超时。

**236选择 / 236抵达；顶层228 PASS / 6 FAIL / 2 SKIP。543终态叶534 PASS / 7 FAIL / 2 SKIP；未抵达0。** 父子测试不重复计为叶。完整60630条JSONL事件和469份合成输入/HTTP/manifest/状态JSON均保留。

## 本轮通过与具体限制

- 原五组独立 conversation-order 全部 PASS，包括真实 PG backend 终止、过期 head、迟到输出和恢复原断言。
- 025三个顶层、022→023/023→024旧语义升级、旧fresh ledger，以及所有既有非D4 `Phase2Runtime` 顶层 PASS。真实三入口、最后派发与生成中fence、手动重新交付、ABA、whole metadata/count/body quota、实际UTF8估算预算和持久依赖边界仍按原gold检查。
- D1–D3 PASS：分别证明副手没有独立许可不外发、两角色合法供给且exact source可同时位于Input/Indirect、真实完成/自动采纳后undo，再撤权及ABA拒用。D4两叶仍红，不能将前三叶通过推广为所有delegate来源安全。
- D独立 action-lineage两个顶层（含字段矩阵）PASS；本报告记录固定头的实际结果，C未修改其gold或测试。
- 正常容量indirect-only、后续kind/revoke/delete、原31k容量拒交付、正式delegate undo日志、Codex假适配、真实timeout与非法userinfo双控、同scope合法普通continuation等先前修正对应组 PASS。

## 唯一失败集合：6顶层 / 7叶

| 顶层 / 失败叶 | 原始因果及责任 |
| --- | --- |
| `ApprovedRules_ReturnToSameContentStillRequiresReverseOrder/one-desk-transaction` | newer_action保护及第一次逆序undo成功后，第二次在approved_rules_acceptance_test.go:243返回精确ChangedSinceAction。另一three-command-transactions叶PASS。产品同事务多动作恢复/afterBlocks hash兼容回归；原逆序撤销业务不可改成接受错误。 |
| `SameTurnOriginalTenActionLimit` | 原十动作上限、九个checklist、undo第10项成功后，第9项same_turn_actions_test.go:312返回ChangedSinceAction。同一产品逆序恢复家族。 |
| `StabilizationUndoExistingTHIS_SameTurnRegression` | newer_action保护及首次add_steps逆序undo成功后，后续update在stabilization_undo_test.go:407返回ChangedSinceAction。同一产品逆序恢复家族。 |
| `SecretaryRejectsStaleRowsAndKeepsOriginalOnCancellation/stale` | verify/conflict日志正确，返回“已记下原话；上下文已变更，请重试”，缺原verify阶段后缀“，稍后会自动整理”（desk_turn_test.go:529）。原精确文案不减弱；本叶后续并发Title保存断言因Fatal未抵达。产品verify反馈兼容问题，与model阶段准确conflict反馈应分别保留。 |
| `Phase2RuntimeQueuedDelegationRequiresOriginalSecretaryActions/delegate_new`、`create_N1_delegate` | secretary和deputy各有独立合法policy，真实完成/自动采纳并undo，再真排队。仅撤secretary后旧Run stale、Output/Brief空、无Adopted，旧queue真实HTTP0，旧完成采纳拒绝；但Snapshot仍返回生成的 `Prompt="整理资料预约码 Q7-LANTERN-482"`（delegation_test.go:244），两条旧run均泄露原atom。deputy独立policy及trusted raw读取仍有效，后续新独立deputy真实HTTP供给原source正控也执行成功。错误只在旧生成Prompt读/擦除来源；不能把该派生Prompt改称owner独立正文。本轮fresh正控尚未显式核旧完成marker缺席/无origins，已另批准新增断言，未污染此次版本。 |
| `DeskHistoryCannotBypassDestinationItemScope/exclude` | 同studio真实秘书bytes/exact claim和真实deputy历史bytes正控均通过。正式toggleContextMemory目标排除后，requestRun在ux_regressions_test.go:487返回ErrConflict，尚未抵达原普通完成且真实下游HTTP无519823反控。其他两studio负控叶PASS。只读定位：prepare/history的typed hydrate未按目标context_exclusions过滤，final run_context.go:602–608对该间接dep拒绝，构成目标排除应丢历史而保普通请求的产品边界问题；不是缺授权夹具。保原成功+实际HTTP安全断言，具体修复待作者。 |

六顶层均按产品原因交作者核查；该分类不改变本次6/7失败数。本轮无C fixture改动或预期迁移，原失败日志及未触达的同叶后续断言保留。根协调已分别安排有因修复，但任何后续提交不属于本次207bd706证据。

## 原始输入、整数及未完成门

[完整JSONL](acceptance.jsonl.gz)解压SHA256 **`345bbc0abeb838f9ed406145eeb74d902d910c29776b4e8ee1de9ad3e93a2aac`**；[469份精简合成原始输入/HTTP/manifest/状态](synthetic-evidence.tar.gz)；[整数、全部终态、失败原话及逐份SHA](summary.json)；[执行时间和环境布尔证据](execution.json)；[未抵达清单](not-reached.json)为[]。原R1 panic及R2红报告未覆盖。顶层全部抵达不等于每个失败叶Fatal后的业务断言已执行。

两项原有live opt-in SKIP：`TestLiveCodexSecretaryAndLegacyFormats`（专用已登录Codex home），`TestLiveContinuityReplay`（真实embedding和专用Codex home）。未配置真实账号；不新增skip，不把它们计PASS。合成PG/假服务证据不代表真实模型门。

**完整后端门仍红。** 本批不代称D负责的独立UI或完整Store SIWC结论；Name alias独立来源用例和Prompt origins新契约尚未在此头验收。真实进程崩溃/重启（区别SQL显式恢复时钟）、线上11题、真机和一天用户试用仍无完成证据。
