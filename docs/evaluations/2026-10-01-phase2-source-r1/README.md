# Phase 2 来源授权首批独立动态验收：未通过

## 精确边界

- 产品与执行 harness HEAD：`643c26c885b856bac35c39e47af3a7ed325fd420`，工作区 `/tmp/pcas-phase2-c-source`，分支 `phase2/source-acceptance`。
- 2026-10-01 UTC，一次真实 PostgreSQL 动态运行；7 顶层、11 叶级。顶层 **3 PASS / 4 FAIL**，叶级 **6 PASS / 5 FAIL**，**0 SKIP**；命令退出 **1**。包 elapsed 1.958 秒。
- 只用专属合成 PostgreSQL、逐例唯一 schema、真实 Store/service/HTTP owner authorization；fake HTTP provider 未被本批消费者调用。不使用真实模型、私人材料或通知。
- 新轮 v2 查询重新绑定 Task.Now 的夹具修复 `c94f4e26e8d9493b83d2ac9bb80607069a64e3a1` 已在执行 HEAD 中。原 gold、exact version、KnownAt 断言未修改。

执行命令（DSN 从环境传入；不记录凭据）：

```sh
PCAS_PHASE2_EVIDENCE_DIR=/tmp/pcas-phase2-c-source-evidence go test ./internal/postgres -run '^TestPhase2Runtime(SourcePublicAuthorizationExactVersion|NonownerRawAPIsRequireTrustedTask|SourcePolicyReplayRouteAndDisabledRevoke|SourceScopeHardBoundary|ClaimNatureDoesNotGrantGlobalScope|ExplicitDenyAndUndoFirstGrant|PublicRecipientMismatchIsRejected)$' -count=1 -json
```

## 结果与责任

| 顶层测试 | 本次结果 | 已观察边界 / 失败责任 |
|---|---|---|
| SourcePublicAuthorizationExactVersion | FAIL | 正式 POST partial recipient 授权成功、canonical tuple 匹配真实 Task；首次 GetSource SQLSTATE 42883。产品 SQL 参数类型故障，后续 Recall、wrong kind、v2/history/KnownAt 尚未抵达。 |
| NonownerRawAPIsRequireTrustedTask | PASS，4 子例 PASS | 已存在 coarse grant 的 raw 候选缺 trusted Task 时，GetSource、Recall、Expand、Summarize 均返回错误且无 gold raw；owner audit 仍可读；客户端 JSON task 注入拒绝。 |
| SourcePolicyReplayRouteAndDisabledRevoke | PASS | revoke 后旧请求重放冲突；另建无后继 revision 的首次 partial 请求，只改 route 再重放仍冲突、原 canonical policy 未变；禁用 agent 后旧完整 tuple 仍可撤权。 |
| SourceScopeHardBoundary | FAIL | C 夹具以 Title 创建项目，真实 command 要求 Name，返回 invalid input。尚未抵达同源 A/B 两合法 receiver allow 与 assignment A 的范围隔离断言。 |
| ClaimNatureDoesNotGrantGlobalScope | FAIL，preference/decision 均 FAIL | C 同一项目创建夹具问题，尚未抵达普通 unscoped nature 禁止跨 studio 与显式 source global marker 正对照。 |
| ExplicitDenyAndUndoFirstGrant | FAIL | source absence 时独立授权 claim 可读/raw 不可读；首次 grant undo 后 revoked=true/explicit_deny=false、claim 仍可读；明确 revoke 后 claim Recall/Expand 被阻断；undo revoke 后恢复 raw 查询触发相同 SQLSTATE 42883。最终恢复 raw 未证明。 |
| PublicRecipientMismatchIsRejected | PASS | 客户端填入不匹配 tuple 字段拒绝，未产生 source policy。 |

产品失败涉及两个顶层，错误原文为：

```text
ERROR: operator does not exist: timestamp with time zone <= text (SQLSTATE 42883)
```

定位执行版本 `internal/postgres/source_policy.go:103`：`rv.recorded_at<=coalesce($5,$6)` 中两个未定型参数被 PostgreSQL 推断为 text。C 未修改产品；已交 A/B。scope 两顶层 / 三叶级失败由 C 夹具承担，独立修复提交 `95fba54` 仅把三个 addProject 参数从 Title 改为 Name，未改 gold 或业务断言。该修复未被本报告计为动态通过。原始整批未重复运行。

## 原始证据

- [完整 JSONL，gzip](acceptance.jsonl.gz)：698 事件，包含真实失败、测试名、全部日志；解压 SHA256 `a17748f6782b1bc6ec3e0142152d23b4264f3cd427909ee5e5399fbb3ef8e692`。
- [23 份合成 HTTP/Recall/拒绝证据](synthetic-evidence.tar.gz)：公开 HTTP 请求结果保留 response_text 与 response_base64；无 bearer、DSN 或真实数据。未声称本批包含实际模型 HTTP 请求。
- [精确整数、文件哈希与每例状态](summary.json)。压缩包 SHA256 逐项记录其中，可复核解包文件 SHA。

## 未完成的验收

本次失败之前未抵达的断言需要在明确产品修复与 C 夹具修复后，对 root 指定的新精确 SHA 作有因复验。本报告不能证明 source raw current/history、KnownAt、studio scope、显式 global claim、最终 undo allow 已通过。消费者 Desk/Run/manual、prepared→dispatch fence、生成中删改撤权、retention/quota 与会话排序五组属于后续组合验收。K0 DDL 报告保留原有 22 migrations 与 35 PASS 精确历史，不能外推 runtime 通过。线上 11 题、真机与一天用户试用仍未完成。
