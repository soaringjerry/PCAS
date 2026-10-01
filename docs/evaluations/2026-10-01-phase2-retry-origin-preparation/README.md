# SourceRunID retry：独立人工预期冻结

依 root 审定契约 `2fd719c`，在 docs 契约基线 `13d36decbbcec33f5d989be1a531e28403b4219e` 冻结 [retry-origin-sequences.json](../../../testdata/phase2/retry-origin-sequences.json)，SHA `6f34d7604e09339237c74153ff39e6d880f40f9d7c912237606f4807b6228860`。冻结前未阅读 backend `be761f5`，没有改既有gold/原golden，也尚未运行任何新用例。

首批规划新 `phase2_runtime_retry_origin_test.go`：**1 顶层/13 真叶**，复用 C 的真实 RT/DeskTurn/HTTP capture/D4 helpers；唯一新测试文件归 C。原始资料零claim；真实秘书收到 source 原文后生成 delegate Prompt，并保真实成功 delegate receipt ID。第一次副手真实 HTTP 故意503，不能直接 SQL 造 failed Run。每叶正式 requestRun 定位原服务端 Run。

| 因果叶 | 数量 | 新供给预期 |
| --- | ---: | --- |
| 当前同目标存活、换独立获准目标 | 2 | 实际新HTTP成功；Prompt origin恰真实delegate ID、Indirect含原exact source、Used=[]不免除 |
| 撤原secretary、纠正v2、删除 × 原文/仅标点 | 6 | 原delegate来源失效，新增HTTP0；副手自身allow作隔离正控 |
| 原producer存活但新目标无allow | 1 | 新接收者不得继承旧许可，HTTP0 |
| 同Thing搬入B，B canonical allow真实存在而source只assignment A | 1 | 精确硬范围拒供，HTTP0，不能被无policy掩盖 |
| 原owner独立Prompt origins为空、旧general context失效 | 1 | 真实普通新请求成功；不复制旧Task/权限/attempt |
| 跨owner、跨Thing定位 | 2 | 正式入口受控拒绝，HTTP0，不披露原Run |

预计 **26 次 setup HTTP**（每叶秘书1+真实副手503一次），加3次成功重试，共29次本地假provider请求；无sleep/真实模型。三种失效两个文本控制保留同一个词与 atom，不通过试词选择结果。失败断言保留完整真实请求、run/attempt/manifest、原source exact ref和实际actionIDs；所有新成功Run有新Task/new attempt，禁止复制旧交付事实。成本仅规划，动态报告以实际请求数为准。

此批专注 failed automatic run 的正式重试。manual initialRun 预填改字、final prepare→commit 竞态以及 UI 携带 sourceRunId 仍是明确后续入口证据，不能由这13叶冒称通过。任意无locator新owner文字不属于该 locator 可信继承契约，也不靠测试端 mint origin授信。

下一边界：root 审gold后指定新组合，C先净harness/compile，再按明确SHA单次动态；不先跑未合消费者。本次没有新测试通过结论。

## 净 harness 边界

基线 `3f30ad4cc5d3d38dca4e59f6a95645c264816fc2` + 既有独立gold。两个新测试文件形成3顶层/18人工叶（retry13、manual/final3、receipt2）；[compile.txt](compile.txt)为 `go test ./internal/postgres -run '^$' -count=1` 退出0，[list.txt](list.txt)是实际三个顶层list。**无动态结果**。

503通过真实loopback代理返回，内层复用原RT capture的schema协议；每次actual请求/外层status/实际响应单独保留，未SQL造failed。manual实际GET正文/DeliveredAt/unknown/当前接收者及exact源Indirect均核。receipt真实两turn/严格逆Undo/HTTP history/原actionID与实际typed依赖分别核。

原外部embedding barrier设计保留为本基线的诊断方案：会保实际query、到达时刻、正式撤权回执/返回时刻及最终Run0。但新审阅确认派生query不具embedding独立许可，这是**未动态执行的已知设计缺口**，不能把该barrier或compile当新的embedding授权通过。root批准下一追加修订使用真实owner行锁等待观察隔离final竞态，同时generated retry embedding trap0；旧gold/设计原文保留，不通过skip或SQL状态注入处理。

## 查询边界与 final gate 追加修订

root已审 `0964d35` / 独立gold `e8d6495`。generated retry embedding trap必须0；owner origins为空的原正控仍实际调用vector，解码input恰本轮Prompt。新增claim正式destination exclusion与秘书本轮query-only两叶；原18叶不删，变为**20新真叶**，加原continuation3叶是**23因果叶**。原gold与首次外部embedding设计保留在前一提交，未执行、不能称通过。

final gate使用独立fixture/monitor连接，仅对实际workspace_owners行SELECT FOR UPDATE，不修改任何状态。正式SetSourceAuthorization先排队，监测pg_stat_activity/pg_blocking_pids实际PID/query/wait；retry真实prepare/本地Recall后在同owner行排队，观察其被先到撤权PID挡住才放行。保存两PID/依赖链/时刻，分别核正式撤权返回与持久deny、final精确Conflict、Run0/模型0。4秒观察超时直接失败，无SQL prepared注入、任意sleep到达推定或重试。

旧UX三叶仅迁移embedding传输层：实际input恰 `Continue that`；`Ordinary permitted plan`仍须实际GET package包含，482910/package/ContextMemoryIDs/scope原断言不变。**package证明合法历史续作保留，不能单独证明Recall lexical query动态含历史**；完整本地query的组成依契约与prepare静态审阅，当前没有动态query捕获，未虚构该证据。旧gold的“proof local full query still used”由本段精确限定，原文件不改。
