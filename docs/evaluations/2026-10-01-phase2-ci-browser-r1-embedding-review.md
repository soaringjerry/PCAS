# embedding query 隔离小补：独立只读审阅

## 范围与结论

审阅A产品 **22351214fce5b5ccbe32d5625349e75f298846f2**，root精确组合 **c49ec2affd5aea67b0425d97384ce023fa392842**，并核授权恢复例组合 **03e5eee98854352c9f88d5c109d1ed6779e2fbc7** 的四Go文件与契约无差异。审阅只读，不改产品/测试/runner，不执行Go测试。

**该四文件小补范围内未发现阻塞项。** 当前独立owner query可外发；已知生成Prompt和拼接事项/旧结果继续本地检索；客户端没有新授权参数可绕过该选择。prepare/final现有来源与权限门保留。此结论来自静态数据流与调用边界，不宣称并发/HTTP embedding隔离已全部动态验证。

审阅文件SHA（03e5eee，同c49ec2a产品字节）：

| 文件 | SHA256 |
| --- | --- |
| internal/postgres/desk.go | f2e9c8e2f491ae40965f6671f8291b19660b1fe1107e36124f0f9d631fe8c2e4 |
| internal/postgres/desk_turn.go | c54177e8a0c70605a039a64b9218802d4a1681f19a03043c4fbc2c8eedd49149 |
| internal/postgres/retrieval.go | d6448df6e2931020344e0e49df8b4b410b90a4773bdade68892ae2017b721408 |
| internal/postgres/run_context.go | 33932e516df2538d4e41d58cd8d6bf076fb72a5ce27012f3ab3fb3a328ab82fb |

[单恢复例开测前SHA记录](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/preflight.json)是相同源码的独立记录；该owner手动路径通过不等于embedding产品全部分支实测。

## 逐项核对

### 1. override只有服务端可设置

`retrieval.go:39-44` 的空类型 `recallEmbeddingQueryKey` 与 `withRecallEmbeddingQuery` 均未导出。值是服务端context中的独立query，trim并tail至4000；既有typed JSON request无法创建此Go key。`memory/contracts.go:83` 的RecallRequest只有query/context/mode/budget/cursor，未新增override字段；`httpapi/server.go:149-153`严格解码后直接传r.Context，`decode:186`拒绝未知字段。

四产品文件全局调用检索结果仅三个服务端consumer：Run prepare、DeskTurn secretaryPrompt、AnswerDesk。客户端scope/Task/recipient参数不构成embedding授权或override。普通公开Recall仍沿当前输入query/Context.Text工作；本批没有新增embedding recipient授权协议。

### 2. Run完整query与外发query分开

`run_context.go:445-450` 分别建立local query和embeddingQuery，两者起点是本轮c.Prompt。私有secretaryArtifact marker存在或derived非空就清空embeddingQuery，即使生成行为typed deps为空，仍不会借零deps放行。

`run_context.go:505-515`正式加载SourceRunID的同owner/同item原persisted prompt origins；任一非空origins清空embeddingQuery，编辑新prompt/标点不能让这分支外发。此处核的是server origins，不相信客户端传递的provenance或授权。

`run_context.go:566/575`将sanitize后的事项Title/Notes/Body/Goal/Progress及允许的previous.Prompt/Output追加到**local query**，没有修改embeddingQuery；588显式私有override传Recall。是否允许拼入历史由既有filter/fence决定，外发选择不依赖Task.DeskActions是否空，合法普通历史仍用于本地检索与最终Brief/package。

### 3. 空override保留本地检索，不回退外发完整query

`retrieval.go:86-90`用type assertion的restricted bool区分“无override”和“有效空override”；空字符串仍restricted，不会回退local query。105-106只追加明确coverage gap，未调用EmbedProvider；tokens/fts仍从完整query构造，128-129仍以完整query/fts/tokens进入recallTx，vector为空。134-135使gap对应Coverage.Complete=false。

独立非空override只用于成本估算和实际 `EmbeddingQueryPrefix + embeddingQuery` 外发，114/117不再使用完整local query。普通owner Recall没有override时保留当前行为，不整体关闭Recall或现有向量存储。这里证明的是Recall返回gap；没有把它夸大为所有consumer UI都显示了该gap。

### 4. DeskTurn与AnswerDesk历史保留在本地

`desk_turn.go:269-291` earlier实际上只累加历史用户Text；完整query是earlier+req.Text，override仅本轮req.Text。Prompt中历史Reply仍走原秘书context权限/来源门，与embedding query变量分离。

`desk.go:100-105` earlier只累加历史Question，override仅本轮question。契约§8明确两处未拼旧Reply到earlier；本审阅不虚构旧Reply embedding泄漏已经实测或已经静态出现。

### 5. prepare exclusions与final重验保留

Run prepare建立真实目的item/scope和server recipient；secretary来源先验证typed deps及producer DAG，SourceRun来源加载并递归核实际origins；derived按当前目的Task hydrate。`run_context.go:532-541`在Recall前按实际item查询context_exclusions，命中indirect dep即Conflict。prepare事务结束后才Recall，workspace.go:440在owner revision FOR UPDATE（450）之前调用prepare；本补没有把Run embedding调用移到owner锁内。

final `requestRunTx:35-47`重读原Run并exact核prompt/origins、递归验证；65-76再核当前recipient、item scope与prepared indirect typed deps；80 sanitize item，88读当前exclusions；合并context后222调用实际verifyRunForItemTx再写run。其643-725保留origin subset/DAG、stale、owner/principal、scope、route、typed deps、当前item exclusions门。Recall取得快照不授予final外发/交付许可，private query选择没有替代这些门。

## 实证边界与后续门

该审阅不运行旧embedding barrier Go测试，不调整C独立gold或当前红的分类。03e5eee手动恢复单例有独立报告，证明真实manual owner路径及剪贴板/交付链；并未给embedding fakeHTTP/并发变更所有分支补动态证据。最终同产品组合的后端来源/检索隔离矩阵与完整CI仍由指定独立门报告，不能用此静态结论或此前不同头绿替代。
