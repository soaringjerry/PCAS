# Phase 2 两个既有 CI raw 读取夹具迁移

产品固定 `88059b1287e4beadc4103b0b3d52b8f5b396bc5f`，C 授权独占修改 continuity/integration 指定两例；产品未改。独立 worktree、合成专属 PostgreSQL、唯一 schema；无真实模型调用或私人数据。

旧夹具仅插 `record_grants` 并以没有 Task 的任意 principal 读 raw。新契约要求注册实际 receiver、来源目的/范围 policy 与服务器 trusted Task。迁移使用真实公开 SourceAuthorizer HTTP owner API，canonical recipient 与 ContextRecipient 构造的 Task。原并发六组 Summary、claim 撤权后 cache miss/版本增/无撤权 claim deps、跨 owner 隔离、读 grant 不允许写、删除重导入阻断均保留。新增 coarse grant 单独不能 raw、明确 source deny 不能 cached raw-root summary，错误严格为 ErrForbidden。ungranted 缺 Task 和已撤 coarse 的 raw guard 从旧 NotFound 迁为 exact Forbidden，不宽泛接收任意错误。

| 一次运行 | harness SHA | 结果 / 原因 |
|---|---|---|
| R1，两顶层 | 8cd8c81 | exit1：integration PASS、continuity FAIL。注册 receiver 在 rememberForContinuity 已有 claim grant，C 手工 INSERT 重复，SQLSTATE23505；归 C 夹具。 |
| R2，仅 continuity | be99ddc | exit1：原并发/cache/claim 撤权业务断言实际通过。新增明确 source revoke exact Forbidden 断言得到 NotFound，因为 revoke 同时移除 coarse，未隔离变量；归 C 新增夹具断言前提。 |
| R3，仅 continuity | 980771c | exit0，1 PASS、0 FAIL、0 SKIP。在 source policy 明确 revoked 后仅恢复 legacy coarse grant，仍不恢复 actual receiver policy，隔离 explicit deny 后严格 ErrForbidden。 |

重复已有 claim grant 改为 ON CONFLICT DO NOTHING，不改授权状态。R3 的 coarse SQL 回补是**诊断隔离夹具，不是自然授权成功**。两次失败原日志保留；三个运行均有明确夹具差异，只对受影响单题作有因复验，未重复碰运气。最终 integration 已在 R1 通过，其文件此后未改；continuity 在 R3 通过。没有声称此处运行过完整 check 或消费者组合。

完整 [R1 JSONL](r1.jsonl.gz)、[R2 JSONL](r2.jsonl.gz)、[R3 JSONL](r3.jsonl.gz)、各轮原合成 HTTP 证据 tar 与 [计数及SHA](summary.json) 在本目录。原 #43 四项 CI 失败仍需协调合并 A/B 两个产品修复后完整验证。本批线上 11 题、真机、一天用户试用未完成。
