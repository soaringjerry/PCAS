# SIWC 未选model的真实Store链路独立验收预期（冻结）

产品基线c29d7af（后端a5046d53）。隔离工作区/tmp/pcas-phase2-siwc-store-acceptance，分支phase2/siwc-store-acceptance。新增测试仅在internal/ai/siwc中，业务实现、已有测试和原fixture不修改。

使用既有siwc_test.go fake OIDC/catalog/SSE fixture，由新的package siwc测试导出桥封装newFixture/signIn并捕获实际HTTP正文；package siwc_test调用公开postgres.Open/Migrate/SetModels/Execute/DeskTurn与诊断API。不复制OAuth/parser，不加入生产注入开关，不用linkname。所有源、claim、问题、回复和账户为合成。

| 子场景 | 冻结预期 |
| --- | --- |
| 普通问答 | provider.Model与账户Model均为空，真实Store.DeskTurn成功产生合成reply，实际fakeSIWC收到一个请求；Used[]为空。 |
| 独立claim | 公开Execute capture→acceptCandidate确认独立claim，原source包含不同raw禁用marker且不授raw权限；真实DeskTurn payload含claim原文、不含raw禁用marker，manifest.Input为确切claim ref；fake reply明确used=[M1]，产品将其记录为supported的确切claim。 |
| 未知model raw拒绝 | 公开Ingest建立零claim raw source；公开ContextRecipient不提供可授权route，SetSourceAuthorization的最小selection和客户端猜first均拒绝，策略无落地；普通DeskTurn仍运行，其真实HTTP正文、snapshot与manifest.Input不含raw源。 |

每子场景同时验证：adapter最终选出catalog中的first，Registry.ContextObserver捕获prepared/before_dispatch/dispatched且model=first/provider/protocol/endpoint正确，每event最终bytes与fakeHTTP接收逐字节一致。Store持久attempt完成，manifest.recipient使用first而非unknown，合法route fingerprint，snapshot与实际HTTP正文完全相等。Registry.generationContext源码已证实其ContextObserver与Store安装的bound observer链式执行；不以外层context observer冒称事件。

数据库仅使用协调者指定的127.0.0.1:33273/phase2_c合成数据库；每子例创建唯一schema，schema search_path连接真实Store.Migrate，cleanup只DROP本人schema。不得skip，不parallel，不接真实账号或模型，不使用生产数据库。

执行顺序：先commit冻结预期和新harness；compile-only gate（-run '^$'，有失败即保留）；再一次3子例，原日志/退出/计数/精确SHA全部保留。发现产品问题交协调者修复；不弱化断言或无因重跑。此证据是实际Store→SIWC适配器→loopback HTTP fake，不是真实模型内部上下文或真实OAuth服务验收。
