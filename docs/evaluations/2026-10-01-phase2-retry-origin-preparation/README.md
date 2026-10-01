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
