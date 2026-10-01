# 既有 manual claim 正对照入口迁移

产品 `eeab123cc8649838a3bd72b28e19a1e842673489`，运行 harness HEAD `c1adcc328dbe403ea4af9bea5560c6cabbd07114`，具体 fixture `1043173`。2026-10-01 UTC，专属合成数据库唯一 schema，一次 `go test ./internal/postgres -run '^TestDirectCaptureEntersDefaultRunAsSourceBacked$' -count=1 -json`：**exit0，1 PASS，0 FAIL，0 SKIP**。

root明确授权C只修改 extraction_lifecycle 指定用例。旧→新映射如下，产品未改，人工正文不变：

| 旧测试入口/断言 | 新入口/断言 |
|---|---|
| manual无明确外部接收者 | command明确ManualRecipient{Provider:"model"}，服务器构造实际目标 |
| run.Brief持久上下文包含同一个claim正文 | 真实GET `/v1/workspace/runs/{id}/package`的package正文包含相同text |
| free verifyRunTx成功 | actual Store method s.verifyRunTx成功，核fresh route |
| State直接供给旧包 | 新增preGET run.Brief不含claim原文，不能跳过交付入口 |

原 adopted/sourced/direct 分类、保守agent不includeInferred、同claim ID在ContextMemoryIDs、原正文被供给、验证成功、模型调用calls==1（仅extraction）全部保留。真实package response.attempt DeliveredAt非空、Dispatchnil、receipt unknown由同一适配器断言。**不额外 grant raw source**，独立授权claim仍可用的正对照保留；没有为green把所有claim强迫成rawpolicy。

[完整JSONL](acceptance.jsonl.gz)、[原HTTP package响应证据](synthetic-evidence.tar.gz)、[哈希](summary.json)在本目录。wire response原JSON保留，adapter仅把package+attempt映到已冻结的语义字段。

本单题不证明source原文三入口、修改/撤权时manual重取/提交、route变更后State/Export gate、全局claim或全部历史兼容。full消费者与既有会话五组仍待组合。线上11/真机/一天用户试用未完成。
