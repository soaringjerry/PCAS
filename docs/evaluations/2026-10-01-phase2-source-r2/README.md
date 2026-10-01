# Phase 2 来源授权首批有因复验

## 精确结果

2026-10-01 UTC，产品与运行 harness HEAD **`b63e02b2316c5608e194d3492050edaa0a1a3c73`**；独立工作区 `/tmp/pcas-phase2-c-source`、分支 `phase2/source-acceptance-r2`，逐例独立合成 PostgreSQL schema。一次执行同一七个顶层，**退出 0；7 顶层 / 13 叶级全 PASS；0 SKIP**。

本次有因复验包含产品 B `c16ca8e` 的 timestamptz 参数修复及 C `95fba54` 的 addProject Name 夹具修复。[首轮记录](../2026-10-01-phase2-source-r1/README.md) 原 643c26c 的 3 PASS / 4 FAIL 完整保留。本次没有降低业务断言或修改 original gold；未抵达的 wrong_kind 三个子例本次实际执行，所以叶级总数为 13（首轮 11）。

执行同首轮七项的精确正则与 `-count=1 -json`；命令见首轮报告。没有运行消费者后续测试，没有重跑旧失败基线。

## 本次证明的边界

| 真实动态测试 | 本次结果与证据 |
|---|---|
| PublicAuthorizationExactVersion | 最小 recipient 公开授权由服务器补全 canonical tuple；零 claim raw GetSource/Recall/Expand 可读；source ID 冒充 claim/summary/unknown 不返 raw；新轮 v2 正确、历史 v1 保持原正文和版本；KnownAt 旧记录时点拒绝 v2。 |
| NonownerRawAPIsRequireTrustedTask | 有 coarse grant 且 raw 查询命中时，四入口缺 trusted Task 均严格返回错误、无 raw；owner audit 兼容、客户端 JSON task 注入拒绝。 |
| SourcePolicyReplayRouteAndDisabledRevoke | 原 policy revoke 后重放冲突；独立首次 grant、无后继 revision 只变 route 再重放仍冲突且绑定保持原样；旧 tuple 在 agent 禁用后仍能 revoke。 |
| SourceScopeHardBoundary | 同一 source 给 studio A 和 B 分别合法 receiver allow；assignment 只 A，A 可读、B 与 soft Objects 提示不能放宽；assignment 清空使 A 旧读失效。 |
| ClaimNatureDoesNotGrantGlobalScope | preference/decision 不凭 nature 跨 studio；显式 source global_constraint 标记加已有 claim grant 可供给 claim，仍不隐含 raw source 授权。 |
| ExplicitDenyAndUndoFirstGrant | 无 source policy 的独立 claim 可用、raw 不可用；首次 grant undo 恢复 revoked 且非 explicit deny 的 absence 语义并保 revision；普通 revoke 阻断 evidence-derived claim Recall/Expand；undo revoke 恢复当前 allow/raw。 |
| PublicRecipientMismatchIsRejected | 客户端非空 tuple mismatch 拒绝且没有 policy 行。 |

## 证据与限制

[完整 JSONL](acceptance.jsonl.gz)、[全部合成原始 HTTP/Recall 证据](synthetic-evidence.tar.gz)、[计数/每例/全部 SHA256](summary.json) 均来自本次一次执行。HTTP 响应保留 text/base64；未声称本批运行 fake provider 消费者，未使用真实模型或私人数据。

本次只证明以上来源 API / typed reading / scope / undo 当前态边界。父源 derived_from/archive 传播、旧 attempt ABA、三入口实际模型输入、manual交付/重取/提交、生成中撤权/删改、whole metadata quota/retention 和会话排序五组仍需消费者组合验收。023 old-revoked 数据迁移、并发 policy 原子性未由本批证明。K0 DDL 历史证据保持独立。线上 11 题、真机与一天用户试用仍未完成。
